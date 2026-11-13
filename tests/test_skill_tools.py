"""Contracts for agent-visible projections and the installed skill bundle."""
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


def envelope(data, ok=True):
    return {'schemaVersion': 'uis.cli/v1', 'ok': ok, 'data': data}


def brief(mode, payload, *args):
    proc = subprocess.run([sys.executable, str(ROOT / 'scripts/call_brief.py'), mode, *args],
                          input=json.dumps(payload), text=True, capture_output=True)
    return proc.returncode, json.loads(proc.stdout)


class BriefTests(unittest.TestCase):
    def test_search_keeps_exact_identity_and_unknown_count(self):
        code, out = brief('search', envelope({'source': 'private.report_constructor',
            'metadata': {'total_items': None}, 'offset': 20, 'limit': 20,
            'data': [{'communication_id': 9007199254740993,
                      'employees': [{'value': 'Agent A', 'entity_id': 123}],
                      'recording_url': 'https://private.invalid/signed-secret'}]}))
        self.assertEqual(code, 0)
        self.assertEqual(out['rows'][0]['communication_id'], 9007199254740993)
        self.assertIsNone(out['metadata']['total_items'])
        self.assertEqual(out['offset'], 20)
        self.assertEqual(out['rows'][0]['employees'][0]['value'], 'Agent A')
        self.assertNotIn('signed-secret', json.dumps(out))

    def test_details_keeps_nested_termination_and_parent_path(self):
        payload = envelope({'source': 'private.app', 'data': {'header': {'id': 123},
            'grid': [{'event_text': 'Transfer to receiving line', 'data': [
                {'event_time': '12:01:00', 'event_text': 'Connected', 'childNode': [
                    {'event_time': '12:01:02', 'event_mnemonic': 'finish',
                     'event_text': 'Employee disconnected', 'fail_hint': None}]}]}]}})
        code, out = brief('details', payload)
        self.assertEqual(code, 0)
        self.assertEqual([e['path'] for e in out['events']],
                         ['/0', '/0/data/0', '/0/data/0/childNode/0'])
        self.assertEqual(out['events'][2]['event_text'], 'Employee disconnected')
        self.assertIn('ambiguous', out['attribution'])
        _, tail = brief('details', payload, '--offset', '2', '--limit', '1')
        self.assertEqual(tail['events'], [out['events'][2]])
        self.assertEqual(tail['omitted'], 2)

    def test_live_boolean_child_marker_and_durations(self):
        payload = envelope({'source': 'private.app', 'data': {'header': {
            'id': 123, 'durations': {'talk_duration': 180, 'wait_duration': 2},
            'lost_info': {'is_lost': False}}, 'grid': [{'event_text': 'Transfer',
            'data': [{'childNode': True, 'event_text': 'Employee disconnected'}]}]}})
        code, out = brief('details', payload)
        self.assertEqual(code, 0)
        self.assertEqual(out['header']['durations']['talk_duration'], 180)
        self.assertFalse(out['header']['lost_info']['is_lost'])
        self.assertEqual(out['events'][1]['event_text'], 'Employee disconnected')

    def test_catalog_discards_unrequested_values_and_pages_selected_field(self):
        payload = envelope({'source': 'private.cabinet_rpc', 'data': {'data': [
            {'id': 'employees', 'name': 'Employees', 'format': 'list',
             'operators': ['='], 'values': ['Name ' + str(n) for n in range(10000)]},
            {'id': 'contact_phone_number', 'name': 'Phone', 'format': 'text',
             'operators': ['=', 'like'], 'values': []}]}})
        code, out = brief('filters', payload, '--field', 'contact_phone_number')
        self.assertEqual(code, 0)
        self.assertEqual(out['field']['operators'], ['=', 'like'])
        self.assertLess(len(json.dumps(out)), 700)
        self.assertNotIn('values', out)
        _, selected = brief('filters', payload, '--field', 'employees', '--values',
                            '--offset', '3', '--limit', '2')
        self.assertEqual(selected['values'], ['Name 3', 'Name 4'])
        self.assertEqual(selected['values_available'], 10000)
        self.assertEqual(selected['omitted'], 9998)

    def test_field_discovery_pages_descriptors_without_catalog_values(self):
        payload = envelope({'source': 'private.cabinet_rpc', 'data': {'data': [
            {'id': 'employees', 'name': 'Employees', 'format': 'list',
             'operators': [{'id': '=', 'name': 'equals'}],
             'values': [{'id': 'nested-private-field', 'format': 'text',
                         'name': 'Private employee'}]},
            {'id': 'contact_phone_number', 'name': 'Caller', 'format': 'text',
             'operators': ['=', 'like'], 'values': ['private-phone'],
             'recording_url': 'https://private.invalid/signed-secret'}]}})
        code, out = brief('fields', payload, '--limit', '1')
        self.assertEqual(code, 0)
        self.assertEqual(out['fields'], [{'id': 'employees', 'name': 'Employees',
            'format': 'list', 'operators': [{'id': '=', 'name': 'equals'}]}])
        self.assertEqual(out['total_rows'], 2)
        self.assertEqual(out['omitted'], 1)
        self.assertEqual(out['remaining'], 1)
        _, second = brief('fields', payload, '--offset', '1', '--limit', '1')
        self.assertEqual(second['fields'], [{'id': 'contact_phone_number',
            'name': 'Caller', 'format': 'text', 'operators': ['=', 'like']}])
        self.assertEqual(second['remaining'], 0)
        _, empty = brief('fields', payload, '--offset', '2', '--limit', '1')
        self.assertEqual(empty['fields'], [])
        self.assertEqual(empty['remaining'], 0)
        rendered = json.dumps([out, second])
        self.assertNotIn('nested-private-field', rendered)
        self.assertNotIn('Private employee', rendered)
        self.assertNotIn('private-phone', rendered)
        self.assertNotIn('signed-secret', rendered)

    def test_field_discovery_rejects_unknown_catalog_shape(self):
        code, out = brief('fields', envelope({'data': {'unexpected': 'private-body'}}))
        self.assertEqual(code, 2)
        self.assertFalse(out['ok'])
        self.assertNotIn('private-body', json.dumps(out))

    def test_error_does_not_become_empty_success_or_echo_provider_body(self):
        payload = {'schemaVersion': 'uis.cli/v1', 'ok': False, 'error': {
            'code': 'provider_error', 'message': 'unsafe-provider-content',
            'details': {'stage': 'cabinet_transport', 'body': 'private-cookie'}}}
        code, out = brief('search', payload)
        self.assertEqual(code, 1)
        self.assertEqual(out['error'], {'code': 'provider_error', 'stage': 'cabinet_transport'})
        self.assertNotIn('unsafe-provider-content', json.dumps(out))
        self.assertNotIn('private-cookie', json.dumps(out))

    def test_redaction_and_truncation_are_visible(self):
        payload = envelope({'data': {'header': {'id': 1}, 'grid': [
            {'event_text': 'x' * 600 + ' https://media.invalid/signed-secret'}]}})
        code, out = brief('details', payload)
        self.assertEqual(code, 0)
        self.assertEqual(out['truncated_strings'], 1)
        self.assertNotIn('signed-secret', json.dumps(out))
        self.assertLess(len(out['events'][0]['event_text']), 510)

    def test_schema_drift_and_reduced_fallback_fail_explicitly(self):
        for payload in [envelope({'data': [{'unexpected': 'body'}]}),
                        envelope({'data': 'private-body'}), {'raw': 'private-body'}]:
            code, out = brief('search', payload)
            self.assertEqual(code, 2)
            self.assertFalse(out['ok'])
            self.assertNotIn('private-body', json.dumps(out))
        code, out = brief('details', envelope({'source': 'dataapi', 'reduced': True,
            'data': {'header': {'id': 123}, 'grid': []}}))
        self.assertEqual(code, 2)
        self.assertFalse(out['ok'])


