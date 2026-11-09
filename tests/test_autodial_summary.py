import datetime as dt
import importlib.util
import json
from pathlib import Path
import subprocess
import threading
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location('summary', Path(__file__).resolve().parents[1] / 'scripts/autodial_summary.py')
summary = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(summary)
FRIDAY = dt.datetime(2026, 11, 6, 17, 30, tzinfo=dt.timezone.utc)
SATURDAY = dt.datetime(2026, 11, 7, 0, tzinfo=dt.timezone.utc)


class FakeClient:
    def __init__(self, rows=None, grid=None):
        self.rows = rows if rows is not None else [{'id': 1, 'name': 'Auto', 'status': 'in_process'}]
        self.grid = grid if grid is not None else [{'talk_duration': 12, 'marks': ['Исходящий Перевод']}]
        self.calls = []
        self.refreshes = 0

    def request(self, args, timeout):
        self.calls.append((args, timeout))
        if args[1] == 'list':
            with open(args[-1]) as file:
                self.payload = json.load(file)
            return {'ok': True, 'data': {'data': self.rows}}
        return {'ok': True, 'data': {'data': {'grid_data': self.grid}}}

    def refresh(self, timeout):
        self.refreshes += 1


class SummaryTest(unittest.TestCase):
    def test_friday_host_saturday_uses_moscow_day_and_containing_marks(self):
        client = FakeClient(grid=[{'talk_duration': 12, 'marks': ['Исходящий Перевод']},
                                  {'talk_duration': 0, 'marks': []}, {'talk_duration': 7}])
        result = summary.collect(FRIDAY, client)
        self.assertFalse(result['weekend_msk'])
        self.assertTrue(result['should_send'])
        self.assertEqual(client.payload['begin_date'], '2026-11-06 00:00:00')
        self.assertEqual((result['campaigns'][0]['attempts'], result['campaigns'][0]['answered'],
                          result['campaigns'][0]['transfers_uis_mark']), (3, 2, 1))
        self.assertIn('дозвон 2 (67%) → перевод 1 (50%)', result['message_text'])
        self.assertIn('отметка UIS', result['message_text'])

    def test_real_moscow_weekend_never_reads(self):
        client = FakeClient()
        for instant in [SATURDAY, dt.datetime(2026, 11, 6, 21, tzinfo=dt.timezone.utc)]:
            result = summary.collect(instant, client)
            self.assertFalse(result['should_send'])
            self.assertEqual(result['skip_reason'], 'weekend_msk')
        self.assertEqual(client.calls, [])

    def test_naive_clock_is_rejected(self):
        with self.assertRaises(ValueError):
            summary.collect(dt.datetime(2026, 11, 6), FakeClient())

    def test_no_running_campaigns_is_an_explicit_sendable_empty_state(self):
        result = summary.collect(FRIDAY, FakeClient(rows=[{'id': 1, 'status': 'paused'}]))
        self.assertTrue(result['should_send'])
        self.assertIn('сейчас нет', result['message_text'])
        self.assertEqual(result['campaigns'], [])

    def test_failed_calls_never_become_zero(self):
        client = FakeClient()
        original = client.request
        def fail(args, timeout):
            if args[1] == 'calls':
                raise summary.SourceError('source_failed')
            return original(args, timeout)
        client.request = fail
        result = summary.collect(FRIDAY, client)
        self.assertEqual(result['campaigns'][0]['status'], 'error')
        self.assertNotIn('attempts', result['campaigns'][0])
        self.assertIn('не ноль', result['message_text'])
        self.assertTrue(result['should_send'])

    def test_failed_list_is_failure_not_empty_campaign_report(self):
        client = FakeClient()
        client.request = lambda *args: (_ for _ in ()).throw(summary.SourceError('invalid_json'))
        result = summary.collect(FRIDAY, client)
        self.assertTrue(result['failures'])
        self.assertTrue(result['should_send'])
        self.assertIn('недоступен', result['message_text'])
        self.assertNotIn('сейчас нет', result['message_text'])

    def test_historical_proof_discloses_current_status_selection_without_delivery(self):
        day = dt.date(2026, 11, 6)
        for rows in [[{'id': 1, 'status': 'in_process'}],
                     [{'id': 1, 'status': 'paused'}, {'id': 2, 'status': 'completed'}]]:
            with self.subTest(rows=rows):
                client = FakeClient(rows=rows)
                result = summary.collect(SATURDAY, client, day=day)
                self.assertEqual(result['mode'], 'historical')
                self.assertFalse(result['should_send'])
                self.assertEqual(result['campaign_status_basis'], 'current')
                self.assertIn('сейчас в работе', result['message_text'])
                self.assertIn('Статусы на эту дату не восстановлены', result['message_text'])
                self.assertNotIn('Исторические данные за', result['message_text'])
                self.assertIn(str(day), result['message_text'])
                if rows[0]['status'] != 'in_process':
                    self.assertEqual(result['campaigns'], [])
                    self.assertIn('По фильтру списка', result['message_text'])
                    self.assertEqual(len(client.calls), 1)

    def test_refresh_is_bounded_and_once_for_concurrent_auth_failures(self):
        client = FakeClient(rows=[{'id': n, 'name': str(n), 'status': 'in_process'} for n in range(1, 5)])
        original = client.request
        simultaneous = threading.Barrier(3)
        def authenticated(args, timeout):
            if args[1] == 'calls' and not client.refreshes:
                simultaneous.wait(timeout=2)
                raise summary.SourceError('authentication_required', auth=True)
            return original(args, timeout)
        client.request = authenticated
        result = summary.collect(FRIDAY, client)
        self.assertEqual(client.refreshes, 1)
        self.assertTrue(all(row['status'] == 'ok' for row in result['campaigns']))

    def test_exhausted_deadline_starts_no_subprocess(self):
        client = FakeClient()
        result = summary.collect(FRIDAY, client, budget=0)
        self.assertEqual(client.calls, [])
        self.assertEqual(result['failures'][0]['code'], 'deadline_exceeded')

    def test_invalid_call_data_is_not_silently_ignored(self):
        for row in [{'talk_duration': ''}, {'talk_duration': float('nan')}, {'marks': [{'label': 'Перевод'}]}]:
            result = summary.collect(FRIDAY, FakeClient(grid=[row]))
            self.assertEqual(result['campaigns'][0]['status'], 'error')
            self.assertNotIn('attempts', result['campaigns'][0])

    def test_duplicate_campaign_id_does_not_duplicate_counts(self):
        result = summary.collect(FRIDAY, FakeClient(rows=[{'id': 1}, {'id': 1}]))
        self.assertEqual(result['failures'][0]['code'], 'duplicate_campaign_id')

    def test_raw_provider_and_cli_data_never_enter_error_output(self):
        client = summary.Cli('uis', 'test')
        for output in ['customer secret +79991234567', '{"ok":false,"error":{"message":"secret"}}']:
            with patch.object(subprocess, 'run', return_value=subprocess.CompletedProcess([], 1, output, 'private-token')):
                with self.assertRaises(summary.SourceError) as caught:
                    client.request(['autodial', 'list'], 10)
                self.assertNotIn('secret', str(caught.exception))
                self.assertNotIn('7999', str(caught.exception))

    def test_subprocess_has_an_explicit_remaining_timeout(self):
        client = summary.Cli('uis', 'test')
        with patch.object(subprocess, 'run', side_effect=subprocess.TimeoutExpired('uis', 3)) as run:
            with self.assertRaises(summary.SourceError):
                client.request(['autodial', 'list'], 3)
            self.assertEqual(run.call_args.kwargs['timeout'], 3)

    def test_campaign_names_cannot_expose_phone_or_markdown(self):
        result = summary.collect(FRIDAY, FakeClient(rows=[{'id': 1, 'name': '*+79991234567*', 'status': 'in_process'}]))
        self.assertNotIn('79991234567', result['message_text'])
        self.assertIn('номер скрыт', result['message_text'])


if __name__ == '__main__':
    unittest.main()
