"""Non-sensitive receipt schema and sandbox preflight for the real CLI gate."""
import json
import os
import pathlib
import re
import socket
import stat

BASE_IMAGE_SHA256 = '08ea48a03a3e78ebc7cd526e6a275053223aadd88bfc09cc49b06d5281525fde'

FLAGS = {
    'passed', 'agent_cli_app_startup_tested', 'plain_cli_startup_tested',
    'native_pointer_keyboard_events', 'actual_native_viewer', 'presented_pixels_verified',
    'stop_joined', 'dialog_close_joined', 'parent_close_to_tray_joined', 'quit_joined',
    'usbpass_bound_before_preview', 'usbpass_released_only_on_exit',
    'baseline_restored_after_each_preview', 'no_capture_before_approval',
    'invalid_manifest_rejected', 'consent_consumed', 'repeated_start_no_overlap',
    'stock_engine_idle_throughout', 'no_account_enrollment', 'read_only_ui_observer',
    'private_pid_proc_mount_ipc_network', 'network_none', 'no_external_interface',
    'no_default_route', 'external_egress_blocked', 'read_only_root',
    'no_host_devices_or_sockets', 'unprivileged_zero_caps_no_new_privileges',
    'generated_display_only', 'private_ephemeral_state', 'sandbox_helpers_joined',
    'fixture_tray_host_only', 'production_parity', 'user_desktop_captured',
    'real_audio_captured', 'input_consent', 'pixels_saved', 'source_artifacts_published',
}
FALSE_FLAGS = {'production_parity', 'user_desktop_captured', 'real_audio_captured',
               'input_consent', 'pixels_saved', 'source_artifacts_published'}
HASHES = {'commit', 'manifest_sha256', 'plain_binary_sha256', 'observed_binary_sha256',
          'source_binary_sha256', 'viewer_binary_sha256', 'base_image_sha256', 'runtime_image_sha256'}
COUNTS = {'preview_runs', 'pixel_samples', 'lifetime_seconds', 'schema_version'}
PRIVATE_MOUNTS = {
    '/tmp': (True, 0o1777, 0, 128 << 20),
    '/tmp/.X11-unix': (True, 0o1777, 0, 1 << 20),
    '/run': (True, 0o755, 0, 16 << 20),
    '/work': (True, 0o700, 10001, 768 << 20),
    '/work/state': (True, 0o700, 10001, 64 << 20),
    '/work/state/source-preview': (False, 0o700, 10001, 256 << 20),
}


def validate_private_mounts(value):
    assert type(value) is dict and set(value) == set(PRIVATE_MOUNTS)
    for path, (noexec, mode, owner, capacity) in PRIVATE_MOUNTS.items():
        record = value[path]
        assert type(record) is dict and set(record) == {'type', 'rw', 'nosuid', 'nodev', 'noexec', 'mode', 'uid', 'gid', 'size_bytes'}
        assert record['type'] == 'tmpfs'
        assert all(type(record[key]) is bool and record[key] for key in ('rw', 'nosuid', 'nodev'))
        assert type(record['noexec']) is bool and record['noexec'] == noexec
        for key, expected in (('mode', mode), ('uid', owner), ('gid', owner), ('size_bytes', capacity)):
            assert type(record[key]) is int and record[key] == expected
    return value


def private_mount_receipt(mount_rows):
    value = {}
    for row in mount_rows:
        path = row[4]
        if any(path == root or path.startswith(root + '/') for root in ('/work', '/tmp', '/run')):
            assert path in PRIVATE_MOUNTS, 'unapproved mount in private writable state'
    for path in PRIVATE_MOUNTS:
        rows = [row for row in mount_rows if row[4] == path]
        assert len(rows) == 1
        row = rows[0]
        separator = row.index('-')
        flags = set(row[5].split(','))
        node = pathlib.Path(path)
        assert not node.is_symlink()
        info = node.stat()
        capacity = os.statvfs(path)
        value[path] = {'type': row[separator + 1],
                       **{key: key in flags for key in ('rw', 'nosuid', 'nodev', 'noexec')},
                       'mode': stat.S_IMODE(info.st_mode), 'uid': info.st_uid, 'gid': info.st_gid,
                       'size_bytes': capacity.f_frsize * capacity.f_blocks}
    return validate_private_mounts(value)


