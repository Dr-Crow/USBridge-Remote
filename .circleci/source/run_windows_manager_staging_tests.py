#!/usr/bin/env python3
"""Run the unchanged staging suite with bounded, path-free failure categories."""
import argparse
import io
import json
import pathlib
import re
import traceback
import unittest

import test_windows_manager_components
from windows_manager_components import ComponentStagingError

ALLOWED_SKIPS = {
    'test_case_collision_on_disk_is_rejected': 'case-insensitive filesystem cannot represent both basenames',
    'test_destination_file_and_dangling_symlink_are_rejected': 'native unprivileged symlink creation unavailable',
    'test_symlink_executable_dependency_and_parent_are_rejected': 'native unprivileged symlink creation unavailable',
    'test_destination_parent_symlink_is_rejected': 'native unprivileged symlink creation unavailable',
}
CODES = {'absolute_path_required','unsafe_path','file_size_limit','input_changed','file_hash_mismatch',
         'entry_count_limit','basename_case_collision','unexpected_component_member','missing_component_member',
         'executable_identity_mismatch','aggregate_size_limit','atomic_publish_unavailable','atomic_publish_failed',
         'invalid_commit','invalid_version','invalid_destination','invalid_executable_pin','invalid_dependency_inventory',
         'invalid_dependency_pin','cross_role_executable_alias','cross_role_executable_hash','destination_exists',
         'input_directory_alias','source_destination_alias','copied_size_mismatch','manifest_size_limit',
         'unexpected_staged_member','destination_changed','staging_io_or_input_error'}
EXCEPTIONS = {'AssertionError','FileNotFoundError','PermissionError','FileExistsError','OSError',
              'ValueError','TypeError','AttributeError','ComponentStagingError','NotImplementedError'}


def test_name(test):
    match = re.search(r'\.(test_[a-z0-9_]{1,128})(?:$|[ (])', test.id())
    return match.group(1) if match else 'unclassified_test'


def diagnostic(test, error, kind):
    cls, exception, tb = error
    value = {'test': test_name(test), 'kind': kind,
             'exception': cls.__name__ if cls.__name__ in EXCEPTIONS else 'unclassified_exception'}
    if isinstance(exception, ComponentStagingError):
        code = exception.receipt.get('failure_code')
        if code in CODES:
            value['staging_code'] = code
    if cls is AssertionError:
        # Only declared source error enums may survive an equality assertion.
        codes = re.findall(r"'([a-z_]{1,64})'", str(exception))
        if len(codes) == 2 and all(code in CODES for code in codes):
            value['compared_codes'] = codes
    locations = []
    for entry in traceback.extract_tb(tb):
        name = pathlib.Path(entry.filename).name
        if name in {'test_windows_manager_components.py','windows_manager_components.py'} and 0 < entry.lineno < 10000:
            locations.append({'file': name, 'line': entry.lineno})
    value['locations'] = locations[-8:]
    return value


class ClosedResult(unittest.TextTestResult):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.diagnostics = []

    def addError(self, test, error):
        self.diagnostics.append(diagnostic(test, error, 'error'))
        super().addError(test, error)

    def addFailure(self, test, error):
        self.diagnostics.append(diagnostic(test, error, 'failure'))
        super().addFailure(test, error)

    def addSubTest(self, test, subtest, error):
        if error is not None:
            self.diagnostics.append(diagnostic(test, error, 'failure' if issubclass(error[0], test.failureException) else 'error'))
        super().addSubTest(test, subtest, error)


def run(output, commit):
    if re.fullmatch('[0-9a-f]{40}', commit) is None or output.exists():
        raise SystemExit('Exact commit and fresh diagnostic output required')
    suite = unittest.defaultTestLoader.loadTestsFromModule(test_windows_manager_components)
    result = unittest.TextTestRunner(stream=io.StringIO(), resultclass=ClosedResult).run(suite)
    skips = [{'test': test_name(test), 'reason': reason if ALLOWED_SKIPS.get(test_name(test)) == reason else 'unclassified_skip'}
             for test, reason in result.skipped]
    passed = result.wasSuccessful() and result.testsRun == 36 and all(x['reason'] != 'unclassified_skip' for x in skips)
    receipt = {'schema_version': 1, 'commit': commit, 'passed': passed, 'tests_run': result.testsRun,
               'tests_passed': result.testsRun-len(result.failures)-len(result.errors)-len(skips),
               'failures': len(result.failures), 'errors': len(result.errors), 'skips': skips,
               'diagnostics': result.diagnostics[:128], 'raw_tracebacks_or_paths_published': False,
               'synthetic_files_only': True, 'component_execution_performed': False,
               'source_or_binary_artifacts_published': False}
    raw = (json.dumps(receipt, indent=2) + '\n').encode()
    if len(raw) > 65536:
        raise SystemExit('Diagnostic receipt exceeded bound')
    with output.open('xb') as target:
        target.write(raw)
    if not passed:
        raise SystemExit('Manager staging checks failed; see source-free test receipt')


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--output', required=True, type=pathlib.Path)
    parser.add_argument('--commit', required=True)
    args = parser.parse_args()
    run(args.output, args.commit)
