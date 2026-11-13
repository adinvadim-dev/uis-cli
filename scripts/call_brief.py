#!/usr/bin/env python3
"""Offline, bounded projection of uis.cli/v1 envelopes. No credentials or I/O writes."""
import argparse
import html
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


def filter_fields(value, field=None, depth=0):
    if depth > 30:
        raise ShapeError('excessive catalog nesting')
    if isinstance(value, list):
        for child in value:
            yield from filter_fields(child, field, depth + 1)
    elif isinstance(value, dict):
        if (isinstance(value.get('id'), str)
                and (field is None or value['id'] == field)
                and ('format' in value or 'operators' in value)):
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
    elif args.mode == 'fields':
        rows = list(filter_fields(data.get('data')))
        if not rows:
            raise ShapeError('catalog fields unavailable')
        out['fields'] = [p.fields(row, ('id', 'name', 'format', 'operators'))
                         for row in page(rows, args)]
        out['remaining'] = max(0, len(rows) - args.offset - len(out['fields']))
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


BOLD = re.compile(r'<b>(.*?)</b>', re.S | re.I)
KEY_TEXT = re.compile(r'нажал[аи]?\s+клавишу', re.I)
SEQUENCE_TEXT = re.compile(r'вв[её]л[аи]?\s+последовательность', re.I)
DTMF_VALUE = re.compile(r'[0-9*#A-D]+')
DTMF_LIKE = re.compile(r'тональн|нажал[аи]?\s+клавиш|последовательност', re.I)


def plain(text):
    return ' '.join(html.unescape(re.sub(r'<[^>]*>', ' ', text)).split())


def mask_digits(text):
    return re.sub(r'\d{5,}', lambda m: '***' + m.group()[-4:], text)


def parse_dtmf(text):
    """Return (kind, value, party) or a reason string for an unparsed text."""
    if not isinstance(text, str):
        return 'unknown_text'
    words = plain(text)
    kind = 'key' if KEY_TEXT.search(words) else 'sequence' if SEQUENCE_TEXT.search(words) else None
    bold = [plain(b) for b in BOLD.findall(text)]
    if kind is None or not bold:
        return 'unknown_text'
    value = bold[-1].upper()
    if not DTMF_VALUE.fullmatch(value) or (kind == 'key' and len(value) != 1):
        return 'invalid_' + kind
    party = mask_digits(bold[0]) if len(bold) > 1 else None
    return kind, value, party


def read_captures(paths, stdin):
    """Yield (label, envelope-or-failure-reason) for JSON, JSON arrays and JSONL."""
    for path in paths or ['-']:
        name = 'stdin' if path == '-' else path
        try:
            if path == '-':
                raw = stdin.buffer.read(MAX_INPUT + 1)
            else:
                with open(path, 'rb') as handle:
                    raw = handle.read(MAX_INPUT + 1)
        except OSError:
            yield name, 'unreadable'
            continue
        if len(raw) > MAX_INPUT:
            yield name, 'input_exceeds_16_MiB'
            continue
        if not raw.strip():
            # A failed or timed-out `calls show` leaves an empty capture: a failure, never "no keys".
            yield name, 'empty_input'
            continue
        try:
            doc = json.loads(raw)
        except ValueError:
            doc = None
            lines = raw.splitlines()
        else:
            lines = None
        if isinstance(doc, list):
            for index, item in enumerate(doc):
                yield f'{name}[{index}]', item
        elif lines is None:
            yield name, doc
        else:
            for number, line in enumerate(lines, 1):
                if not line.strip():
                    continue
                try:
                    yield f'{name}:{number}', json.loads(line)
                except ValueError:
                    yield f'{name}:{number}', 'invalid_json'


def detail_rows(capture):
    """Return (call_id, events) or a failure reason; never treat failure as no keys."""
    if isinstance(capture, str):
        return capture
    if not isinstance(capture, dict) or capture.get('schemaVersion') != 'uis.cli/v1':
        return 'not_uis_envelope'
    if capture.get('ok') is False:
        code = (capture.get('error') or {}).get('code')
        return 'error_envelope:' + (code if isinstance(code, str) else 'unknown')
    data = capture.get('data')
    detail = data.get('data') if isinstance(data, dict) else None
    header = detail.get('header') if isinstance(detail, dict) else None
    if (capture.get('ok') is not True or not isinstance(data, dict) or data.get('reduced') or not isinstance(header, dict)
            or not isinstance(header.get('id'), (int, str)) or isinstance(header.get('id'), bool)):
        return 'reduced_or_unsupported_detail'
    try:
        return header['id'], list(events(detail.get('grid')))
    except (ShapeError, RecursionError):
        return 'unsupported_event_tree'


