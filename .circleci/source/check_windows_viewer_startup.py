#!/usr/bin/env python3
"""Verify the exact native viewer's owned window before component compilation."""
import argparse
import json
import os
import pathlib
import re
import subprocess

from windows_preview_build_receipt import sha


def checked_command(work, output, runner, commit):
    assert re.fullmatch('[0-9a-f]{40}', commit)
    receipt = json.loads((output / 'build.json').read_text())
    assert receipt['passed'] and receipt['viewer_compiled'] and receipt['commit'] == commit
    viewer = work / 'bin/source-preview-viewer.exe'
    for path in (viewer, runner):
        assert path.is_absolute() and path.is_file() and not path.is_symlink()
    assert sha(viewer) == receipt['viewer_sha256']
    for name, digest in receipt['runtime_dlls_sha256'].items():
        assert re.fullmatch(r'[A-Za-z0-9_.+-]{1,100}\.dll', name, re.I)
        path = viewer.parent / name
        assert path.is_file() and not path.is_symlink() and sha(path) == digest
    return [str(runner), '--window-startup', '--viewer', str(viewer),
            '--viewer-sha256', receipt['viewer_sha256'], '--commit', commit,
            '--work', str(work / 'native-window-startup-state'),
            '--output', str(output / 'window-startup.json')]


def main():
    p = argparse.ArgumentParser()
    for name in ('work', 'output', 'runner'):
        p.add_argument('--'+name, type=pathlib.Path, required=True)
    p.add_argument('--commit', required=True)
    a = p.parse_args()
    assert os.name == 'nt'
    subprocess.run(checked_command(a.work, a.output, a.runner, a.commit), check=True, timeout=30)


if __name__ == '__main__':
    main()
