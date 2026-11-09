#!/usr/bin/env python3
"""Offline per-employee comparison of UIS and Medivey snapshots. No network/writes."""
import argparse
from collections import Counter
from datetime import datetime
import hashlib
import json
from pathlib import Path
import re
from zoneinfo import ZoneInfo, ZoneInfoNotFoundError

MAX_BYTES = 64 * 1024 * 1024
MAX_ROWS = 50000


def load(path):
    with Path(path).open('rb') as stream:
        raw = stream.read(MAX_BYTES + 1)
    if len(raw) > MAX_BYTES:
        raise ValueError('input_too_large')
    value = json.loads(raw)
    if not isinstance(value, dict) or value.get('ok') is not True:
        raise ValueError('expected_success_envelope')
    return value, hashlib.sha256(raw).hexdigest()


def identifier(value):
    if isinstance(value, bool) or not re.fullmatch(r'[1-9][0-9]*', str(value)):
        raise ValueError('invalid_identity')
    return str(value)


def enum_id(value):
    return value.get('value_id') if isinstance(value, dict) else None


def duration(value):
    if value is None:
        return None
    if not isinstance(value, str) or not re.fullmatch(r'\d{2,}:\d{2}:\d{2}', value):
        raise ValueError('invalid_duration')
    hours, minutes, seconds = map(int, value.split(':'))
    if minutes >= 60 or seconds >= 60:
        raise ValueError('invalid_duration')
    return hours * 3600 + minutes * 60 + seconds