def dtmf_report(captures, summary=False):
    calls, failed, unparsed, seen = {}, [], [], set()
    duplicates = 0
    count = 0
    for label, capture in captures:
        count += 1
        result = detail_rows(capture)
        if isinstance(result, str):
            failed.append({'capture': label, 'reason': result})
            continue
        call_id, rows = result
        call = calls.setdefault(call_id, [])
        for path, row in rows:
            time = row.get('event_time')
            is_dtmf = row.get('event_mnemonic') == 'dtmf'
            if not is_dtmf:
                hint = ' '.join(str(row.get(k) or '') for k in ('event_name', 'event_text'))
                if not DTMF_LIKE.search(hint):
                    continue
            key = (call_id, path, time)
            if key in seen:
                duplicates += 1
                continue
            seen.add(key)
            parsed = parse_dtmf(row.get('event_text')) if is_dtmf else 'dtmf_like_without_mnemonic'
            if isinstance(parsed, str):
                text = row.get('event_text')
                unparsed.append({'call_id': call_id, 'path': path, 'time': time, 'reason': parsed,
                                 'text': mask_digits(plain(text))[:200] if isinstance(text, str) else None})
                continue
            kind, value, party = parsed
            call.append({'time': time, 'path': path, 'kind': kind, 'value': value, 'party': party})
    key_counts, calls_by_key, sequence_counts = {}, {}, {}
    for items in calls.values():
        for item in items:
            target = key_counts if item['kind'] == 'key' else sequence_counts
            target[item['value']] = target.get(item['value'], 0) + 1
        for value in {i['value'] for i in items if i['kind'] == 'key'}:
            calls_by_key[value] = calls_by_key.get(value, 0) + 1
    complete = not failed and not unparsed
    out = {'ok': True, 'projection': 'dtmf', 'complete': complete, 'captures': count,
           'calls_scanned': len(calls), 'calls_with_dtmf': sum(1 for v in calls.values() if v),
           'key_counts': dict(sorted(key_counts.items())),
           'calls_by_key': dict(sorted(calls_by_key.items())),
           'sequence_counts': dict(sorted(sequence_counts.items())),
           'duplicate_events': duplicates, 'failed': failed, 'unparsed': unparsed}
    if not summary:
        out['calls_without_dtmf'] = [k for k, v in calls.items() if not v]
        out['calls'] = [{'call_id': k, 'dtmf': v} for k, v in calls.items() if v]
    return out, 0 if complete else 1


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=['search', 'details', 'fields', 'filters', 'dtmf'])
    parser.add_argument('paths', nargs='*', help='dtmf only: detail captures (JSON, JSON array or JSONL); - is stdin')
    parser.add_argument('--summary', action='store_true', help='dtmf only: omit per-call lists')
    parser.add_argument('--field', help='exact catalog field ID (required for filters)')
    parser.add_argument('--values', action='store_true', help='include one bounded field value page')
    parser.add_argument('--offset', type=int, default=0)
    parser.add_argument('--limit', type=int, default=40)
    args = parser.parse_args()
    if args.offset < 0 or not 1 <= args.limit <= 100 or (args.mode == 'filters' and not args.field):
        parser.error('use offset >=0, limit 1..100, and --field for filters')
    if args.mode == 'fields' and (args.field or args.values):
        parser.error('fields lists descriptors; use filters --field for values')
    if args.mode != 'dtmf' and (args.paths or args.summary):
        parser.error('file paths and --summary apply only to dtmf')
    if args.mode == 'dtmf':
        out, code = dtmf_report(read_captures(args.paths, sys.stdin), args.summary)
        print(json.dumps(out, ensure_ascii=False, allow_nan=False, separators=(',', ':')))
        return code
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
