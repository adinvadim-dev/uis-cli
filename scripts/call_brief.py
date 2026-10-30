#!/usr/bin/env python3
"""Offline, bounded projection of uis.cli/v1 envelopes. No credentials or I/O writes."""
import argparse
import json
import re
import sys

MAX_INPUT = 16 * 1024 * 1024
SEARCH_FIELDS = ('communication_id', 'call_start_date_time', 'call_direction',
                 'contact_phone_number', 'virtual_phone_number', 'call_status',
                 'employees', 'call_scenario', 'call_is_transfer')
HEADER_FIELDS = ('id', 'start_time', 'direction', 'employee_name', 'employees',
                 'scenario_name', 'scenarios', 'numa', 'numb', 'durations',
                 'total_duration', 'lost_info')
EVENT_FIELDS = ('event_time', 'event_mnemonic', 'event_name', 'event_text',
                'event_type', 'duration', 'fail_hint', 'is_failed',
                'scenario_redirection_id', 'scenario_redirection_name')
CELL_FIELDS = ('value', 'value_id', 'entity_id', 'entity_type', 'name', 'id',
               'postprocess_duration', 'talk_clean_duration', 'talk_duration',
               'wait_duration', 'wait_total_duration', 'is_lost')


class ShapeError(ValueError):
    pass


class Projection:
    def __init__(self):
        self.truncated_strings = 0
        self.omitted_cells = 0

    def clean(self, value, depth=0):
        if depth > 12:
            raise ShapeError('excessive nesting')
        if isinstance(value, str):
            value = re.sub(r'https?://[^\s<>"\']+', '[URL]', value)
            if len(value) > 500:
                self.truncated_strings += 1
                value = value[:500] + '…'
            return value
        if isinstance(value, list):
            self.omitted_cells += max(0, len(value) - 20)
            return [self.clean(v, depth + 1) for v in value[:20]]
        if isinstance(value, dict):
            # Only semantic cell members survive; provider extras/recording locators do not.
            return {k: self.clean(v, depth + 1) for k, v in value.items() if k in CELL_FIELDS}
        if value is None or isinstance(value, (bool, int, float)):
            return value
        raise ShapeError('unsupported value')

    def fields(self, obj, names):
        if not isinstance(obj, dict):
            raise ShapeError('expected object')
        return {k: self.clean(obj[k]) for k in names if k in obj}


def page(rows, args):
    return rows[args.offset:args.offset + args.limit]


def events(rows, parent='', depth=0):
    if depth > 12 or not isinstance(rows, list):
        raise ShapeError('unsupported event tree')
    for index, row in enumerate(rows):
        if not isinstance(row, dict):
            raise ShapeError('unsupported event row')
        path = f'{parent}/{index}'
        yield path, row
        for key in ('data', 'childNode'):
            children = row.get(key)
            if children is None:
                continue
            if isinstance(children, dict):
                children = [children]
            # In live UIS timelines childNode is also a presentation flag.
            if isinstance(children, bool):
                continue
            yield from events(children, path + '/' + key, depth + 1)


def filter_fields(value, field, depth=0):
    if depth > 30:
        raise ShapeError('excessive catalog nesting')
    if isinstance(value, list):
        for child in value:
            yield from filter_fields(child, field, depth + 1)
    elif isinstance(value, dict):
        if value.get('id') == field and ('format' in value or 'operators' in value):
            yield value
            return
        for key, child in value.items():
            if key != 'values':
                yield from filter_fields(child, field, depth + 1)


def project(envelope, args):
    if not isinstance(envelope, dict) or envelope.get('schemaVersion') != 'uis.cli/v1':
        raise ShapeError('expected uis.cli/v1 envelope')
    if envelope.get('ok') is False:
        err = envelope.get('error') or {}
        details = err.get('details') or {}
        return {'ok': False, 'error': {'code': err.get('code'),
                                     'stage': details.get('stage')}}, 1
    if envelope.get('ok') is not True or not isinstance(envelope.get('data'), dict):
        raise ShapeError('expected successful data object')
    data = envelope['data']
    out = {'ok': True, 'projection': args.mode, 'source': data.get('source')}
    p = Projection()
    if args.mode == 'search':
        rows = data.get('data')
        if not isinstance(rows, list) or any(not isinstance(r, dict) or 'communication_id' not in r for r in rows):
            raise ShapeError('expected private call rows with communication_id')
        out.update(p.fields(data, ('date_from', 'date_till', 'limit', 'offset', 'limitations')))
        out['metadata'] = {'total_items': (data.get('metadata') or {}).get('total_items')}
        out['rows'] = [p.fields(row, SEARCH_FIELDS) for row in page(rows, args)]
    elif args.mode == 'details':
        detail = data.get('data')
        if data.get('reduced') or not isinstance(detail, dict) or not isinstance(detail.get('header'), dict) or 'id' not in detail['header']:
            raise ShapeError('full private timeline unavailable; inspect source/reduced marker locally')
        out['header'] = p.fields(detail['header'], HEADER_FIELDS)
        rows = list(events(detail.get('grid')))
        out['events'] = [dict(path=path, **p.fields(row, EVENT_FIELDS))
                         for path, row in page(rows, args)]
        out['attribution'] = 'Match termination to a leg; generic employee disconnect is ambiguous.'
    else:
        rows = list(filter_fields(data.get('data'), args.field))
        if len(rows) != 1:
            raise ShapeError('requested catalog field missing or ambiguous')
        field = rows[0]
        out['field'] = p.fields(field, ('id', 'name', 'format'))
        # Operators are small descriptors, unlike the account-wide value lists.
        out['field']['operators'] = p.clean(field.get('operators', []))
        values = field.get('values', [])
        if not isinstance(values, list):
            raise ShapeError('unsupported catalog values')
        out['values_available'] = len(values)
        rows = values if args.values else []
        if args.values:
            out['values'] = [p.clean(v) for v in page(rows, args)]
    out.update(total_rows=len(rows), projection_offset=args.offset,
               omitted=len(rows) - len(page(rows, args)),
               truncated_strings=p.truncated_strings, omitted_cells=p.omitted_cells)
    return out, 0


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=['search', 'details', 'filters'])
    parser.add_argument('--field', help='exact catalog field ID (required for filters)')
    parser.add_argument('--values', action='store_true', help='include one bounded field value page')
    parser.add_argument('--offset', type=int, default=0)
    parser.add_argument('--limit', type=int, default=40)
    args = parser.parse_args()
    if args.offset < 0 or not 1 <= args.limit <= 100 or (args.mode == 'filters' and not args.field):
        parser.error('use offset >=0, limit 1..100, and --field for filters')
    try:
        raw = sys.stdin.buffer.read(MAX_INPUT + 1)
        if len(raw) > MAX_INPUT:
            raise ShapeError('input exceeds 16 MiB')
        out, code = project(json.loads(raw), args)
    except (ValueError, TypeError, AttributeError, RecursionError):
        # Never echo the input or raw provider errors, even on schema drift.
        out, code = {'ok': False, 'error': {'code': 'unsupported_input',
                     'message': 'Expected supported UIS JSON; inspect the private capture locally.'}}, 2
    print(json.dumps(out, ensure_ascii=False, allow_nan=False, separators=(',', ':')))
    return code


if __name__ == '__main__':
    sys.exit(main())
