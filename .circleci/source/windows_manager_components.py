"""Stage an exact, pinned Windows Manager component set, without executing it.

Callers supply the already-verified dependency closures (including app-local
Mesa for the viewer). This module does not discover imports or trust a build
receipt on its own. Each input directory must contain only its EXE and closure.
The destination must not exist; its parent must already exist. Publication is
an atomic, no-replace directory rename on Windows and Linux, or fails closed.
"""
import ctypes
import hashlib
import json
import os
import pathlib
import re
import shutil
import stat
import sys
import tempfile


MAX_DEPENDENCIES = 100
MAX_EXECUTABLE_BYTES = 64 << 20
MAX_DLL_BYTES = 256 << 20
MAX_TOTAL_BYTES = 768 << 20
MAX_MANIFEST_BYTES = 1 << 20
CHUNK_BYTES = 1 << 20
REPARSE_POINT = 0x400


class ComponentStagingError(Exception):
    """A fixed-category error; its receipt never contains paths or file bytes."""

    def __init__(self, code):
        super().__init__('Windows manager component staging failed: ' + code)
        self.receipt = {
            'schema_version': 1, 'passed': False,
            'stage': 'windows_manager_components', 'failure_code': code,
            'raw_paths_or_contents_published': False,
            'component_execution_performed': False,
            'source_or_binary_artifacts_published': False,
        }


def _require(condition, code):
    if not condition:
        raise ComponentStagingError(code)


def _safe_name(name, suffix):
    if not isinstance(name, str) or not re.fullmatch(
            r'[A-Za-z0-9_+-][A-Za-z0-9_.+-]{0,99}\.' + suffix, name, re.I):
        return False
    base = name.split('.', 1)[0].upper()
    return base not in {'CON', 'PRN', 'AUX', 'NUL'} and not re.fullmatch(r'(COM|LPT)[1-9]', base)


def _safe_destination_name(name):
    if not re.fullmatch(r'[A-Za-z0-9_+-][A-Za-z0-9_.+-]{0,99}', name) or name.endswith('.'):
        return False
    base = name.split('.', 1)[0].upper()
    return base not in {'CON', 'PRN', 'AUX', 'NUL'} and not re.fullmatch(r'(COM|LPT)[1-9]', base)


def _digest(value):
    return isinstance(value, str) and re.fullmatch(r'[0-9a-f]{64}', value) is not None


def _plain(info, directory=False):
    return (stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode)) and not (
        stat.S_ISLNK(info.st_mode) or getattr(info, 'st_file_attributes', 0) & REPARSE_POINT)


def _checked_path(path, directory=False):
    # Inspect the spelling before resolving it: resolving link/../file first
    # would hide a link in an ancestor. A genuine parent/../parent alias is OK.
    _require(path.is_absolute(), 'absolute_path_required')
    parts = path.parts
    current = pathlib.Path(parts[0])
    _require(_plain(current.lstat(), directory=True), 'unsafe_path')
    for index, part in enumerate(parts[1:], 1):
        current /= part
        info = current.lstat()
        _require(_plain(info, directory=directory or index < len(parts) - 1), 'unsafe_path')
    return path.lstat()


def _identity(info):
    return info.st_dev, info.st_ino


def _stamp(info):
    return _identity(info), info.st_size, info.st_mtime_ns, info.st_ctime_ns


def _cross_api_stamp(info):
    # CPython on Windows exposes birth time through path st_ctime_ns, but
    # metadata-change time through fstat st_ctime_ns (cpython issue #157671).
    # Use the explicitly named birth field only for cross-API comparison.
    # Full path and handle stamps are each still checked against their own
    # original observation, so a handle-side metadata change remains fatal.
    if os.name == 'nt':
        _require(hasattr(info, 'st_birthtime_ns'), 'input_changed')
        timestamp = info.st_birthtime_ns
    else:
        timestamp = info.st_ctime_ns
    return _identity(info), info.st_size, info.st_mtime_ns, timestamp


def _absent(path):
    try:
        path.lstat()
    except FileNotFoundError:
        return True
    return False


def _validated_inputs(executable, digest, dependencies):
    _require(_safe_name(executable.name, 'exe') and _digest(digest), 'invalid_executable_pin')
    _require(isinstance(dependencies, dict) and len(dependencies) <= MAX_DEPENDENCIES,
             'invalid_dependency_inventory')
    expected, seen = {executable.name: digest}, {executable.name.casefold()}
    # Copy the mapping before touching files; never silently merge collisions.
    for name, pin in list(dependencies.items()):
        _require(_safe_name(name, 'dll') and _digest(pin), 'invalid_dependency_pin')
        _require(name.casefold() not in seen, 'basename_case_collision')
        seen.add(name.casefold())
        expected[name] = pin
    return expected


