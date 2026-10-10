#!/usr/bin/env python3
"""Stage verified dependency closure and run only the generated-input Windows gate."""
import argparse
import hashlib
import json
import os
import pathlib
import re
import subprocess

from windows_preview_build_receipt import stage_dependencies, sha, summarize_tests
from windows_component_inventory import component_files

SOURCE = '2e07af3484369bc68bc8091d5a04969867eff0f9'
ARCHIVE = '7a8ec9b04f0b3f090de55dc6fc7194e7bd7caf93a08a11182c7c03dcfce1ab2b'


FIXTURE_REQUIRED = {
    'TestExactArgvFailClosed', 'TestAssetHashSizeTypeAndSymlinkRejection',
    'TestAnnexBBoundaries', 'TestOggStructureAndChecksum', 'TestPacedRepeatedVideo',
    'TestPacedOggAndBoundedPCM', 'TestImmediateBrokenPipeAndCancellation',
    'TestWallClockPacing', 'TestRuntimeImportAndAPIAllowlist', 'TestChildBrokenPipe',
}
NATIVE_REQUIRED = {'TestWindowsAPILayouts',
                   'TestWindowsSuspendedLaunchPrivatePipesNaturalEOF',
                   'TestWindowsJobCloseKillsInheritedDescendant',
                   'TestWindowsParentCrashRetiresSuspendedChild'}


def summarize_fixture(path):
    allowed = {'TestAssetHashSizeTypeAndSymlinkRejection/symlink'}
    passed, skipped = set(), set()
    package_passed = False
    for line in path.read_text().splitlines():
        event = json.loads(line)
        action, name = event.get('Action'), event.get('Test')
        assert action != 'fail', 'generated encoder unit tests failed'
        if action == 'skip':
            assert name in allowed, 'unexpected generated encoder test skip'
            skipped.add(name)
        if action == 'pass':
            if name:
                passed.add(name)
            else:
                package_passed = True
    assert package_passed and FIXTURE_REQUIRED <= passed
    assert allowed <= passed | skipped, 'native symlink fixture did not execute'
    return {'passed': len(passed), 'required_passed': sorted(FIXTURE_REQUIRED),
            'skipped': sorted(skipped), 'symlink_rejection_native_tested': not skipped,
            'skip_reason': 'native unprivileged symlink creation unavailable' if skipped else None}


def main():
    parser = argparse.ArgumentParser()
    for name in ('work', 'output', 'ucrt-bin', 'runner'):
        parser.add_argument('--' + name, required=True, type=pathlib.Path)
    parser.add_argument('--commit', required=True)
    args = parser.parse_args()
    assert os.name == 'nt' and re.fullmatch('[0-9a-f]{40}', args.commit)
    work, out = args.work, args.output
    agent = work / 'agent/USBridgeAgent.exe'
    viewer = work / 'bin/source-preview-viewer.exe'
    fixture = work / 'fixture/ffmpeg-fixture.exe'
    source = work / 'components/bin/source-streamer.exe'
    for path in (agent, viewer, fixture, source, args.runner):
        assert path.is_absolute() and path.is_file() and not path.is_symlink()
    fixture_tests = summarize_fixture(work / 'tests/fixture.jsonl')
    native_tests = summarize_tests(work / 'tests/acceptance.jsonl', NATIVE_REQUIRED)
    build = json.loads((out / 'build.json').read_text())
    assert build['commit'] == args.commit and build['passed']
    assert build['viewer_sha256'] == sha(viewer)
    for name, digest in build['runtime_dlls_sha256'].items():
        assert sha(viewer.parent / name) == digest
    generated = json.loads((out / 'generated-encoder.json').read_text())
    assert generated['fixture_sha256'] == sha(fixture)
    assert generated['native_windows_fixture_exercised'] and not generated['desktop_capture_tested']
    assert generated['video']['frames'] == 900 and generated['video']['strict_decode']
    assert generated['audio']['pcm_bytes'] == 5759040 and generated['audio']['strict_decode']
    system = pathlib.Path(os.environ['SystemRoot']) / 'System32'
    agent_dlls, agent_system = stage_dependencies(agent, args.ucrt_bin, system)
    source_dlls, source_system = stage_dependencies(source, args.ucrt_bin, system)
    files = component_files(source, source_dlls, out / 'staging-failure.json')
    manifest = {'schema': 1, 'components': [{'name': 'source-streamer', 'platform': 'windows/amd64',
                'version': SOURCE, 'profile': 'source-streamer-v1', 'entry': 'bin/source-streamer.exe', 'files': files}]}
    raw = (json.dumps(manifest, indent=2) + '\n').encode()
    (work / 'components/manifest.json').write_bytes(raw)
    pins = {'agent': sha(agent), 'viewer': sha(viewer), 'fixture': sha(fixture), 'source': sha(source),
            'manifest': hashlib.sha256(raw).hexdigest(), 'fixture_manifest': sha(fixture.parent / 'fixture-assets.json')}
    receipt = {'schema_version': 1, 'commit': args.commit, 'source_streamer_commit': SOURCE,
               'source_archive_sha256': ARCHIVE, 'files_sha256': pins,
               'native_harness_tests': native_tests, 'generated_encoder_tests': fixture_tests,
               'agent_runtime_dlls_sha256': agent_dlls, 'source_runtime_dlls_sha256': source_dlls,
               'agent_system_dll_imports': agent_system, 'source_system_dll_imports': source_system,
               'generated_input_substitution': True, 'desktop_capture_tested': False,
               'source_or_binary_artifacts_published': False}
    (out / 'media-inputs.json').write_text(json.dumps(receipt, indent=2, sort_keys=True) + '\n')
    command = [str(args.runner), '--agent', str(agent), '--agent-sha256', pins['agent'],
               '--viewer', str(viewer), '--viewer-sha256', pins['viewer'],
               '--fixture', str(fixture), '--fixture-sha256', pins['fixture'],
               '--components', str(work / 'components'), '--manifest-sha256', pins['manifest'],
               '--source-sha256', pins['source'], '--fixture-manifest-sha256', pins['fixture_manifest'],
               '--work', str(work / 'native-media-state'), '--output', str(out / 'media.json'),
               '--commit', args.commit]
    subprocess.run(command, check=True, timeout=150)


if __name__ == '__main__':
    main()
