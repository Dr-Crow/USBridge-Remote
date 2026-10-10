#!/usr/bin/env python3
"""Real CLI/App.New + native dialog + real renderer, inside disposable Docker only.
Metadata is read-only. All actions use XTest, WM events, or the real tray DBus API.
No configurations, secrets, descriptors, screenshots, raw argv or logs are receipts.
"""
import ctypes as C
import json
import os
import pathlib
import signal
import socket
import stat
import subprocess
import time
import urllib.request
from collections import Counter
import dbus
from engine_preview_contract import FLAGS, FALSE_FLAGS, tcp_udp_tables
from preview_processes import validate_capture_chain
from preview_windows import parent_window

WORK = pathlib.Path('/work')
OUT = WORK / 'samples'
OUT.mkdir()
assert os.environ.get('SOURCE_PREVIEW_ENGINE_ACCEPTANCE') == '1'
assert os.environ.get('DISPLAY') == ':97' and os.geteuid() == 10001
assert os.environ['HOME'] == '/work/home'
x = C.CDLL('libX11.so.6')
x.XOpenDisplay.argtypes = [C.c_char_p]
x.XOpenDisplay.restype = C.c_void_p
xd = x.XOpenDisplay(b':97')
assert xd
x.XGetGeometry.argtypes = [C.c_void_p, C.c_ulong, C.POINTER(C.c_ulong), C.POINTER(C.c_int), C.POINTER(C.c_int), C.POINTER(C.c_uint), C.POINTER(C.c_uint), C.POINTER(C.c_uint), C.POINTER(C.c_uint)]
x.XGetImage.argtypes = [C.c_void_p, C.c_ulong, C.c_int, C.c_int, C.c_uint, C.c_uint, C.c_ulong, C.c_int]
x.XGetImage.restype = C.c_void_p
x.XGetPixel.argtypes = [C.c_void_p, C.c_int, C.c_int]
x.XGetPixel.restype = C.c_ulong
x.XDestroyImage.argtypes = [C.c_void_p]
x.XInternAtom.argtypes = [C.c_void_p, C.c_char_p, C.c_int]
x.XInternAtom.restype = C.c_ulong
x.XSendEvent.argtypes = [C.c_void_p, C.c_ulong, C.c_int, C.c_long, C.c_void_p]
x.XSync.argtypes = [C.c_void_p, C.c_int]
x.XCloseDisplay.argtypes = [C.c_void_p]


def command(*args, **kwargs):
    return subprocess.check_output(args, text=True, timeout=10, **kwargs).strip()

def ui():
    try:
        return {'title': 'USBridge Agent'} if plain_phase else json.loads((WORK / 'ui.json').read_text())
    except FileNotFoundError:
        return {}

def hit(name):
    matches = [c for c in ui().get('controls', []) if c['id'] == name]
    assert len(matches) <= 1, 'ambiguous target: ' + name
    return matches[0] if matches else None

def click(name, allow_disabled=False):
    control = wait_for(lambda: hit(name))
    assert control['enabled'] or allow_disabled, 'disabled target: ' + name
    wid = window_id()
    # Raise the existing native parent (there is intentionally no window manager).
    command('xdotool', 'windowraise', wid, 'windowfocus', wid)
    command('xdotool', 'mousemove', '--window', wid, str(round(control['x'])), str(round(control['y'])), 'click', '1')

def enter(name, text):
    click(name)
    command('xdotool', 'key', '--clearmodifiers', 'ctrl+a')
    command('xdotool', 'type', '--clearmodifiers', '--delay', '1', '--', text)
    def updated():
        control = hit(name)
        # Empty Go metadata strings are omitted, and the native UI update is
        # asynchronous. Keep polling instead of indexing an absent field.
        return control is not None and control.get('text', '') == text
    wait_for(updated)

def status(prefix):
    return hit('status') and hit('status').get('text', '').startswith(prefix)

def descendants(parent):
    parents = {}
    for p in pathlib.Path('/proc').glob('[0-9]*'):
        try:
            tail = (p / 'stat').read_text().rsplit(')', 1)[1].split()
            parents[int(p.name)] = int(tail[1])
        except (FileNotFoundError, ProcessLookupError, PermissionError):
            pass
    selected = {parent}
    while True:
        expanded = selected | {pid for pid, ppid in parents.items() if ppid in selected}
        if expanded == selected:
            return selected - {parent}
        selected = expanded