def compare(uis, transfers, employee_id, ext_ids, time_zone):
    employee_id = identifier(employee_id)
    if not ext_ids or any(not re.fullmatch(r'(?:authentik:)?[1-9][0-9]*', value) for value in ext_ids):
        raise ValueError('explicit_medivey_identity_required')
    data = uis.get('data')
    if not isinstance(data, dict) or data.get('source') != 'private.report_constructor':
        raise ValueError('expected_private_call_snapshot')
    rows = data.get('data')
    med = transfers.get('data')
    if not isinstance(rows, list) or not isinstance(med, dict) or not isinstance(med.get('items'), list):
        raise ValueError('invalid_snapshot_rows')
    if max(len(rows), len(med['items'])) > MAX_ROWS:
        raise ValueError('too_many_rows')
    zone = ZoneInfo(time_zone)
    start = datetime.strptime(data['date_from'], '%Y-%m-%d %H:%M:%S').replace(tzinfo=zone)
    end = datetime.strptime(data['date_till'], '%Y-%m-%d %H:%M:%S').replace(tzinfo=zone)
    if start >= end:
        raise ValueError('invalid_period')
    sources = {name: {'calls': 0, 'conversations': 0, 'unknown_duration': 0, 'uis_transfer_marks': 0}
               for name in ('uis_autodial', 'manual_sip', 'crm_provider', 'incoming', 'unknown')}
    types = Counter()
    seen = set()
    unresolved = other = 0
    for row in rows:
        key = identifier(row['communication_id'])
        if key in seen:
            raise ValueError('duplicate_call_identity')
        seen.add(key)
        participants = row.get('employees')
        if participants is None or participants == []:
            unresolved += 1
            continue
        if not isinstance(participants, list) or any(not isinstance(cell, dict) for cell in participants):
            raise ValueError('invalid_employee_cells')
        known = [cell for cell in participants if cell.get('entity_type') == 'employee' and cell.get('entity_id') is not None]
        ids = {identifier(cell['entity_id']) for cell in known}
        if employee_id not in ids:
            if len(known) != len(participants):
                unresolved += 1
            else:
                other += 1
            continue
        call_time = datetime.strptime(row['call_start_date_time'], '%Y-%m-%d %H:%M:%S').replace(tzinfo=zone)
        if not start <= call_time <= end:
            raise ValueError('call_outside_declared_period')
        direction, kind = enum_id(row.get('call_direction')), enum_id(row.get('call_type'))
        source = 'unknown'
        if direction == 'in':
            source = 'incoming'
        elif direction == 'out':
            if kind in ('auto_out_calls', 'progressive_call'):
                source = 'uis_autodial'
            elif kind == 'sip':
                source = 'manual_sip'
            elif kind in ('retailcrm', 'amocrm', 'bitrix'):
                source = 'crm_provider'
        talk = duration(row.get('call_talk_duration'))
        bucket = sources[source]
        bucket['calls'] += 1
        bucket['conversations'] += talk is not None and talk > 0
        bucket['unknown_duration'] += talk is None
        bucket['uis_transfer_marks'] += enum_id(row.get('call_is_transfer')) is True
        # Only known enum IDs enter output; arbitrary provider strings remain private.
        types[kind if kind in ('auto_out_calls', 'progressive_call', 'sip', 'retailcrm', 'amocrm', 'bitrix') else 'other_or_missing'] += 1
    selected = []
    seen = set()
    for row in med['items']:
        key = identifier(row['id'])
        if key in seen:
            raise ValueError('duplicate_transfer_identity')
        seen.add(key)
        time = row.get('startTime')
        if isinstance(time, bool) or not isinstance(time, (int, float)) or not start.timestamp() * 1000 <= time <= end.timestamp() * 1000:
            raise ValueError('transfer_outside_declared_period')
        if str(row.get('extUserId')) in ext_ids:
            selected.append(row['id'])
    coverage = data.get('coverage', {})
    if not isinstance(coverage, dict):
        raise ValueError('invalid_coverage')
    if coverage.get('complete') is True and (coverage.get('initial_offset') != 0
            or coverage.get('returned') != len(rows) or coverage.get('reported_total') != len(rows)):
        raise ValueError('inconsistent_coverage')
    paging_complete = coverage.get('complete') is True and coverage.get('initial_offset') == 0
    scope = data.get('query_scope')
    unfiltered = isinstance(scope, dict) and 'filter' in scope and scope['filter'] is None
    return {
        'ok': True, 'comparison': 'independent_employee_populations',
        'binding': {'uis_employee_id': employee_id, 'medivey_ext_user_ids': sorted(set(ext_ids)), 'basis': 'caller_asserted_mapping; verify both directories'},
        'period': {'from': start.isoformat(), 'till': end.isoformat(), 'end': 'inclusive', 'time_zone_basis': 'caller_supplied; verify cabinet account'},
        'uis': {'by_source': sources, 'call_types': dict(types), 'rows_in_snapshot': len(rows), 'unresolved_participant_rows': unresolved, 'other_employee_rows': other,
                'coverage': coverage, 'pagination_complete': paging_complete,
                'employee_period_coverage': 'complete_unfiltered_snapshot' if paging_complete and unfiltered and unresolved == 0 else 'unknown_filtered_or_unresolved_scope'},
        'medivey': {'transfers': len(selected), 'transfer_ids': sorted(selected, key=int),
                    'pagination_complete': med.get('complete') is True and med.get('nextAfterId') is None,
                    'employee_period_coverage': 'unknown_query_bounds_and_filters_not_echoed'},
        'per_call_links': 'not_established; current Medivey wrapper omits provider/extId/leadId',
        'limitations': ['Employee-level comparison is not a call-to-transfer conversion or join.',
                        'UIS transfer marks are not map transfers; do not sum them.',
                        'SIP is transport evidence, not proof of manual intent; provider CRM is not proof of Medivey CRM initiation.',
                        'Keys are a manual subset; key-activity measures reveals, not call origin.',
                        'Do not join masked phones; cabinet IDs are not verified classic session IDs.',
                        'Partial inputs and unresolved participants prevent full employee coverage; zero observed is not proof of zero activity.',
                        'Pagination exhaustion is not proof of matching employee/period query scopes; Medivey does not echo its export query.',
                        'Fresh snapshots cannot reconstruct unavailable historical membership.'],
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--uis', required=True, help='Protected calls search --analysis --all JSON file')
    parser.add_argument('--transfers', required=True, help='Protected medivey lead transfer list --all JSON file')
    parser.add_argument('--uis-employee-id', required=True)
    parser.add_argument('--medivey-ext-user-id', action='append', required=True, help='Verified author alias; repeat for more aliases')
    parser.add_argument('--time-zone', required=True, help='Verified cabinet IANA timezone, e.g. Europe/Moscow')
    args = parser.parse_args()
    try:
        uis, uis_hash = load(args.uis)
        transfers, transfer_hash = load(args.transfers)
        result = compare(uis, transfers, args.uis_employee_id, args.medivey_ext_user_id, args.time_zone)
        result['input_sha256'] = {'uis': uis_hash, 'medivey': transfer_hash}
        print(json.dumps(result, ensure_ascii=False))
    except (ValueError, KeyError, TypeError, OSError, ZoneInfoNotFoundError):
        # Never repeat provider payloads, filenames, identities or customer data in errors.
        print(json.dumps({'ok': False, 'error': 'invalid_or_incompatible_snapshots'}))
        return 2
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
