#!/usr/bin/env python3
"""Publish only bounded numeric/metadata receipts, never source or archives."""
import argparse
import hashlib
import json
import pathlib
import re

PATTERNS = {
    'source-preview': re.compile(r'(?:result|pixels-[a-z0-9-]+|[a-z0-9-]+-pixels|viewer-[a-z0-9-]+|[a-z0-9-]+-gone|stop|expiry)\.json'),
    'source-preview-dialog': re.compile(r'(?:result|steps|last-ui|(?:dialog-stop|dialog-close|parent-close)-pixels-[0-9]+|(?:dialog-stop|dialog-close|parent-close)-viewer-gone)\.json'),
}
SECRET_FIELDS = {'key', 'key_b64', 'rikey', 'ri_key', 'private_key', 'password', 'token', 'descriptor'}


def check_metadata(value):
    if isinstance(value, dict):
        assert not (set(k.lower() for k in value) & SECRET_FIELDS), 'secret-like field in receipt'
        for child in value.values():
            check_metadata(child)
    elif isinstance(value, list):
        for child in value:
            check_metadata(child)
    elif isinstance(value, str):
        assert len(value) <= 16384, 'oversized receipt string'


def collect(root):
    destination = root / 'preview-evidence'
    destination.mkdir(exist_ok=True)
    assert not destination.is_symlink()
    files = {}
    for directory, pattern in PATTERNS.items():
        source = root / directory
        if not source.exists():
            continue
        assert source.is_dir() and not source.is_symlink()
        target = destination / directory
        target.mkdir(exist_ok=True)
        assert not target.is_symlink()
        for path in sorted(source.iterdir()):
            if not pattern.fullmatch(path.name):
                continue
            assert path.is_file() and not path.is_symlink() and path.stat().st_size <= 1 << 20
            value = json.loads(path.read_text())
            check_metadata(value)
            content = (json.dumps(value, indent=2, allow_nan=False) + '\n').encode()
            output = target / path.name
            assert not output.exists(), 'refuse receipt overwrite'
            output.write_bytes(content)
            files[output.relative_to(destination).as_posix()] = hashlib.sha256(content).hexdigest()
    receipt = {'schema_version': 1, 'source_or_binary_artifacts_published': False,
               'new_private_source_added': False, 'files_sha256': files}
    (destination / 'PUBLICATION-SCOPE.json').write_text(json.dumps(receipt, indent=2) + '\n')
    return receipt


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('artifacts', type=pathlib.Path)
    print(json.dumps(collect(parser.parse_args().artifacts)))