def socket_inodes(pids):
    found = set()
    for pid in pids:
        try:
            for fd in pathlib.Path(f'/proc/{pid}/fd').iterdir():
                try:
                    link = os.readlink(fd)
                    if link.startswith('socket:['):
                        found.add(link[8:-1])
                except (FileNotFoundError, ProcessLookupError):
                    pass
        except (FileNotFoundError, ProcessLookupError):
            pass
    return found

def open_dialog():
    click('settings')
    click('source-menu')
    wait_for(lambda: hit('components') and hit('consent') and hit('close-dialog'))
    assert not hit('consent').get('checked', False)
    assert hit('display')['text'] == ':0', 'reopen retained the prior capture display'
    assert hit('components').get('text', '') == ''
    assert hit('profile')['text'] == '128 × 72 (validated transport)'
    assert status('No capture has started.')

def fill_form(pin):
    enter('components', '/opt/preview/components')
    enter('manifest', pin)
    enter('ffmpeg', '/usr/bin/ffmpeg')
    enter('display', ':96')

def parent_nonblank():
    r = C.c_ulong(); xx = C.c_int(); yy = C.c_int(); ww = C.c_uint(); hh = C.c_uint(); b = C.c_uint(); depth = C.c_uint()
    wid = int(window_id())
    assert x.XGetGeometry(xd, wid, C.byref(r), C.byref(xx), C.byref(yy), C.byref(ww), C.byref(hh), C.byref(b), C.byref(depth))
    if ww.value < 640 or hh.value < 400:
        return None
    im = x.XGetImage(xd, wid, 0, 0, ww.value, hh.value, C.c_ulong(-1).value, 2)
    if not im:
        return None
    colors = {x.XGetPixel(im, col, row) for row in range(10, hh.value, 11) for col in range(10, ww.value, 11)}
    x.XDestroyImage(im)
    if len(colors) < 8:
        return None
    return {'sampled_colors': len(colors), 'width': ww.value, 'height': hh.value, 'pixels_saved': False}

class MessageData(C.Union):
    _fields_ = [('b', C.c_char * 20), ('s', C.c_short * 10), ('l', C.c_long * 5)]

class ClientMessage(C.Structure):
    _fields_ = [('type', C.c_int), ('serial', C.c_ulong), ('send_event', C.c_int), ('display', C.c_void_p), ('window', C.c_ulong), ('message_type', C.c_ulong), ('format', C.c_int), ('data', MessageData)]

def wm_close():
    # WM_DELETE_WINDOW is the same close request a title-bar close sends. There
    # is no WM on this Xvfb; never use XDestroyWindow or kill to fake cleanup.
    message = ClientMessage()
    message.type = 33; message.send_event = 1; message.display = xd
    message.window = int(window_id()); message.format = 32
    message.message_type = x.XInternAtom(xd, b'WM_PROTOCOLS', 0)
    message.data.l[0] = x.XInternAtom(xd, b'WM_DELETE_WINDOW', 0)
    buf = C.create_string_buffer(24 * C.sizeof(C.c_long))
    C.memmove(buf, C.byref(message), C.sizeof(message))
    assert x.XSendEvent(xd, message.window, 0, 0, buf)
    x.XSync(xd, 0)

def mapped():
    # Visibility transitions query the XID already verified while visible.
    # Never substitute a hidden GLFW support window after close-to-tray.
    wid = known_parent_windows.get(process.pid)
    assert wid is not None, 'parent XID was never verified'
    return 'Map State: IsViewable' in command('xwininfo', '-id', wid)

def tray_event(label):
    data = ui()
    bus = dbus.SessionBus()
    item = bus.get_object(data['tray_sender'], data['tray_path'])
    path = item.Get('org.kde.StatusNotifierItem', 'Menu', dbus_interface='org.freedesktop.DBus.Properties')
    menu = bus.get_object(data['tray_sender'], path)
    _, layout = menu.GetLayout(dbus.Int32(0), dbus.Int32(-1), dbus.Array([], signature='s'), dbus_interface='com.canonical.dbusmenu')
    def find(node):
        result = [int(node[0])] if str(node[1].get('label', '')).replace('_', '') == label else []
        for child in node[2]:
            result += find(child)
        return result
    ids = find(layout)
    assert len(ids) == 1, 'missing/ambiguous real tray action: ' + label
    menu.Event(dbus.Int32(ids[0]), 'clicked', dbus.Int32(0), dbus.UInt32(0), dbus_interface='com.canonical.dbusmenu')


