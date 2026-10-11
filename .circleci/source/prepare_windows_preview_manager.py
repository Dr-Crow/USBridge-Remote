#!/usr/bin/env python3
"""Stage the two production Manager roles only after the exact CLI media proof.

No process is launched here. Files remain in job-local work; the only output is
an explicit manifest/hash receipt. The actual Manager gate is separately opt-in.
"""
import argparse
import hashlib
import json
import os
import pathlib
import re

from windows_manager_components import (stage_manager_components, ComponentStagingError,
    _checked_path, _hash_file, _identity, _publish_directory)
from windows_software_gl import verified_viewer_dependencies

SOURCE = '2e07af3484369bc68bc8091d5a04969867eff0f9'
CASES = {'window_close', 'stdin_eof', 'stdin_extra'}


def require(value, code):
    if not value:
        raise ComponentStagingError(code)


def read_receipt(path):
    require(path.is_file() and not path.is_symlink() and 0 < path.stat().st_size <= 65536,
            'invalid_prerequisite_receipt')
    def unique(pairs):
        result = {}
        for key, value in pairs:
            require(key not in result, 'duplicate_prerequisite_field')
            result[key] = value
        return result
    try:
        value = json.loads(path.read_bytes(), object_pairs_hook=unique)
        require(isinstance(value, dict), 'prerequisite_object_required')
        return value
    except (ValueError, UnicodeError):
        raise ComponentStagingError('invalid_prerequisite_json') from None


def validate_media(media, inputs, build, commit):
    require(re.fullmatch(r'[0-9a-f]{40}', commit) is not None, 'invalid_commit')
    require(all(r.get('schema_version') == 1 and r.get('commit') == commit for r in (media, inputs, build)),
            'prerequisite_commit_mismatch')
    require(media.get('passed') is True and media.get('platform') == 'windows/amd64' and
            media.get('source_streamer_commit') == SOURCE and inputs.get('source_streamer_commit') == SOURCE,
            'media_prerequisite_missing')
    for key in ('actual_agent_cli_supervision', 'actual_media_tested', 'actual_owned_window_pixels_tested',
                'job_containment', 'fresh_restart_verified'):
        require(media.get(key) is True, 'media_prerequisite_incomplete')
    for key in ('actual_agent_app_new', 'physical_display_presentation_tested', 'desktop_capture_tested',
                'input_injection_tested', 'windows_agent_preview_manager_enabled', 'source_snapshot_changed',
                'source_or_binary_artifacts_published', 'pixels_saved'):
        require(media.get(key) is False, 'unexpected_prerequisite_scope')
    require(build.get('passed') is True and build.get('viewer_compiled') is True and
            build.get('platform') == 'windows/amd64' and build.get('go_version') == 'go1.26.9',
            'viewer_build_prerequisite_missing')
    require(media.get('viewer_sha256') == build.get('viewer_sha256'), 'viewer_prerequisite_identity')
    pins = inputs.get('files_sha256')
    require(isinstance(pins, dict), 'missing_media_pins')
    for role in ('viewer', 'source', 'fixture'):
        value = pins.get(role)
        require(isinstance(value, str) and re.fullmatch(r'[0-9a-f]{64}', value) and
                media.get(role + '_sha256') == value, 'media_pin_mismatch')
    cases = media.get('cases')
    require(isinstance(cases, list) and len(cases) == 3, 'media_case_count')
    seen = set()
    for case in cases:
        require(isinstance(case, dict) and case.get('name') in CASES and case['name'] not in seen,
                'media_case_identity')
        seen.add(case['name'])
        for key in ('passed', 'typed_first_frame', 'owned_window_blue', 'owned_window_orange',
                    'natural_cleanup', 'job_empty_before_safety_close', 'owned_window_removed',
                    'advertised_listeners_closed'):
            require(case.get(key) is True, 'media_case_unverified')
        require(case.get('safety_job_kill_used') is False, 'media_forced_cleanup')
        for key in ('owned_window_color_transitions', 'owned_window_samples'):
            require(type(case.get(key)) is int and 2 <= case[key] <= 1500, 'media_pixel_counts')
    return pins



