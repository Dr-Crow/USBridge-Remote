#!/usr/bin/env python3
"""One-way, read-only public Go cache seed into the nonpublished job-local cache.

Run after public-cache save, before media/agent builds, with no concurrent Go
commands. No module caches, links, executable-cache directories or source trees.
The native entry point has fixed cache paths; path arguments exist only in the
portable core so its fail-closed behavior can be tested without Windows.
"""
import argparse
import hashlib
import json
import os
import pathlib
import re
import stat
import subprocess
from contextlib import contextmanager
from dataclasses import dataclass

from windows_viewer_cache_key import AUXILIARY, key

SOURCE = r'C:\ci\public-viewer-go-build'
DESTINATION = r'C:\ci\go-build'
MAX_FILES = 200_000
MAX_BYTES = 16 << 30
MAX_FILE_BYTES = 512 << 20
METADATA = {'README', 'trim.txt'}
REPARSE_POINT = getattr(stat, 'FILE_ATTRIBUTE_REPARSE_POINT', 0x400)
NAME = re.compile(r'([0-9a-f]{64})-([ad])')
ACTION = re.compile(rb'v1 ([0-9a-f]{64}) ([0-9a-f]{64}) ([ 0-9]{20}) ([ 0-9]{20})\n')


class SeedError(ValueError):
    pass


def require(ok, message):
    if not ok:
        raise SeedError(message)


def signature(info):
    return (info.st_dev, info.st_ino, info.st_mode, info.st_nlink,
            info.st_size, info.st_mtime_ns,
            getattr(info, 'st_file_attributes', 0))


def checked_stat(path, directory=False):
    info = path.lstat()
    require(not stat.S_ISLNK(info.st_mode) and
            not getattr(info, 'st_file_attributes', 0) & REPARSE_POINT,
            'links and reparse points are forbidden')
    require((stat.S_ISDIR if directory else stat.S_ISREG)(info.st_mode),
            'nonstandard cache entry type')
    if not directory:
        require(info.st_nlink == 1, 'hard-linked files are forbidden')
    return info


def check_directory(path):
    for part in reversed((path, *path.parents)):
        checked_stat(part, directory=True)


@contextmanager
def read_regular(path):
    check_directory(path.parent)
    before = checked_stat(path)
    fd = os.open(path, os.O_RDONLY | getattr(os, 'O_BINARY', 0) |
                 getattr(os, 'O_NOFOLLOW', 0))
    with os.fdopen(fd, 'rb') as handle:
        require(signature(before) == signature(os.fstat(handle.fileno())),
                'file changed while opening')
        yield handle, before
        require(signature(before) == signature(os.fstat(handle.fileno())) ==
                signature(checked_stat(path)), 'file changed while reading')


def small_read(path, limit):
    with read_regular(path) as (handle, info):
        require(info.st_size <= limit, 'oversized control file')
        value = handle.read(limit + 1)
        require(len(value) <= limit, 'oversized control file')
        return value


@dataclass(frozen=True)
class Entry:
    size: int
    sha256: str
    signature: tuple


def inspect_file(path, cache_name=None, sink=None):
    with read_regular(path) as (handle, info):
        limit = MAX_FILE_BYTES if cache_name else 4096
        require(info.st_size <= limit, 'oversized cache file')
        digest, size, header = hashlib.sha256(), 0, b''
        for block in iter(lambda: handle.read(1 << 20), b''):
            size += len(block)
            require(size <= limit, 'oversized cache file')
            digest.update(block)
            if cache_name and cache_name.endswith('-a'):
                require(size <= 175, 'invalid action metadata')
                header += block
            if sink:
                sink.write(block)
        require(size == info.st_size, 'cache file size changed')
        sha = digest.hexdigest()
        if cache_name:
            if cache_name.endswith('-d'):
                require(sha == cache_name[:64], 'data hash does not match Go output ID')
            else:
                match = ACTION.fullmatch(header)
                require(match is not None and match[1].decode() == cache_name[:64],
                        'invalid action metadata')
                for field in match.groups()[2:]:
                    require(field.strip().isdigit(), 'invalid action number')
                    number = int(field)
                    require(number <= (1 << 63) - 1 and f'{number:20d}'.encode() == field,
                            'invalid action number')
        return Entry(size, sha, signature(info))


def scan(cache):
    check_directory(cache)
    files, directories, total = {}, {'': signature(checked_stat(cache, True))}, 0
    for top in sorted(cache.iterdir()):
        if top.name in METADATA:
            files[top.name] = inspect_file(top)
            continue
        require(re.fullmatch(r'[0-9a-f]{2}', top.name), 'unexpected cache root entry')
        directories[top.name] = signature(checked_stat(top, True))
        for path in sorted(top.iterdir()):
            match = NAME.fullmatch(path.name)
            require(match is not None and path.name[:2] == top.name,
                    'invalid cache filename or shard')
            require(len(files) < MAX_FILES, 'too many cache files')
            entry = inspect_file(path, path.name)
            total += entry.size
            require(total <= MAX_BYTES, 'cache byte limit exceeded')
            files[top.name + '/' + path.name] = entry
    return files, directories


def aggregate(snapshot):
    files, directories = snapshot
    rows = [('dir', name) for name in sorted(directories)]
    rows += [('file', name, value.size, value.sha256) for name, value in sorted(files.items())]
    return hashlib.sha256(json.dumps(rows, separators=(',', ':')).encode()).hexdigest()