def detail(call_id, grid, **extra):
    return envelope({'source': 'private.app', **extra,
                     'data': {'header': {'id': call_id}, 'grid': grid}})


def dtmf_event(time, text, **extra):
    return {'event_time': time, 'event_mnemonic': 'dtmf', 'event_name': 'Тональный набор',
            'event_text': text, 'event_type': 'talk', **extra}


def run_dtmf(*args, stdin=''):
    proc = subprocess.run([sys.executable, str(ROOT / 'scripts/call_brief.py'), 'dtmf', *args],
                          input=stdin, text=True, capture_output=True)
    return proc.returncode, json.loads(proc.stdout)


class DtmfTests(unittest.TestCase):
    CALL_A = detail(101, [
        {'event_text': 'Входящий звонок', 'data': [
            dtmf_event('09:30:56', '<b>Иванова Анна, 0590001</b> нажал клавишу <b>#</b>'),
            dtmf_event('09:30:59', '<b>Иванова Анна, 0590001</b> ввёл последовательность <b>2693</b>',
                       event_type='wait')]}])
    CALL_B = detail(102, [
        {'event_text': 'Автообзвон', 'data': [
            {'event_text': 'Соединение', 'childNode': [
                dtmf_event('09:34:24', '<b>79161234567</b> нажал клавишу <b>2</b>')]}]},
        {'event_mnemonic': 'finish', 'event_text': 'Абонент завершил звонок'}])
    CALL_C = detail(103, [{'event_mnemonic': 'talk', 'event_text': 'Разговор'}])

    def test_counts_nested_keys_sequences_and_masks_parties(self):
        payload = json.dumps([self.CALL_A, self.CALL_B, self.CALL_C])
        code, out = run_dtmf(stdin=payload)
        self.assertEqual(code, 0)
        self.assertTrue(out['complete'])
        self.assertEqual(out['calls_scanned'], 3)
        self.assertEqual(out['calls_with_dtmf'], 2)
        self.assertEqual(out['key_counts'], {'#': 1, '2': 1})
        self.assertEqual(out['calls_by_key'], {'#': 1, '2': 1})
        self.assertEqual(out['sequence_counts'], {'2693': 1})
        self.assertEqual(out['calls_without_dtmf'], [103])
        calls = {c['call_id']: c for c in out['calls']}
        self.assertEqual(calls[101]['dtmf'], [
            {'time': '09:30:56', 'path': '/0/data/0', 'kind': 'key', 'value': '#',
             'party': 'Иванова Анна, ***0001'},
            {'time': '09:30:59', 'path': '/0/data/1', 'kind': 'sequence', 'value': '2693',
             'party': 'Иванова Анна, ***0001'}])
        self.assertEqual(calls[102]['dtmf'][0]['path'], '/0/data/0/childNode/0')
        self.assertEqual(calls[102]['dtmf'][0]['value'], '2')
        rendered = json.dumps(out, ensure_ascii=False)
        self.assertNotIn('0590001', rendered)
        self.assertNotIn('79161234567', rendered)
        self.assertEqual(out['failed'], [])
        self.assertEqual(out['unparsed'], [])

    def test_duplicate_captures_and_jsonl_files_are_counted_once(self):
        with tempfile.TemporaryDirectory() as temp:
            first = Path(temp) / 'a.json'
            first.write_text(json.dumps(self.CALL_B))
            lines = Path(temp) / 'b.jsonl'
            lines.write_text('\n'.join(json.dumps(x) for x in [self.CALL_B, self.CALL_A]) + '\n')
            code, out = run_dtmf(str(first), str(lines))
        self.assertEqual(code, 0)
        self.assertEqual(out['captures'], 3)
        self.assertEqual(out['calls_scanned'], 2)
        self.assertEqual(out['duplicate_events'], 1)
        self.assertEqual(out['key_counts'], {'#': 1, '2': 1})

    def test_failed_captures_are_listed_never_zero(self):
        failed = {'schemaVersion': 'uis.cli/v1', 'ok': False, 'error': {
            'code': 'provider_error', 'message': 'private-body',
            'details': {'stage': 'cabinet_transport'}}}
        reduced = detail(105, [], reduced=True)
        no_grid = envelope({'source': 'private.app', 'data': {'header': {'id': 107}}})
        stdin = '\n'.join([json.dumps(self.CALL_B), json.dumps(failed),
                           json.dumps(reduced), '{not json', json.dumps(no_grid)])
        code, out = run_dtmf('-', '/nonexistent/detail.json', stdin=stdin)
        self.assertEqual(code, 1)
        self.assertFalse(out['complete'])
        self.assertEqual(out['calls_scanned'], 1)
        self.assertEqual(out['key_counts'], {'2': 1})
        reasons = [(f['capture'], f['reason']) for f in out['failed']]
        self.assertEqual(reasons, [
            ('stdin:2', 'error_envelope:provider_error'),
            ('stdin:3', 'reduced_or_unsupported_detail'),
            ('stdin:4', 'invalid_json'),
            ('stdin:5', 'unsupported_event_tree'),
            ('/nonexistent/detail.json', 'unreadable')])
        self.assertNotIn('private-body', json.dumps(out))

    def test_empty_capture_and_non_dict_data_are_failures(self):
        not_dict = {'schemaVersion': 'uis.cli/v1', 'ok': True, 'data': None}
        listed = {'schemaVersion': 'uis.cli/v1', 'ok': True, 'data': []}
        with tempfile.TemporaryDirectory() as temp:
            empty = Path(temp) / 'detail-108.json'
            empty.write_text('')
            blank = Path(temp) / 'detail-109.json'
            blank.write_text('  \n')
            code, out = run_dtmf(str(empty), str(blank), '-',
                                 stdin='\n'.join([json.dumps(self.CALL_B), json.dumps(not_dict), json.dumps(listed)]))
        self.assertEqual(code, 1)
        self.assertFalse(out['complete'])
        self.assertEqual(out['calls_scanned'], 1)
        self.assertEqual([f['reason'] for f in out['failed']],
                         ['empty_input', 'empty_input', 'reduced_or_unsupported_detail', 'reduced_or_unsupported_detail'])

    def test_unknown_dtmf_text_is_reported_unparsed(self):
        call = detail(106, [
            dtmf_event('10:00:00', 'Абонент 79161234567 что-то сделал'),
            dtmf_event('10:00:01', '<b>Иван</b> нажал клавишу <b>12</b>'),
            {'event_time': '10:00:02', 'event_name': 'Тональный набор',
             'event_text': '<b>Иван</b> нажал клавишу <b>5</b>'}])
        code, out = run_dtmf(stdin=json.dumps(call))
        self.assertEqual(code, 1)
        self.assertFalse(out['complete'])
        self.assertEqual(out['key_counts'], {})
        self.assertEqual(out['calls_with_dtmf'], 0)
        self.assertEqual(out['calls_without_dtmf'], [])
        self.assertEqual(out['calls_with_unparsed_dtmf'], [106])
        self.assertEqual([(u['path'], u['reason']) for u in out['unparsed']], [
            ('/0', 'unknown_text'), ('/1', 'invalid_key'), ('/2', 'dtmf_like_without_mnemonic')])
        self.assertNotIn('79161234567', json.dumps(out))
        self.assertEqual(out['unparsed'][0]['text'], 'Абонент ***4567 что-то сделал')