def window_id(allow_missing=False):
    try:
        title = ui()['title']
    except KeyError:
        if allow_missing:
            return None
        raise AssertionError('native parent window metadata missing') from None
    selected = parent_window(command, process.pid, title, required=not allow_missing)
    if selected is not None:
        assert known_parent_windows.get(process.pid) in (None, selected), 'native parent identity changed'
        known_parent_windows[process.pid] = selected
    return selected


def assert_no_stock():
    forbidden = {'sunshine', 'rustshine', 'punktfunk', 'usbridge-streamer', 'gamestream-server', 'usbridge-usb-broker', 'source-broker', 'tailscaled'}
    for path in pathlib.Path('/proc').glob('[0-9]*/exe'):
        try:
            name = pathlib.Path(os.readlink(path)).name
        except (FileNotFoundError, ProcessLookupError, PermissionError):
            continue
        assert name not in forbidden, 'unexpected stock backend, broker or enrollment process'
    if not plain_phase and ui().get('engine'):
        engine = ui()['engine']
        for key in ('app_new_completed', 'strict_lan', 'runtime_local', 'usbpass_loopback'):
            assert engine[key], 'real engine required invariant absent'
        for key in ('local_runtime_enabled', 'streamer_consent', 'usb_consent', 'stock_streamer_running', 'stock_session_active', 'account_logged_in', 'tailscale_enabled', 'tls_enabled', 'clipboard_enabled'):
            assert not engine[key], 'unexpected real engine activity or permission'


def wait_for(fn, timeout=15):
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        assert process.poll() is None, 'actual CLI exited prematurely'
        assert_no_stock()
        value = fn()
        if value:
            return value
        time.sleep(.1)
    raise AssertionError('native condition timed out')


def media_processes():
    found = {}
    for pid in descendants(process.pid):
        try:
            name = pathlib.Path(os.readlink(f'/proc/{pid}/exe')).name
        except (FileNotFoundError, ProcessLookupError, PermissionError):
            continue
        if name in {'source-streamer', 'source-preview-viewer', 'ffmpeg'}:
            found[pid] = name
    return found


def verify_media_shape(children):
    # Reuse the unchanged independently unit-tested four-process classifier.
    def arguments(pid):
        return pathlib.Path(f'/proc/{pid}/cmdline').read_bytes().decode().rstrip('\0').split('\0')
    validate_capture_chain(children, arguments)


def owned_inet(pids):
    table = tcp_udp_tables()
    return {inode: table[inode] for inode in socket_inodes(pids) if inode in table}


def health():
    try:
        with urllib.request.urlopen('http://127.0.0.1:8080/api/healthz', timeout=1) as response:
            return response.status == 200
    except OSError:
        return False


def baseline_ready(state):
    if not health():
        return None
    sockets = owned_inet({process.pid})
    listeners = {i: s for i, s in sockets.items() if s['state'] == '0A'}
    if len(listeners) != 2:
        return None
    assert all(s['proto'] == 'tcp' for s in sockets.values())
    assert all(s['proto'] == 'tcp' and s['local'].startswith('0100007F:') for s in listeners.values())
    ports = {int(s['local'].split(':')[1], 16) for s in listeners.values()}
    assert 8080 in ports
    if not plain_phase:
        assert ui()['engine']['usbpass_port'] in ports - {8080}
    admin = state / 'admin.sock'
    try:
        admin_mode = admin.lstat().st_mode
    except FileNotFoundError:
        return None  # Startup is bounded by wait_for, not by a fixed sleep.
    assert stat.S_ISSOCK(admin_mode) and stat.S_IMODE(admin_mode) == 0o600
    return listeners


def idle(label, hold=.8):
    end = time.monotonic() + hold
    while time.monotonic() < end:
        assert process.poll() is None
        assert_no_stock()
        assert not media_processes(), 'capture without explicit approved Start'
        owned = owned_inet({process.pid})
        assert {i: s for i, s in owned.items() if s['state'] == '0A'} == baseline, 'engine did not return to baseline listeners'
        assert all(s['proto'] == 'tcp' and s['local'].startswith('0100007F:') for s in owned.values()), 'unexpected engine transport socket'
        time.sleep(.1)


