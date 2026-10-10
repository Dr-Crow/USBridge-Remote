#!/usr/bin/env python3
"""Job-local official Mesa DLL staging. Never installs a GPU or OS driver."""
import argparse
import hashlib
import json
import os
import pathlib
import re
import subprocess
import urllib.request

from windows_preview_build_receipt import sha, stage_dependencies

PACKAGE = 'mingw-w64-ucrt-x86_64-mesa'
VERSION = '26.2.4-1'
ARCHIVE = PACKAGE + '-' + VERSION + '-any.pkg.tar.zst'
URL = 'https://mirror.msys2.org/mingw/ucrt64/' + ARCHIVE
ARCHIVE_SHA = '82a30042848b6393f2a21cdee66b164e4cf4fe15a9721a5f1d1c7280e004ebdc'
DLLS = {
    'opengl32.dll': 'f73078a77b51c634faa36f616a8eb9f7f1b30f82b21d689267b9146dbbc8b943',
    'libgallium_wgl.dll': '42362a4b7063591ad1f2bf5d1dbf26fb808ac49cb419ac237d63c8f1cb49bc75',
}


def checked_files(directory, files):
    assert isinstance(files, dict) and 0 < len(files) <= 100
    total, seen = 0, set()
    for name, digest in files.items():
        assert name.lower() not in seen
        seen.add(name.lower())
        assert re.fullmatch(r'[A-Za-z0-9_.+-]{1,100}\.dll', name, re.I)
        assert re.fullmatch('[0-9a-f]{64}', digest)
        path = directory / name
        info = path.lstat()
        assert path.is_file() and not path.is_symlink()
        assert not getattr(info, 'st_file_attributes', 0) & 0x400
        assert 0 < info.st_size <= 256 << 20
        total += info.st_size
        assert total <= 768 << 20 and sha(path) == digest


def verified_viewer_dependencies(build, output, viewer):
    files = dict(build['runtime_dlls_sha256'])
    checked_files(viewer.parent, files)
    path = output / 'software-graphics-inputs.json'
    if path.exists():
        assert path.is_file() and not path.is_symlink() and path.stat().st_size <= 65536
        value = json.loads(path.read_text())
        assert value['schema_version'] == 1 and value['passed'] is True
        assert value['commit'] == build['commit'] and value['viewer_sha256'] == build['viewer_sha256']
        assert value['mesa_package'] == PACKAGE and value['mesa_version'] == VERSION
        assert value['mesa_archive_sha256'] == ARCHIVE_SHA
        assert value['fixed_driver'] == 'llvmpipe' and value['fixed_software'] is True
        merged = value['runtime_dlls_sha256']
        assert all(merged.get(name) == digest for name, digest in files.items())
        assert all(merged.get(name) == digest for name, digest in DLLS.items())
        checked_files(viewer.parent, merged)
        files = merged
    return files


def unchanged_existing_packages(before, after):
    def parse(raw):
        result = {}
        for line in raw.splitlines():
            assert re.fullmatch('[A-Za-z0-9_+.-]{1,160} [A-Za-z0-9_+:~.-]{1,160}', line)
            key, value = line.split(' ', 1)
            assert key not in result
            result[key] = value
        assert result
        return result
    old, new = parse(before), parse(after)
    assert all(new.get(k) == v for k, v in old.items()), 'existing compiler/runtime package changed'
    return [{'name': k, 'version': v} for k, v in sorted(new.items()) if k not in old]


def extract_pinned(archive, directory):
    assert archive.is_file() and not archive.is_symlink() and archive.stat().st_size <= 20 << 20
    assert sha(archive) == ARCHIVE_SHA
    for name, digest in DLLS.items():
        target = directory / name
        assert not target.exists() and not target.is_symlink()
        # Read only two exact regular-file payloads from a pinned official
        # archive; never extract paths, package hooks, executables or settings.
        with target.open('xb') as out:
            subprocess.run(['tar', '--force-local', '-xOf', str(archive), 'ucrt64/bin/' + name],
                           stdout=out, check=True, timeout=30)
        checked_files(directory, {name: digest})


def main():
    p = argparse.ArgumentParser()
    for name in ('work', 'output', 'ucrt-bin'):
        p.add_argument('--' + name, type=pathlib.Path, required=True)
    p.add_argument('--commit', required=True)
    a = p.parse_args()
    assert os.name == 'nt' and re.fullmatch('[0-9a-f]{40}', a.commit)
    build = json.loads((a.output / 'build.json').read_text())
    viewer = a.work / 'bin/source-preview-viewer.exe'
    assert build['passed'] and build['commit'] == a.commit and sha(viewer) == build['viewer_sha256']
    checked_files(viewer.parent, build['runtime_dlls_sha256'])
    installed = subprocess.check_output(['pacman', '-Q'], text=True, timeout=30)
    additions = unchanged_existing_packages((a.work / 'packages.txt').read_text(), installed)
    archive = a.work / ARCHIVE
    with urllib.request.urlopen(URL, timeout=60) as response:
        raw = response.read((20 << 20) + 1)
    assert len(raw) <= 20 << 20 and hashlib.sha256(raw).hexdigest() == ARCHIVE_SHA
    with archive.open('xb') as out: out.write(raw)
    extract_pinned(archive, viewer.parent)
    extras = {name: viewer.parent / name for name in DLLS}
    dependencies, system = stage_dependencies(extras['opengl32.dll'], a.ucrt_bin,
                                             pathlib.Path(os.environ['SystemRoot']) / 'System32', extras)
    dependencies['opengl32.dll'] = DLLS['opengl32.dll']
    merged = dict(build['runtime_dlls_sha256'])
    for name, digest in dependencies.items():
        assert name not in merged or merged[name] == digest
        merged[name] = digest
    checked_files(viewer.parent, merged)
    result = {'schema_version': 1, 'passed': True, 'commit': a.commit,
              'viewer_sha256': build['viewer_sha256'], 'mesa_package': PACKAGE, 'mesa_version': VERSION,
              'mesa_archive_sha256': ARCHIVE_SHA, 'mesa_archive_url': URL,
              'fixed_driver': 'llvmpipe', 'fixed_software': True,
              'runtime_dlls_sha256': dict(sorted(merged.items())),
              'system_dll_imports': system, 'additional_job_local_packages': additions,
              'existing_compiler_packages_unchanged': True, 'os_driver_installed': False,
              'source_or_binary_artifacts_published': False}
    (a.output / 'software-graphics-inputs.json').write_text(json.dumps(result, indent=2, sort_keys=True) + '\n')
    (a.work / 'probe-staging.json').write_text(json.dumps({'schema_version': 1,
                              'runtime_dlls_sha256': result['runtime_dlls_sha256']}, sort_keys=True) + '\n')
    print('Verified official Mesa app-local DLLs staged; no system driver installed.')


if __name__ == '__main__':
    main()