class BundleTests(unittest.TestCase):
    def test_packaged_skill_has_its_recursive_dependencies_and_helper(self):
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp) / 'bundle'
            proc = subprocess.run([sys.executable, str(ROOT / 'scripts/skill_bundle.py'),
                                   '--output', str(output)], capture_output=True, text=True)
            self.assertEqual(proc.returncode, 0, proc.stderr)
            self.assertTrue((output / 'docs/TRANSFER_INVESTIGATION.md').is_file())
            self.assertTrue((output / 'docs/CALL_ANALYTICS.md').is_file())
            self.assertTrue((output / 'docs/AUTH_SELF_HEAL.md').is_file())
            self.assertTrue((output / 'docs/research/05-private-call-search-live.md').is_file())
            self.assertTrue((output / '.agents/skills/verify-uis-cli/features/staff-diagnostics.md').is_file())
            self.assertTrue((output / '.agents/skills/verify-uis-cli/scripts/verify.py').stat().st_mode & 0o111)
            code = subprocess.run([sys.executable, str(output / 'scripts/call_brief.py'),
                'search'], input=json.dumps(envelope({'data': [], 'metadata': {}})),
                capture_output=True, text=True)
            self.assertEqual(code.returncode, 0, code.stderr)
            self.assertEqual(json.loads(code.stdout)['rows'], [])

    def test_missing_nested_reference_blocks_packaging(self):
        spec = importlib.util.spec_from_file_location('bundle', ROOT / 'scripts/skill_bundle.py')
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / 'SKILL.md').write_text('[limits](limits.md)')
            (root / 'limits.md').write_text('[research](missing.md)')
            with self.assertRaisesRegex(ValueError, 'missing dependency missing.md'):
                module.manifest(root)


if __name__ == '__main__':
    unittest.main()
