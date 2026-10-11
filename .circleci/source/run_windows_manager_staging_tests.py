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

EXPECTED_TESTS = frozenset({
    'test_altered_dependency_hash_or_bytes_is_rejected',
    'test_altered_executable_hash_is_rejected',
    'test_case_collision_in_allowlist_is_rejected',
    'test_case_collision_on_disk_is_rejected',
    'test_case_only_mismatch_does_not_weaken_allowlist',
    'test_changed_input_during_copy_is_rejected_and_temporary_tree_removed',
    'test_combined_component_aggregate_is_bounded',
    'test_corrupted_copied_file_is_rejected',
    'test_cross_role_hardlink_is_rejected',
    'test_dependency_count_is_bounded',
    'test_destination_file_and_dangling_symlink_are_rejected',
    'test_destination_parent_symlink_is_rejected',
    'test_destination_reuse_preserves_existing_content',
    'test_empty_and_huge_files_are_rejected_before_hashing',
    'test_empty_dependency_closure_is_valid',
    'test_exact_two_components_with_app_local_dlls_and_hashed_manifest',
    'test_executable_size_is_bounded',
    'test_handle_metadata_change_during_read_is_still_rejected',
    'test_invalid_destination_basenames_are_rejected',
    'test_invalid_hashes_versions_and_commit_are_rejected',
    'test_manifest_is_rehashed_before_publication',
    'test_missing_dependency_is_rejected',
    'test_new_input_member_during_copy_is_rejected',
    'test_no_destination_visible_until_complete_atomic_publication',
    'test_os_error_is_sanitized_without_exception_chaining',
    'test_publish_race_never_replaces_existing_empty_destination',
    'test_relative_paths_are_rejected',
    'test_reparse_file_and_ancestor_are_rejected_portably',
    'test_separate_cross_role_identical_executables_are_rejected',
    'test_size_diagnosis_clamps_only_output_not_acceptance',
    'test_source_alias_accepts_samefile_without_lexical_equality',
    'test_source_destination_aliasing_is_rejected',
    'test_successful_destination_cannot_be_reused',
    'test_symlink_executable_dependency_and_parent_are_rejected',
    'test_unknown_dll_is_rejected_in_both_components',
    'test_unknown_non_dll_or_subdirectory_is_rejected',
    'test_untrusted_dependency_names_are_rejected',
    'test_viewer_alias_accepts_samefile_without_lexical_equality',
    'test_viewer_size_diagnosis_preserves_bound_and_privacy',
    'test_windows_cross_api_birthtime_is_explicit_and_full_stamps_remain_distinct',
    'test_windows_cross_api_identity_size_write_and_birth_changes_are_rejected',
})

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


def metadata_comparison(path_info, handle_info):
    # Compare only observations already captured by the failing operation.
    # Never issue a second stat query or publish identifiers/timestamps.
    fields = ('st_dev', 'st_ino', 'st_size', 'st_mtime_ns', 'st_ctime_ns', 'st_birthtime_ns')
    return {name[3:] + '_equal': getattr(path_info, name, None) == getattr(handle_info, name, None)
            for name in fields} | {
        'path_birthtime_available': hasattr(path_info, 'st_birthtime_ns'),
        'handle_birthtime_available': hasattr(handle_info, 'st_birthtime_ns'),
        'path_ctime_equals_birthtime': getattr(path_info, 'st_ctime_ns', None) == getattr(path_info, 'st_birthtime_ns', -1),
        'handle_ctime_equals_birthtime': getattr(handle_info, 'st_ctime_ns', None) == getattr(handle_info, 'st_birthtime_ns', -1),
    }


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
    frame = tb
    while frame is not None:
        if frame.tb_frame.f_code.co_name == '_hash_file' and pathlib.Path(frame.tb_frame.f_code.co_filename).name == 'windows_manager_components.py':
            observed = frame.tb_frame.f_locals
            if 'opened' in observed and 'info' in observed:
                value['path_handle_metadata'] = metadata_comparison(observed['info'], observed['opened'])
        frame = frame.tb_next
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
    def identities(group):
        for item in group:
            if isinstance(item, unittest.TestSuite):
                yield from identities(item)
            else:
                yield test_name(item)
    discovered = list(identities(suite))
    manifest_matches = len(discovered) == len(EXPECTED_TESTS) and set(discovered) == EXPECTED_TESTS
    result = unittest.TextTestRunner(stream=io.StringIO(), resultclass=ClosedResult).run(suite)
    skips = [{'test': test_name(test), 'reason': reason if ALLOWED_SKIPS.get(test_name(test)) == reason else 'unclassified_skip'}
             for test, reason in result.skipped]
    passed = result.wasSuccessful() and manifest_matches and result.testsRun == len(EXPECTED_TESTS) and all(x['reason'] != 'unclassified_skip' for x in skips)
    receipt = {'schema_version': 1, 'commit': commit, 'passed': passed, 'tests_run': result.testsRun,
               'reviewed_test_manifest_matches': manifest_matches, 'expected_tests': len(EXPECTED_TESTS),
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