def retire_verified_graphics_probe(work, output):
    """Relocate only the exact naturally retired WGL fixture, never ignore extras."""
    proof = read_receipt(output / 'software-graphics-probe.json')
    require(proof.get('schema_version') == 1 and proof.get('role') == 'owned-wgl-probe-only',
            'graphics_probe_identity')
    for key in ('passed', 'fixed_software', 'natural_cleanup', 'job_empty_before_safety_close'):
        require(proof.get(key) is True, 'graphics_probe_not_retired')
    for key in ('safety_job_kill_used', 'actual_viewer_tested', 'actual_window_pixels_tested',
                'desktop_capture_tested', 'input_injection_tested'):
        require(proof.get(key) is False, 'graphics_probe_unexpected_scope')
    require(proof.get('fixed_driver') == 'llvmpipe', 'graphics_probe_identity')
    digest = proof.get('probe_sha256')
    require(isinstance(digest, str) and re.fullmatch(r'[0-9a-f]{64}', digest), 'graphics_probe_identity')
    source = work / 'bin/windows-graphics-probe.exe'
    before = _hash_file(source, digest, 32 << 20)
    # A fresh sibling holding directory prevents overwrite. A rename preserves
    # the exact file identity and no bytes are removed or reclassified as DLLs.
    holding = work / 'retired-graphics-probe'
    try:
        holding.mkdir()
        _checked_path(holding, directory=True)
        target = holding / 'windows-graphics-probe.exe'
        _publish_directory(source, target)  # atomic no-replace also applies to files
        after = _hash_file(target, digest, 32 << 20)
        require(_identity(after) == _identity(before) and after.st_size == before.st_size,
                'graphics_probe_identity')
    except OSError:
        raise ComponentStagingError('graphics_probe_retirement_io') from None
    return {'sha256': digest, 'natural_cleanup_verified': True, 'identity_preserved': True,
            'relocated_outside_component': True, 'deleted': False}


def prepare(work, output, commit):
    media = read_receipt(output / 'media.json')
    inputs = read_receipt(output / 'media-inputs.json')
    build = read_receipt(output / 'build.json')
    pins = validate_media(media, inputs, build, commit)
    viewer = work / 'bin/source-preview-viewer.exe'
    source = work / 'components/bin/source-streamer.exe'
    dependencies = verified_viewer_dependencies(build, output, viewer)
    retired_probe = retire_verified_graphics_probe(work, output)
    receipt = stage_manager_components(
        source=source, source_sha256=pins['source'],
        source_dependencies=inputs.get('source_runtime_dlls_sha256'), source_version=SOURCE,
        viewer=viewer, viewer_sha256=pins['viewer'], viewer_dependencies=dependencies,
        viewer_version=commit, commit=commit, output=work / 'manager-components')
    receipt['exact_cli_media_receipt_sha256'] = hashlib.sha256((output / 'media.json').read_bytes()).hexdigest()
    receipt['retired_graphics_probe'] = retired_probe
    receipt['production_manager_executed'] = False
    receipt['actual_app_new'] = False
    raw = (json.dumps(receipt, indent=2, sort_keys=True) + '\n').encode()
    require(len(raw) <= 65536, 'manager_staging_receipt_bound')
    with (output / 'manager-staging.json').open('xb') as target:
        target.write(raw)
    return receipt


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--work', required=True, type=pathlib.Path)
    parser.add_argument('--output', required=True, type=pathlib.Path)
    parser.add_argument('--commit', required=True)
    args = parser.parse_args()
    require(os.name == 'nt', 'native_windows_required')
    try:
        prepare(args.work, args.output, args.commit)
    except ComponentStagingError as error:
        with (args.output / 'manager-staging-failure.json').open('xb') as target:
            target.write((json.dumps(error.receipt, indent=2) + '\n').encode())
        raise SystemExit('Manager staging failed; inspect the closed failure receipt') from None


if __name__ == '__main__':
    main()
