#!/usr/bin/env python3
"""Extract real small C preambles and emit only closed native boundary receipts."""
import argparse
import hashlib
import json
import os
import pathlib
import re

FILES = ('main_windows.go', 'environment.go', 'environment_windows.go',
         'environment_test.go', 'main_windows_test.go', 'graphics_environment.go',
         'graphics_environment_windows.go', 'graphics_environment_test.go', 'graphics_environment_windows_test.go')
REQUIRED = {'TestPrivateEventWriterIsolatesWindowsAndCRTOutput',
            'TestClearPreviewEnvironmentScrubsActualCRT',
            'TestClearPreviewEnvironmentReacquiresCRTArrays',
            'TestGraphicsEnvironmentOnlyFixedSoftwarePolicy', 'TestGraphicsEnvironmentFailureStopsConfiguration',
            'TestWindowsSoftwareGraphicsOverridesAreFixed'}


def extract(source):
    start = source.index('/*') + 2
    end = source.index('*/', start)
    assert source[end + 2:].lstrip().startswith('import "C"')
    return '\n'.join(line for line in source[start:end].splitlines()
                     if not line.startswith('#cgo ')) + '\n'


def summarize(path):
    counts = {name: 0 for name in REQUIRED}
    packages = 0
    for line in path.read_text().splitlines():
        row = json.loads(line)
        action, name = row.get('Action'), row.get('Test')
        assert action not in {'fail', 'skip', 'build-fail'}, 'boundary tests failed or skipped'
        if action == 'pass' and name in counts:
            counts[name] += 1
        elif action == 'pass' and not name:
            assert row.get('Package') == 'command-line-arguments'
            packages += 1
    assert packages == 1 and all(value == 5 for value in counts.values()), 'incomplete native boundary tests'
    return dict(sorted(counts.items()))


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--extract', action='store_true')
    parser.add_argument('--root', type=pathlib.Path, required=True)
    parser.add_argument('--work', type=pathlib.Path, required=True)
    parser.add_argument('--output', type=pathlib.Path)
    parser.add_argument('--commit')
    parser.add_argument('--elapsed-seconds', type=int)
    args = parser.parse_args()
    source = args.root / 'client/cmd/source-preview-viewer'
    if args.extract:
        for name in ('main_windows.go', 'environment_windows.go'):
            (args.work / (name + '.c')).write_text(extract((source / name).read_text()))
        return
    assert os.name == 'nt' and re.fullmatch('[0-9a-f]{40}', args.commit or '')
    assert args.output and not args.output.exists() and 0 <= args.elapsed_seconds <= 900
    tests = {mode: summarize(args.work / (mode + '.jsonl')) for mode in ('console', 'gui')}
    packages = {}
    for line in (args.work / 'packages.txt').read_text().splitlines():
        name, version = line.split(' ', 1)
        assert re.fullmatch('[A-Za-z0-9_+.-]{1,160}', name)
        assert re.fullmatch('[A-Za-z0-9_+:~.-]{1,160}', version)
        packages[name] = version
    assert len(packages) == 3
    receipt = {'schema_version': 1, 'source_commit': args.commit,
               'platform': 'windows/amd64', 'go_version': 'go1.26.9',
               'native_c_syntax_passed': True, 'actual_boundary_tests': tests,
               'elapsed_seconds': args.elapsed_seconds, 'toolchain_packages': packages,
               'source_files_sha256': {name: hashlib.sha256((source / name).read_bytes()).hexdigest() for name in FILES},
               'full_viewer_compiled': False, 'media_started': False,
               'window_opened': False, 'desktop_capture': False, 'input_injected': False,
               'source_or_binary_artifacts_published': False}
    args.output.write_text(json.dumps(receipt, indent=2, sort_keys=True) + '\n')


if __name__ == '__main__':
    main()
