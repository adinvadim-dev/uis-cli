import copy
import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('operator_compare', Path(__file__).resolve().parents[1] / 'scripts/operator_compare.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


def snapshots():
    def row(key, kind, talk, employee=101):
        return {'communication_id': key, 'call_start_date_time': '2026-11-09 12:00:00',
                'employees': [{'entity_id': employee, 'entity_type': 'employee'}], 'call_type': {'value_id': kind},
                'call_direction': {'value_id': 'out'}, 'call_talk_duration': talk,
                'call_is_transfer': {'value_id': True}, 'contact_phone_number': {'value': 'PRIVATE_PHONE'}}
    rows = [row(1, 'auto_out_calls', '00:01:00'), row(2, 'sip', '00:00:00'), row(3, 'other', None), row(4, 'sip', '00:01:00', 202)]
    uis = {'ok': True, 'schemaVersion': 'uis.cli/v1', 'data': {'source': 'private.report_constructor',
        'date_from': '2026-11-09 00:00:00', 'date_till': '2026-11-09 18:00:00', 'data': rows,
        'coverage': {'complete': True, 'initial_offset': 0, 'reported_total': 4, 'returned': 4}}}
    transfers = {'ok': True, 'data': {'items': [{'id': 9, 'extUserId': '101', 'startTime': 1794214800000, 'phone': 'PRIVATE_PHONE'},
        {'id': 10, 'extUserId': '202', 'startTime': 1794214800000}], 'complete': True, 'nextAfterId': None}}
    return uis, transfers


class OperatorCompareTests(unittest.TestCase):
    def run_compare(self, uis, transfers):
        return module.compare(uis, transfers, '101', ['101'], 'Europe/Moscow')

    def test_independent_populations_and_unknowns_do_not_become_conversion(self):
        uis, transfers = snapshots()
        result = self.run_compare(uis, transfers)
        self.assertEqual(result['uis']['by_source']['uis_autodial']['conversations'], 1)
        self.assertEqual(result['uis']['by_source']['manual_sip']['calls'], 1)
        self.assertEqual(result['uis']['by_source']['unknown']['unknown_duration'], 1)
        self.assertEqual(result['uis']['other_employee_rows'], 1)
        self.assertEqual(result['medivey']['transfer_ids'], [9])
        self.assertEqual(result['medivey']['transfers'], 1)
        self.assertIn('not_established', result['per_call_links'])
        self.assertNotIn('PRIVATE_PHONE', str(result))

    def test_null_participants_and_partial_inputs_stay_visible(self):
        uis, transfers = snapshots()
        uis['data']['data'][0]['employees'] = None
        uis['data']['coverage']['complete'] = False
        transfers['data'].update(complete=False, nextAfterId=10)
        result = self.run_compare(uis, transfers)
        self.assertFalse(result['uis']['pagination_complete'])
        self.assertFalse(result['medivey']['pagination_complete'])
        self.assertEqual(result['uis']['unresolved_participant_rows'], 1)

    def test_complete_snapshot_does_not_hide_missing_participant_identity(self):
        uis, transfers = snapshots()
        uis['data']['data'][0]['employees'] = None
        result = self.run_compare(uis, transfers)
        self.assertTrue(result['uis']['pagination_complete'])
        self.assertIn('unknown', result['uis']['employee_period_coverage'])

    def test_full_pagination_does_not_certify_query_scope_or_transfer_period(self):
        uis, transfers = snapshots()
        uis['data']['query_scope'] = {'filter': {'field': 'call_scenario', 'operator': '=', 'value': ['Synthetic scenario']}}
        result = self.run_compare(uis, transfers)
        self.assertTrue(result['uis']['pagination_complete'])
        self.assertIn('unknown', result['uis']['employee_period_coverage'])
        # All transfer rows fit even if their source export actually ended earlier.
        self.assertTrue(result['medivey']['pagination_complete'])
        self.assertEqual(result['medivey']['employee_period_coverage'], 'unknown_query_bounds_and_filters_not_echoed')
        uis['data']['query_scope']['filter'] = None
        self.assertEqual(self.run_compare(uis, transfers)['uis']['employee_period_coverage'], 'complete_unfiltered_snapshot')

    def test_unknown_or_other_entity_namespace_is_not_an_employee_match(self):
        for cell in ({'entity_type': 'employee', 'entity_id': None}, {'entity_type': 'group', 'entity_id': 101}):
            uis, transfers = snapshots()
            uis['data']['data'][0]['employees'] = [cell]
            result = self.run_compare(uis, transfers)
            self.assertEqual(result['uis']['unresolved_participant_rows'], 1)
            self.assertEqual(result['uis']['by_source']['uis_autodial']['calls'], 0)

    def test_incoming_overrides_raw_call_type_and_mav_is_not_a_classifier(self):
        uis, transfers = snapshots()
        uis['data']['data'][0]['call_direction']['value_id'] = 'in'
        uis['data']['data'][1]['is_mass_auto_call_enabled'] = 'Да'
        result = self.run_compare(uis, transfers)
        self.assertEqual(result['uis']['by_source']['incoming']['calls'], 1)
        self.assertEqual(result['uis']['by_source']['uis_autodial']['calls'], 0)
        self.assertEqual(result['uis']['by_source']['manual_sip']['calls'], 1)

    def test_duplicates_out_of_period_and_inconsistent_coverage_are_rejected(self):
        for kind in ('duplicate_call', 'duplicate_transfer', 'wrong_period', 'bad_duration', 'coverage'):
            with self.subTest(kind=kind):
                uis, transfers = snapshots()
                if kind == 'duplicate_call':
                    uis['data']['data'].append(copy.deepcopy(uis['data']['data'][0]))
                elif kind == 'duplicate_transfer':
                    transfers['data']['items'].append(copy.deepcopy(transfers['data']['items'][0]))
                elif kind == 'wrong_period':
                    transfers['data']['items'][0]['startTime'] = 0
                elif kind == 'bad_duration':
                    uis['data']['data'][0]['call_talk_duration'] = '00:99:00'
                else:
                    uis['data']['coverage']['reported_total'] = 9
                with self.assertRaises(ValueError):
                    self.run_compare(uis, transfers)


if __name__ == '__main__':
    unittest.main()