def _hash_file(path, expected, limit, snapshot=None, target=None):
    info = _checked_path(path)
    _require(0 < info.st_size <= limit, 'file_size_limit')
    if snapshot is not None:
        _require(_stamp(info) == snapshot, 'input_changed')
    flags = os.O_RDONLY | getattr(os, 'O_BINARY', 0) | getattr(os, 'O_NOFOLLOW', 0) | getattr(os, 'O_NONBLOCK', 0)
    with os.fdopen(os.open(path, flags), 'rb') as source:
        opened = os.fstat(source.fileno())
        _require(_plain(opened) and _cross_api_stamp(opened) == _cross_api_stamp(info), 'input_changed')
        sink = target.open('xb') if target is not None else None
        try:
            size, digest = 0, hashlib.sha256()
            while True:
                chunk = source.read(min(CHUNK_BYTES, limit - size + 1))
                if not chunk:
                    break
                size += len(chunk)
                _require(size <= limit, 'file_size_limit')
                digest.update(chunk)
                if sink is not None:
                    sink.write(chunk)
            if sink is not None:
                sink.flush()
                os.fsync(sink.fileno())
            _require(size == info.st_size and _stamp(os.fstat(source.fileno())) == _stamp(opened), 'input_changed')
        finally:
            if sink is not None:
                sink.close()
    _require(_stamp(_checked_path(path)) == _stamp(info), 'input_changed')
    _require(digest.hexdigest() == expected, 'file_hash_mismatch')
    return info


def _inventory(executable, expected):
    directory = executable.parent
    parent = _checked_path(directory, directory=True)
    entry_info = _checked_path(executable)
    paths, seen = {}, set()
    # Bound enumeration before sorting; never materialize an unbounded listing.
    with os.scandir(directory) as entries:
        for entry in entries:
            _require(len(paths) < MAX_DEPENDENCIES + 1, 'entry_count_limit')
            _require(entry.name.casefold() not in seen, 'basename_case_collision')
            seen.add(entry.name.casefold())
            _require(entry.name in expected, 'unexpected_component_member')
            paths[entry.name] = directory / entry.name
    _require(paths.keys() == expected.keys(), 'missing_component_member')
    # Windows iterator and argument spellings can differ. Exact basename plus
    # OS file identity is required, rather than lexical Path equality.
    _require(paths[executable.name].samefile(executable), 'executable_identity_mismatch')
    result, total = {}, 0
    for name, path in sorted(paths.items()):
        executable_member = name == executable.name
        limit = MAX_EXECUTABLE_BYTES if executable_member else MAX_DLL_BYTES
        info = _checked_path(path)
        _require(0 < info.st_size <= limit, 'file_size_limit')
        total += info.st_size
        _require(total <= MAX_TOTAL_BYTES, 'aggregate_size_limit')
        if executable_member:
            _require(_identity(info) == _identity(entry_info), 'executable_identity_mismatch')
        _hash_file(path, expected[name], limit, snapshot=_stamp(info))
        result[name] = {'path': path, 'size': info.st_size, 'sha256': expected[name],
                        'executable': executable_member, 'stamp': _stamp(info)}
    _require(_identity(_checked_path(directory, directory=True)) == _identity(parent), 'input_changed')
    return result, total


def _publish_directory(staged, output):
    # os.rename on POSIX would overwrite a concurrently-created empty output
    # directory. Never fall back to that unsafe check-then-rename operation.
    if os.name == 'nt':
        os.rename(staged, output)
        return
    _require(sys.platform.startswith('linux'), 'atomic_publish_unavailable')
    rename = getattr(ctypes.CDLL(None, use_errno=True), 'renameat2', None)
    _require(rename is not None, 'atomic_publish_unavailable')
    rename.argtypes = [ctypes.c_int, ctypes.c_char_p, ctypes.c_int, ctypes.c_char_p, ctypes.c_uint]
    rename.restype = ctypes.c_int
    if rename(-100, os.fsencode(staged), -100, os.fsencode(output), 1) != 0:  # AT_FDCWD, RENAME_NOREPLACE
        raise ComponentStagingError('atomic_publish_failed')