def probe(mode, name):
    target = OUT / (name + '.json')
    subprocess.run(['/usr/bin/python3', '/opt/gate/preview_pixels.py', '--mode', mode, '--output', str(target)], check=True, timeout=10, stdout=subprocess.DEVNULL)
    receipt = json.loads(target.read_text())
    assert receipt['passed'] and not receipt['pixels_saved']
    if mode == 'pixels':
        assert receipt['matching_samples'] / receipt['total_samples'] >= .95


def start_and_view(label):
    global pixel_samples
    click('consent')
    wait_for(lambda: hit('consent').get('checked', False))
    idle(label + '-checkbox-alone')
    click('start')
    wait_for(lambda: hit('start') and not hit('start')['enabled'])
    click('start', allow_disabled=True)
    wait_for(lambda: status('Previewing actual frames.'), timeout=20)
    assert not hit('consent').get('checked', False)
    children = wait_for(lambda: media_processes() if Counter(media_processes().values()) == Counter({'source-streamer': 1, 'source-preview-viewer': 1, 'ffmpeg': 2}) else None)
    verify_media_shape(children)
    sockets = owned_inet(children)
    protocols = [s['proto'] for s in sockets.values()]
    assert 'tcp' in protocols and protocols.count('udp') >= 3
    viewers = command('xdotool', 'search', '--name', r'^Source preview \(experimental, this computer\)$').splitlines()
    assert len(viewers) == 1
    command('xdotool', 'windowraise', viewers[0])
    start = time.monotonic()
    while time.monotonic() - start < 5:
        assert_no_stock()
        assert media_processes() == children, 'repeated Start created overlapping media children'
        probe('pixels', label + '-' + str(pixel_samples))
        pixel_samples += 1
        time.sleep(.5)
    return children, sockets


def joined(children, sockets, label):
    wait_for(lambda: not media_processes())
    assert all(not pathlib.Path('/proc/' + str(pid)).exists() for pid in children)
    assert not (set(sockets) & set(tcp_udp_tables()))
    probe('gone', label)
    idle(label)


def exited(children, sockets, state):
    assert process.wait(timeout=15) == 0
    assert not pathlib.Path('/proc/' + str(process.pid)).exists()
    assert all(not pathlib.Path('/proc/' + str(pid)).exists() for pid in children)
    assert not (set(sockets) & set(tcp_udp_tables()))
    assert not health()
    # A stale pathname is not a live socket. Tray Quit can win the race with
    # the engine cancellation goroutine; this gate claims OS process/socket
    # cleanup, not that every internal shutdown goroutine ran to completion.
    with socket.socket(socket.AF_UNIX) as probe_socket:
        probe_socket.settimeout(.3)
        assert probe_socket.connect_ex(str(state / 'admin.sock')) != 0


def process_env(base, observer):
    names = ['PATH', 'LANG', 'DISPLAY', 'XAUTHORITY', 'DBUS_SESSION_BUS_ADDRESS', 'FYNE_SCALE', 'LIBGL_ALWAYS_SOFTWARE']
    env = {key: os.environ[key] for key in names}
    for key, directory in {'HOME': 'home', 'XDG_CONFIG_HOME': 'config', 'XDG_CACHE_HOME': 'cache', 'XDG_DATA_HOME': 'data', 'XDG_RUNTIME_DIR': 'runtime', 'TMPDIR': 'tmp', 'USBRIDGE_LOG_DIR': 'logs'}.items():
        env[key] = str(base / directory)
    if observer:
        env.update(SOURCE_PREVIEW_ENGINE_ACCEPTANCE='1', SOURCE_PREVIEW_ENGINE_WORK='/work')
    return env


def start_cli(base, observer):
    binary = '/opt/agent/usbridge-agent-observed' if observer else '/opt/agent/usbridge-agent'
    log = (base / 'logs/cli-private.log').open('w')
    child = subprocess.Popen([binary, '--strict-lan'], cwd=base / 'cwd', env=process_env(base, observer), stdout=log, stderr=log, start_new_session=True)
    log.close()
    assert os.readlink(f'/proc/{child.pid}/exe') == binary, 'unexpected executable'
    return child


