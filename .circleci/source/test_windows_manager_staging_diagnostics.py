import json
import pathlib
import sys
import unittest
import types
import run_windows_manager_staging_tests as runner
from windows_manager_components import ComponentStagingError


class DiagnosticTests(unittest.TestCase):
    def id(self): return 'test_windows_manager_components.ManagerComponentTests.test_closed_diagnostic'
    def check_error(self, exception):
        try: raise exception
        except Exception: return runner.diagnostic(self, sys.exc_info(), 'error')

    def test_reviewed_suite_identity_manifest_matches(self):
        names = set(unittest.defaultTestLoader.getTestCaseNames(runner.test_windows_manager_components.ManagerComponentTests))
        self.assertEqual(names, runner.EXPECTED_TESTS)
        self.assertEqual(len(names), 41)
        self.assertNotIn('unclassified_test', names)

    def test_metadata_diagnostics_are_fixed_booleans_from_captured_values(self):
        left = types.SimpleNamespace(st_dev=771, st_ino=991, st_size=301, st_mtime_ns=101, st_ctime_ns=19, st_birthtime_ns=19)
        right = types.SimpleNamespace(**(vars(left) | {'st_ctime_ns': 20}))
        value = runner.metadata_comparison(left, right)
        self.assertIs(value['ctime_ns_equal'], False)
        self.assertIs(value['birthtime_ns_equal'], True)
        self.assertIs(value['path_ctime_equals_birthtime'], True)
        self.assertIs(value['handle_ctime_equals_birthtime'], False)
        self.assertTrue(all(type(v) is bool for v in value.values()))
        self.assertNotIn('771', json.dumps(value))
        self.assertNotIn('991', json.dumps(value))

    def test_paths_and_unrecognized_values_do_not_escape(self):
        for exception in (OSError('C:/private/user/secret'), ValueError('private content'), AssertionError("'secret' != 'payload'")):
            result = self.check_error(exception)
            encoded = json.dumps(result)
            self.assertNotIn('private', encoded)
            self.assertNotIn('secret', encoded)
            self.assertNotIn('payload', encoded)
            self.assertNotIn('compared_codes', result)

    def test_only_declared_staging_codes_are_retained(self):
        self.assertEqual(self.check_error(ComponentStagingError('input_changed'))['staging_code'], 'input_changed')
        self.assertNotIn('staging_code', self.check_error(ComponentStagingError('private_secret')))
        self.assertEqual(self.check_error(AssertionError("'input_changed' != 'file_hash_mismatch'"))['compared_codes'], ['input_changed','file_hash_mismatch'])

    def test_test_name_does_not_export_subtest_parameters(self):
        class Subtest:
            def id(self): return 'module.Tests.test_fixed (path="C:/private")'
        self.assertEqual(runner.test_name(Subtest()), 'test_fixed')


if __name__ == '__main__': unittest.main()