def validate_receipt(value):
    assert isinstance(value, dict)
    assert set(value) == FLAGS | HASHES | COUNTS | {'instrumentation', 'private_mounts'}
    validate_private_mounts(value['private_mounts'])
    assert all(type(value[k]) is bool for k in FLAGS)
    assert value['instrumentation'] == 'source_preview_engine_acceptance/read-only-v1'
    for key in HASHES:
        assert isinstance(value[key], str) and re.fullmatch('[0-9a-f]{40}' if key == 'commit' else '[0-9a-f]{64}', value[key])
    for key in COUNTS:
        assert type(value[key]) is int and 0 <= value[key] <= 10000
    assert value['schema_version'] == 1
    assert value['base_image_sha256'] == BASE_IMAGE_SHA256, 'unreviewed base image digest'
    assert value['runtime_image_sha256'] != value['base_image_sha256'], 'runtime identity must be independently recorded'
    assert not any(value[key] for key in FALSE_FLAGS)
    if value['passed']:
        assert all(value[key] for key in FLAGS - FALSE_FLAGS)
        assert value['preview_runs'] == 4 and value['pixel_samples'] >= 20
        assert value['lifetime_seconds'] >= 76
    return value


def tcp_udp_tables(proc=pathlib.Path('/proc')):
    result = {}
    for proto in ('tcp', 'tcp6', 'udp', 'udp6'):
        for line in (proc / 'net' / proto).read_text().splitlines()[1:]:
            fields = line.split()
            result[fields[9]] = {'proto': proto, 'local': fields[1], 'remote': fields[2], 'state': fields[3]}
    return result


def preflight():
    assert os.getuid() == os.geteuid() == 10001 and os.getgid() == 10001
    assert set(os.getgroups()) <= {10001}, 'supplementary groups forbidden'
    status = dict(line.split(':', 1) for line in pathlib.Path('/proc/self/status').read_text().splitlines() if ':' in line)
    assert all(int(status[key].strip(), 16) == 0 for key in ('CapInh', 'CapPrm', 'CapEff', 'CapBnd', 'CapAmb'))
    assert status['NoNewPrivs'].strip() == '1' and status['Seccomp'].strip() == '2'
    for ns in ('pid', 'mnt', 'ipc', 'net', 'uts'):
        assert os.readlink('/proc/self/ns/' + ns) != os.environ['OUTER_' + ns.upper() + '_NS']
    root_mount = [line.split() for line in pathlib.Path('/proc/self/mountinfo').read_text().splitlines() if line.split()[4] == '/']
    assert len(root_mount) == 1 and 'ro' in root_mount[0][5].split(',')
    interfaces = {line.split(':', 1)[0].strip() for line in pathlib.Path('/proc/net/dev').read_text().splitlines()[2:]}
    assert interfaces == {'lo'}
    routes = pathlib.Path('/proc/net/route').read_text().splitlines()[1:]
    assert not any(line.split()[1] == '00000000' for line in routes)
    for destination in ('1.1.1.1', '8.8.8.8'):
        with socket.socket() as probe:
            probe.settimeout(.2)
            assert probe.connect_ex((destination, 443)) != 0
    for path in ('/dev/uinput', '/dev/input', '/dev/dri', '/dev/bus/usb', '/dev/snd', '/var/run/docker.sock', '/run/dbus/system_bus_socket'):
        assert not pathlib.Path(path).exists()
    for path in pathlib.Path('/dev').rglob('*'):
        if path.is_symlink():
            continue
        mode = path.stat().st_mode
        assert not stat.S_ISBLK(mode)
        if stat.S_ISCHR(mode):
            assert str(path) in {'/dev/null', '/dev/zero', '/dev/full', '/dev/random', '/dev/urandom', '/dev/tty', '/dev/pts/ptmx'}
    mount_rows = [line.split() for line in pathlib.Path('/proc/self/mountinfo').read_text().splitlines()]
    mounts = private_mount_receipt(mount_rows)
    xdir = pathlib.Path('/tmp/.X11-unix')
    assert xdir.is_dir() and not xdir.is_symlink() and not list(xdir.iterdir())
    assert xdir.stat().st_uid == 0 and stat.S_IMODE(xdir.stat().st_mode) == 0o1777
    xmount = [row for row in mount_rows if row[4] == str(xdir)]
    assert len(xmount) == 1 and 'tmpfs' in xmount[0]
    assert os.environ['HOME'] == '/work/home'
    return mounts