known_parent_windows = {}
stage = 0
pixel_samples = 0
plain_phase = True
process = None
result = {key: False for key in FLAGS}
result.update(passed=False, schema_version=1, instrumentation='source_preview_engine_acceptance/read-only-v1', preview_runs=0, pixel_samples=0, lifetime_seconds=0, sandbox_helpers_joined=False)
result.update(json.loads(pathlib.Path('/opt/gate/input-hashes.json').read_text()))
try:
    process = start_cli(WORK / 'plain', False)
    wait_for(lambda: window_id(allow_missing=True))
    wait_for(parent_nonblank)
    baseline = wait_for(lambda: baseline_ready(WORK / 'plain/state'))
    idle('plain-native-startup', 16)
    stage = 1
    assert not (WORK / 'ui.json').exists(), 'ordinary binary activated acceptance observer'
    children = descendants(process.pid)
    wm_close()  # no fixture tray host exists: ordinary close exits normally
    exited(children, baseline, WORK / 'plain/state')
    stage = 2

    plain_phase = False
    process = start_cli(WORK, True)
    started = time.monotonic()
    wait_for(lambda: ui().get('native_glfw') and ui().get('owns_engine') and ui().get('tray_sender'))
    assert ui()['pid'] == process.pid
    assert ui()['instrumentation'] == result['instrumentation']
    wait_for(lambda: window_id(allow_missing=True))
    wait_for(parent_nonblank)
    baseline = wait_for(lambda: baseline_ready(WORK / 'state'))
    idle('real-engine-before-dialog', 16)
    stage = 3
    open_dialog()
    idle('dialog-before-consent')
    fill_form('0' * 64)
    click('start')
    wait_for(lambda: status('Approve the exact display'))
    idle('start-without-consent')
    click('consent')
    wait_for(lambda: hit('consent').get('checked', False))
    idle('checkbox-without-start')
    click('start')
    wait_for(lambda: status('verified preview viewer is unavailable') and hit('start')['enabled'])
    assert not hit('consent').get('checked', False)
    idle('untrusted-manifest-rejected')
    stage = 4
    enter('manifest', result['manifest_sha256'])
    children, sockets = start_and_view('stop')
    click('stop')
    wait_for(lambda: status('Preview stopped.'))
    joined(children, sockets, 'stop')
    stage = 5
    click('start')
    wait_for(lambda: status('Approve the exact display'))
    idle('retry-needs-new-consent')
    children, sockets = start_and_view('close')
    click('close-dialog')
    wait_for(lambda: not ui()['dialog_open'])
    joined(children, sockets, 'close')
    stage = 6
    open_dialog()
    fill_form(result['manifest_sha256'])
    children, sockets = start_and_view('parent-close')
    wm_close()
    wait_for(lambda: not ui()['dialog_open'] and not mapped())
    joined(children, sockets, 'parent-close')
    stage = 7
    assert health(), 'close-to-tray terminated the real engine'
    tray_event('Open USBridge Agent')
    wait_for(mapped)
    open_dialog()
    idle('tray-reopen-without-consent')
    # Cover the missing-stock-backend watchdog after its one-minute cooldown.
    idle('post-cooldown-engine-idle', max(.8, 77 - (time.monotonic() - started)))
    stage = 8
    fill_form(result['manifest_sha256'])
    children, sockets = start_and_view('quit-active')
    stage = 9
    all_children = descendants(process.pid)
    all_sockets = dict(baseline, **sockets)
    tray_event('Quit')
    exited(all_children, all_sockets, WORK / 'state')
    probe('gone', 'quit')
    stage = 10
    result.update({key: key not in FALSE_FLAGS for key in FLAGS})
    result['sandbox_helpers_joined'] = False
    result.update(passed=True, preview_runs=4, pixel_samples=pixel_samples, lifetime_seconds=int(time.monotonic() - started))
finally:
    if process is not None and process.poll() is None:
        os.killpg(process.pid, signal.SIGTERM)
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGKILL)
            process.wait(timeout=5)
    if not result['passed'] and process is not None:
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
    (WORK / 'failure-stage').write_text(str(stage))
    (WORK / 'result.json').write_text(json.dumps(result, sort_keys=True) + '\n')
    x.XCloseDisplay(xd)