def stage_manager_components(*, source, source_sha256, source_dependencies, source_version,
                             viewer, viewer_sha256, viewer_dependencies, viewer_version,
                             commit, output):
    """Return a source-free receipt after publishing source/, viewer/, manifest.json.

    source/viewer are absolute EXE paths; dependencies are exact basename to
    lower-case SHA-256 dicts, previously verified by the caller. Versions are
    bounded release identifiers and commit is a full lower-case Git SHA-1.
    Hash and size bounds apply again to inputs, copied files and the manifest.
    Errors have a safe ``receipt`` attribute; this helper writes no failure file.
    No existing destination (including an empty directory) can be reused.
    """
    staged = None
    try:
        _require(isinstance(commit, str) and re.fullmatch(r'[0-9a-f]{40}', commit), 'invalid_commit')
        for version in (source_version, viewer_version):
            _require(isinstance(version, str) and re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_.+-]{0,99}', version),
                     'invalid_version')
        source, viewer, output = pathlib.Path(source), pathlib.Path(viewer), pathlib.Path(output)
        _require(output.is_absolute() and _safe_destination_name(output.name), 'invalid_destination')
        expected = [_validated_inputs(source, source_sha256, source_dependencies),
                    _validated_inputs(viewer, viewer_sha256, viewer_dependencies)]
        _checked_path(source)
        _checked_path(viewer)
        _require(not source.samefile(viewer), 'cross_role_executable_alias')
        _require(source_sha256 != viewer_sha256, 'cross_role_executable_hash')
        output_parent = _checked_path(output.parent, directory=True)
        _require(_absent(output), 'destination_exists')
        source_dir, viewer_dir = source.parent.resolve(strict=True), viewer.parent.resolve(strict=True)
        resolved_output = output.parent.resolve(strict=True) / output.name
        _require(not source_dir.samefile(viewer_dir), 'input_directory_alias')
        for directory in (source_dir, viewer_dir):
            _require(not resolved_output.is_relative_to(directory) and not directory.is_relative_to(resolved_output),
                     'source_destination_alias')
        inventories, total = [], 0
        for executable, pins in zip((source, viewer), expected):
            inventory, size = _inventory(executable, pins)
            inventories.append(inventory)
            total += size
            _require(total <= MAX_TOTAL_BYTES, 'aggregate_size_limit')
        staged = pathlib.Path(tempfile.mkdtemp(prefix='.manager-components-', dir=output.parent))
        components = []
        for prefix, name, profile, executable, version, inventory in zip(
                ('source', 'viewer'), ('source-streamer', 'source-preview-viewer'),
                ('source-streamer-v1', 'source-preview-v1'), (source, viewer),
                (source_version, viewer_version), inventories):
            entry_dir = staged / prefix
            entry_dir.mkdir()
            files = []
            for basename, record in inventory.items():
                limit = MAX_EXECUTABLE_BYTES if record['executable'] else MAX_DLL_BYTES
                target = entry_dir / basename
                _hash_file(record['path'], record['sha256'], limit, snapshot=record['stamp'], target=target)
                copied = _hash_file(target, record['sha256'], limit)
                _require(copied.st_size == record['size'], 'copied_size_mismatch')
                files.append({'path': prefix + '/' + basename, 'sha256': record['sha256'],
                              'size': record['size'], 'executable': record['executable']})
            components.append({'name': name, 'platform': 'windows/amd64', 'version': version,
                               'profile': profile, 'entry': prefix + '/' + executable.name, 'files': files})
        # Recheck the complete closure, including member identities and bytes,
        # after copying so changed inputs cannot produce a success receipt.
        for executable, pins, original in zip((source, viewer), expected, inventories):
            current, _ = _inventory(executable, pins)
            _require(current == original, 'input_changed')
        raw = (json.dumps({'schema': 1, 'components': components}, indent=2, sort_keys=True) + '\n').encode('utf-8')
        _require(len(raw) <= MAX_MANIFEST_BYTES, 'manifest_size_limit')
        manifest_sha = hashlib.sha256(raw).hexdigest()
        manifest_path = staged / 'manifest.json'
        with manifest_path.open('xb') as manifest:
            manifest.write(raw)
            manifest.flush()
            os.fsync(manifest.fileno())
        _hash_file(manifest_path, manifest_sha, MAX_MANIFEST_BYTES)
        # Verify the staging tree itself has exactly the published inventory.
        root_members = set()
        with os.scandir(staged) as entries:
            for entry in entries:
                _require(entry.name in {'source', 'viewer', 'manifest.json'} and entry.name not in root_members,
                         'unexpected_staged_member')
                root_members.add(entry.name)
        _require(root_members == {'source', 'viewer', 'manifest.json'}, 'unexpected_staged_member')
        for prefix, executable, pins in zip(('source', 'viewer'), (source, viewer), expected):
            _inventory(staged / prefix / executable.name, pins)
        _require(_identity(_checked_path(output.parent, directory=True)) == _identity(output_parent), 'destination_changed')
        _require(_absent(output), 'destination_exists')
        _publish_directory(staged, output)
        staged = None
        return {'schema_version': 1, 'passed': True, 'stage': 'windows_manager_components',
                'commit': commit, 'manifest_sha256': manifest_sha, 'manifest_size': len(raw),
                'total_file_bytes': total, 'components': components,
                'raw_paths_or_contents_published': False, 'component_execution_performed': False,
                'source_or_binary_artifacts_published': False}
    except ComponentStagingError:
        raise
    except (OSError, ValueError, TypeError):
        # OS error strings can include absolute paths. Neither exception text
        # nor exception chaining is suitable for a publishable failure receipt.
        raise ComponentStagingError('staging_io_or_input_error') from None
    finally:
        if staged is not None:
            shutil.rmtree(staged, ignore_errors=True)
