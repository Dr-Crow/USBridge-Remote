#!/usr/bin/env python3
"""Exact key for public client Go caches; never inspect reconstructed snapshots."""
import argparse
import hashlib
import json
import pathlib
import re
import subprocess

AUXILIARY = ('.gitattributes', '.gitmodules', '.circleci/config.yml', '.circleci/setup-windows.ps1',
             '.circleci/test-source-preview-windows-build.sh',
             '.circleci/test-source-preview-windows-dependencies.sh',
             '.circleci/source/windows_viewer_cache_key.py')
GO_ARCHIVE = 'd722201a9c0c086d1610e111c48203009af690892ed708072bd5ae20160e7a59'
MSYS_ARCHIVE = 'ad336cccfda47758b5e15cda993fbba421115cb0b126697daef1ee4dfe37209f'
PUBLIC_CLIENT = 'a232e27d5c423eb8e7de8da91f0eefcf9171348f'


def inventory(raw):
    assert len(raw) <= 4 << 20
    rows = raw.decode('utf-8').split('\0')
    assert rows.pop() == ''
    paths = set()
    public_client = False
    for row in rows:
        match = re.fullmatch(r'(100644|100755|160000) (blob|commit) ([0-9a-f]{40})\t([^\r\n\t]+)', row)
        assert match, 'invalid public source inventory'
        mode, kind, pin, name = match.groups()
        path = pathlib.PurePosixPath(name)
        assert not path.is_absolute() and '..' not in path.parts
        assert name.startswith('client/') or name in AUXILIARY
        assert name not in paths
        paths.add(name)
        if kind == 'commit':
            assert mode == '160000' and name == 'client/moonlight-common-c' and pin == PUBLIC_CLIENT
            public_client = True
        else:
            assert mode in ('100644', '100755')
    assert public_client and {'client/go.mod', 'client/go.sum'} <= paths
    assert set(AUXILIARY[2:]) <= paths
    return hashlib.sha256(raw).hexdigest(), len(paths)


def key(raw, packages, go_version, cc_version):
    source_sha, count = inventory(raw)
    assert go_version == 'go version go1.26.9 windows/amd64'
    assert 0 < len(packages) <= 1 << 20 and 0 < len(cc_version) < 65536
    assert re.fullmatch(rb'(?:[A-Za-z0-9_+.-]{1,160} [A-Za-z0-9_+:~.-]{1,160}\r?\n)+', packages)
    facts = {'schema': 1, 'image': 'windows-server-2022-gui:2026.05.1',
             'platform': 'windows/amd64', 'go_version': go_version,
             'go_archive_sha256': GO_ARCHIVE, 'msys_archive_sha256': MSYS_ARCHIVE,
             'source_inventory_sha256': source_sha, 'source_entries': count,
             'native_packages_sha256': hashlib.sha256(packages).hexdigest(),
             'compiler_version_sha256': hashlib.sha256(cc_version).hexdigest(),
             'public_client_pin': PUBLIC_CLIENT,
             'go_flags': '-trimpath -mod=readonly', 'cgo': 1, 'cc': 'gcc', 'cxx': 'g++'}
    encoded = json.dumps(facts, sort_keys=True, separators=(',', ':')).encode()
    return hashlib.sha256(encoded).hexdigest(), facts


def main():
    p = argparse.ArgumentParser()
    p.add_argument('--root', type=pathlib.Path, required=True)
    p.add_argument('--work', type=pathlib.Path, required=True)
    a = p.parse_args()
    raw = subprocess.check_output(['git', 'ls-tree', '-rz', 'HEAD', '--', 'client', *AUXILIARY], cwd=a.root, timeout=30)
    version = subprocess.check_output(['go', 'version'], text=True, timeout=10).strip()
    compiler = subprocess.check_output(['gcc', '-v'], stderr=subprocess.STDOUT, timeout=10)
    digest, facts = key(raw, (a.work / 'packages.txt').read_bytes(), version, compiler)
    (a.work / 'public-cache-key.txt').write_text(digest + '\n')
    (a.work / 'public-cache-inputs.json').write_text(json.dumps(facts, indent=2, sort_keys=True) + '\n')
    print('Exact public viewer cache key prepared:', digest)


if __name__ == '__main__':
    main()
