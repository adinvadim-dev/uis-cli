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


class BundleTests(unittest.TestCase):
    def test_packaged_skill_has_its_recursive_dependencies_and_helper(self):
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp) / 'bundle'
            proc = subprocess.run([sys.executable, str(ROOT / 'scripts/skill_bundle.py'),
                                   '--output', str(output)], capture_output=True, text=True)
            self.assertEqual(proc.returncode, 0, proc.stderr)
            self.assertTrue((output / 'docs/TRANSFER_INVESTIGATION.md').is_file())
            self.assertTrue((output / 'docs/research/05-private-call-search-live.md').is_file())
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
