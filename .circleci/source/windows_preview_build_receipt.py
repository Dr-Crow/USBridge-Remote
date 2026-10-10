#!/usr/bin/env python3
"""Resolve native PE imports and retain only bounded, source-free build proof."""
import argparse
import hashlib
import json
import os
import pathlib
import re
import shutil
import subprocess

PIN = 'a232e27d5c423eb8e7de8da91f0eefcf9171348f'
REQUIRED = {
    'service': {'TestSourcePreviewNativeRejectsPlaintextBeforeStart',
                'TestSourcePreviewNativeBlocksAllInputAndUplinks',
                'TestSourcePreviewWindowsNativeWireProfile',
                'TestSourcePreviewWindowsDecodesAudioWithoutWASAPI',
                'TestSourcePreviewWindowsViewOnlyCannotBecomeStock'},
    'command': {'TestPrivateEventWriterIsolatesWindowsAndCRTOutput',
                'TestClearPreviewEnvironmentScrubsActualCRT'},
    'command-gui': {'TestPrivateEventWriterIsolatesWindowsAndCRTOutput',
                    'TestClearPreviewEnvironmentScrubsActualCRT'},
}


def summarize_tests(path, required):
    passed, failed, skipped = set(), set(), set()
    package_passed = False
    for line in path.read_text().splitlines():
        value = json.loads(line)
        action = value.get('Action')
        name = value.get('Test')
        if action == 'fail':
            failed.add(name or 'package')
        if action == 'skip':
            skipped.add(name or 'package')
        if action == 'pass' and name:
            passed.add(name)
        if action == 'pass' and not name:
            package_passed = True
    assert not failed and not skipped, 'native preview tests failed or skipped'
    assert package_passed, 'native test package did not finish'
    assert passed and required <= passed, 'required native privacy test did not execute'
    return {'passed': len(passed), 'failed': 0, 'skipped': 0,
            'required_passed': sorted(required)}


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def stage_dependencies(binary, ucrt_bin, system32):
    queue, seen, copied, system = [binary], set(), {}, set()
    while queue:
        item = queue.pop()
        if item.name.lower() in seen:
            continue
        seen.add(item.name.lower())
        result = subprocess.run([str(ucrt_bin / 'objdump.exe'), '-p', str(item)],
                                check=True, capture_output=True, text=True, timeout=30)
        names = re.findall(r'DLL Name:\s*([^\r\n]+)', result.stdout)
        assert names, 'PE import inventory is absent'
        for name in names:
            name = name.strip()
            assert re.fullmatch(r'[A-Za-z0-9_.+-]{1,100}\.dll', name, re.I)
            source = ucrt_bin / name
            if source.is_file() and not source.is_symlink():
                target = binary.parent / name
                if target.exists() or target.is_symlink():
                    assert target.is_file() and not target.is_symlink(), 'invalid runtime DLL target'
                    assert sha(target) == sha(source), 'conflicting runtime DLL'
                else:
                    shutil.copyfile(source, target)
                copied[name] = sha(target)
                queue.append(target)
            else:
                assert name.lower().startswith(('api-ms-win-', 'ext-ms-win-')) or (system32 / name).is_file(), 'unresolved native DLL'
                system.add(name)
    return dict(sorted(copied.items())), sorted(system)


def main():
    parser = argparse.ArgumentParser()
    for name in ('work', 'output', 'ucrt-bin'):
        parser.add_argument('--' + name, required=True, type=pathlib.Path)
    parser.add_argument('--commit', required=True)
    parser.add_argument('--public-client-pin', required=True)
    args = parser.parse_args()
    assert os.name == 'nt' and re.fullmatch('[0-9a-f]{40}', args.commit)
    assert args.public_client_pin == PIN
    binary = args.work / 'bin/source-preview-viewer.exe'
    assert binary.is_file() and not binary.is_symlink()
    results = {name: summarize_tests(args.work / 'tests' / (name + '.jsonl'), REQUIRED.get(name, set()))
               for name in ('descriptor', 'service', 'command', 'command-gui')}
    system = pathlib.Path(os.environ['SystemRoot']) / 'System32'
    dependencies, system_imports = stage_dependencies(binary, args.ucrt_bin, system)
    packages = []
    for line in (args.work / 'packages.txt').read_text().splitlines():
        name, version = line.split(' ', 1)
        assert re.fullmatch('[A-Za-z0-9_+.-]{1,160}', name)
        assert re.fullmatch('[A-Za-z0-9_+:~.-]{1,160}', version)
        packages.append({'name': name, 'version': version})
    result = {'schema_version': 1, 'passed': True, 'commit': args.commit,
              'platform': 'windows/amd64', 'go_version': 'go1.26.9',
              'public_client_pin': PIN, 'viewer_sha256': sha(binary),
              'native_privacy_tests': results, 'runtime_dlls_sha256': dependencies,
              'system_dll_imports': system_imports, 'toolchain_packages': packages,
              'viewer_compiled': True, 'actual_media_tested': False,
              'actual_window_pixels_tested': False, 'desktop_capture_tested': False,
              'input_injection_tested': False, 'source_snapshot_changed': False,
              'source_or_binary_artifacts_published': False}
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(result, indent=2, sort_keys=True) + '\n')


if __name__ == '__main__':
    main()