def verify_key(root, work):
    """Recompute every original fact from fresh commands, never a saved inventory."""
    saved_key = small_read(work / 'public-cache-key.txt', 66)
    require(re.fullmatch(rb'[0-9a-f]{64}\r?\n', saved_key), 'invalid saved cache key')
    raw = subprocess.check_output(
        ['git', 'ls-tree', '-rz', 'HEAD', '--', 'client', *AUXILIARY], cwd=root, timeout=30)
    packages = subprocess.check_output(['pacman', '-Q'], timeout=30)
    version = subprocess.check_output(['go', 'version'], text=True, timeout=10).strip()
    compiler = subprocess.check_output(['gcc', '-v'], stderr=subprocess.STDOUT, timeout=10)
    digest, facts = key(raw, packages, version, compiler)
    saved_facts = json.loads(small_read(work / 'public-cache-inputs.json', 16384))
    require(saved_key.decode().strip() == digest, 'public cache key no longer matches')
    require(json.dumps(saved_facts, sort_keys=True) == json.dumps(facts, sort_keys=True),
            'public cache facts no longer match')
    require(small_read(work / 'packages.txt', 1 << 20) == packages,
            'native package inventory changed')
    return digest


def seed_cache(source, destination, digest):
    require(re.fullmatch(r'[0-9a-f]{64}', digest), 'invalid verified cache key')
    source, destination = pathlib.Path(source).absolute(), pathlib.Path(destination).absolute()
    require(source != destination and source not in destination.parents and
            destination not in source.parents, 'source and destination overlap')
    check_directory(source)
    check_directory(destination.parent)
    if destination.exists() or destination.is_symlink():
        check_directory(destination)
        require(not os.path.samefile(source, destination), 'source and destination alias')
    before = scan(source)
    public = {name: entry for name, entry in before[0].items() if '/' in name}
    require(public, 'public cache is empty')
    existing = scan(destination)[0] if destination.exists() else {}
    # Preflight all existing files before copying. Never overwrite either kind.
    for name, entry in public.items():
        if name in existing and name.endswith('-d'):
            require(existing[name].sha256 == entry.sha256 and existing[name].size == entry.size,
                    'destination data conflicts with public cache')
    if not destination.exists():
        destination.mkdir()
    copied_files, copied_bytes, reused_files = 0, 0, 0
    for name, entry in sorted(public.items(), key=lambda item: (item[0].endswith('-a'), item[0])):
        target = destination / name
        if name in existing:
            # Existing action mappings may legitimately differ; retain them.
            checked_stat(target)
            reused_files += 1
            continue
        check_directory(destination)
        if not target.parent.exists():
            target.parent.mkdir()
        check_directory(target.parent)
        # Exclusive create: a late collision fails; it never overwrites a file.
        with target.open('xb') as handle:
            actual = inspect_file(source / name, target.name, sink=handle)
        require(actual == entry, 'source changed during copy')
        copied = inspect_file(target, target.name)
        require((copied.size, copied.sha256) == (entry.size, entry.sha256),
                'copied content verification failed')
        copied_files += 1
        copied_bytes += copied.size
    after = scan(source)
    require(before == after, 'public cache changed during seed')
    return {'schema': 1, 'passed': True, 'public_cache_key': digest,
            'public_cache_sha256': aggregate(before), 'source_unchanged': True,
            'source_after_sha256': aggregate(after),
            'public_files': len(public), 'public_bytes': sum(e.size for e in public.values()),
            'copied_files': copied_files, 'copied_bytes': copied_bytes,
            'retained_files': reused_files, 'skipped_metadata_files': len(before[0]) - len(public)}


def native_paths():
    require(os.name == 'nt', 'native Windows is required')
    require(pathlib.PureWindowsPath(os.environ.get('GOCACHE', '')) ==
            pathlib.PureWindowsPath(DESTINATION), 'job-local GOCACHE is required')
    require(pathlib.PureWindowsPath(os.environ.get('GOMODCACHE', '')) !=
            pathlib.PureWindowsPath(r'C:\ci\public-viewer-modules'),
            'public module cache must not be active')
    return pathlib.Path(SOURCE), pathlib.Path(DESTINATION)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=pathlib.Path, required=True)
    args = parser.parse_args()
    try:
        require(__debug__, 'optimized Python is not supported')
        source, destination = native_paths()
        root = args.root.absolute()
        work = root / '.source-preview-windows-build'
        output = root / 'artifacts/windows-preview-evidence/public-cache-seed.json'
        check_directory(root)
        check_directory(output.parent)
        require(not output.exists() and not output.is_symlink(), 'seed receipt already exists')
        commit = os.environ.get('CIRCLE_SHA1', '')
        require(re.fullmatch(r'[0-9a-f]{40}', commit), 'exact CI commit is required')
        actual = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root,
                                         text=True, timeout=10).strip()
        require(actual == commit, 'checkout differs from exact CI commit')
        digest = verify_key(root, work)
        receipt = seed_cache(source, destination, digest)
        receipt['source_commit'] = commit
        check_directory(output.parent)
        with output.open('x', encoding='utf-8') as handle:
            json.dump(receipt, handle, sort_keys=True, indent=2)
            handle.write('\n')
        print('Verified public Go build-cache seed complete; public source cache unchanged.')
    except SeedError as error:
        raise SystemExit(f'Public cache seed rejected: {error}.') from None
    except (OSError, ValueError, AssertionError, subprocess.SubprocessError):
        # Do not emit paths, filenames, subprocess output or cache contents.
        raise SystemExit('Public cache seed rejected; no success receipt produced.') from None


if __name__ == '__main__':
    main()
