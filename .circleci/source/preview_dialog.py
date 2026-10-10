#!/usr/bin/env python3
"""Native input into the shipped agent settings/dialog on an owned Xvfb only.
Hit-target metadata is read-only; decisions execute through XTest/WM/DBus events.
No screenshots, frames, descriptors, session keys, or key hashes are saved.
"""
import ctypes as C
from collections import Counter
import json
import os
import pathlib
import signal
import subprocess
import sys
import time

import dbus
from preview_processes import validate_capture_chain
from preview_windows import parent_window

WORK = pathlib.Path(os.environ['SOURCE_PREVIEW_DIALOG_WORK'])
OUT = pathlib.Path(os.environ['SOURCE_PREVIEW_DIALOG_OUTPUT'])
ROOT = pathlib.Path(os.environ['SOURCE_PREVIEW_REPO'])
assert os.environ.get('SOURCE_PREVIEW_DIALOG_ACCEPTANCE') == '1'
assert os.environ.get('DISPLAY') == ':97' and os.geteuid() != 0
assert WORK.is_absolute() and OUT.is_absolute() and ROOT.is_absolute()
assert os.environ['HOME'] == str(WORK / 'home')
# Fail closed if the privileged runner did not create its own network namespace.
assert os.readlink('/proc/self/ns/net') != os.environ['SOURCE_PREVIEW_OUTER_NETNS']
interfaces = {line.split(':', 1)[0].strip() for line in pathlib.Path('/proc/net/dev').read_text().splitlines()[2:]}
assert interfaces == {'lo'}, 'only private loopback allowed'

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


def wait_for(fn, timeout=12):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise AssertionError('native parent exited before the interaction finished')
        result = fn()
        if result:
            return result
        time.sleep(.1)
    raise AssertionError('native dialog condition timed out: ' + fn.__name__)


def ui():
    try:
        return json.loads((WORK / 'ui.json').read_text())
    except FileNotFoundError:
        return {}


def hit(name):
    matches = [c for c in ui().get('controls', []) if c['id'] == name]
    assert len(matches) <= 1, 'ambiguous target: ' + name
    return matches[0] if matches else None


verified_parent_xid = None


def window_id(required=True):
    global verified_parent_xid
    selected = parent_window(command, process.pid, ui()['title'], required=required)
    if selected is not None:
        assert verified_parent_xid in (None, selected), 'native parent identity changed'
        verified_parent_xid = selected
    return selected


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
    wait_for(lambda: hit(name) and hit(name)['text'] == text)


def status(prefix):
    return hit('status') and hit('status').get('text', '').startswith(prefix)


def snapshot(label):
    data = ui()
    launches = data['launches']
    assert launches['fresh_keys'] and launches['fresh_ids'] and launches['fresh_key_ids'] and launches['clean_joins']
    record = {'step': label, 'launches': launches, 'dialog_open': data['dialog_open']}
    receipts.append(record)
    (OUT / 'steps.json').write_text(json.dumps(receipts, indent=2) + '\n')
    return launches


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


def media_processes():
    found = {}
    for pid in descendants(process.pid):
        try:
            exe = pathlib.Path(os.readlink(f'/proc/{pid}/exe')).name
        except (FileNotFoundError, ProcessLookupError, PermissionError):
            continue
        if exe in {'source-streamer', 'source-preview-viewer', 'ffmpeg'}:
            found[pid] = exe
    return found


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


def inet_sockets():
    found = {}
    for proto in ('tcp', 'tcp6', 'udp', 'udp6'):
        for line in pathlib.Path('/proc/net/' + proto).read_text().splitlines()[1:]:
            fields = line.split()
            found[fields[9]] = proto
    return found


def no_capture(count, label, hold=.8):
    deadline = time.monotonic() + hold
    while time.monotonic() < deadline:
        s = snapshot_counts()
        assert s['stream_starts'] == count and s['viewer_starts'] == count
        assert s['streams_joined'] == count and s['viewers_joined'] == count
        assert not media_processes(), 'media process without new explicit start'
        time.sleep(.1)
    snapshot(label)


def snapshot_counts():
    return ui()['launches']


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
    enter('components', os.environ['SOURCE_PREVIEW_COMPONENTS'])
    enter('manifest', pin)
    enter('ffmpeg', '/usr/bin/ffmpeg')
    enter('display', ':96')


def probe(mode, name):
    subprocess.run(['/usr/bin/python3', str(ROOT / '.circleci/source/preview_pixels.py'), '--mode', mode, '--output', str(OUT / (name + '.json'))], check=True, timeout=10)


def start_and_view(number, label):
    click('consent')
    wait_for(lambda: hit('consent').get('checked', False))
    before = snapshot_counts()['prepare_calls']
    no_capture(number - 1, label + '-checkbox-alone')
    assert snapshot_counts()['prepare_calls'] == before
    click('start')
    wait_for(lambda: hit('start') and not hit('start')['enabled'])
    click('start', allow_disabled=True)  # A repeated native click must be inert.
    wait_for(lambda: status('Previewing actual frames.'), timeout=18)
    assert not hit('consent').get('checked', False), 'capture grant not consumed'
    assert snapshot_counts()['stream_starts'] == number
    assert snapshot_counts()['viewer_starts'] == number
    def complete_chain():
        children = media_processes()
        if Counter(children.values()) == {'source-streamer': 1, 'source-preview-viewer': 1, 'ffmpeg': 2}:
            return children
        return None
    children = wait_for(complete_chain)
    # Source uses distinct video and silence-to-Opus FFmpeg processes.
    def codec_arguments(pid):
        raw = pathlib.Path(f'/proc/{pid}/cmdline').read_bytes()
        assert len(raw) <= 65536
        return [part.decode('ascii') for part in raw.split(b'\0') if part]
    validate_capture_chain(children, codec_arguments)
    assert len(children) == 4, 'exactly one source, one viewer and two codecs required'
    sockets = socket_inodes(children)
    inet = inet_sockets()
    owned_inet = {i: inet[i] for i in sockets if i in inet}
    assert 'tcp' in owned_inet.values() and list(owned_inet.values()).count('udp') >= 3, 'source/viewer transport sockets absent'
    # Independent X11 samples, not a first-frame callback or saved video dump.
    # Parent was raised to test double-click; bring viewer back above it.
    viewers = command('xdotool', 'search', '--name', r'^Source preview \(experimental, this computer\)$').splitlines()
    assert len(viewers) == 1
    command('xdotool', 'windowraise', viewers[0])
    start = time.monotonic()
    samples = 0
    while time.monotonic() - start < 5:
        probe('pixels', label + '-pixels-' + str(samples))
        samples += 1
        time.sleep(.5)
    assert samples >= 5
    snapshot(label + '-viewing')
    return children, owned_inet


def joined(number, children, sockets, label):
    wait_for(lambda: snapshot_counts()['streams_joined'] == number and snapshot_counts()['viewers_joined'] == number)
    wait_for(lambda: not media_processes())
    assert all(not pathlib.Path('/proc/' + str(pid)).exists() for pid in children)
    assert not (set(sockets) & set(inet_sockets())), 'owned transport socket survived join'
    probe('gone', label + '-viewer-gone')
    no_capture(number, label + '-joined')


def parent_nonblank():
    r = C.c_ulong(); xx = C.c_int(); yy = C.c_int(); ww = C.c_uint(); hh = C.c_uint(); b = C.c_uint(); depth = C.c_uint()
    selected = window_id(required=False)
    if selected is None:
        return None
    wid = int(selected)
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
    # The close-to-tray assertion deliberately inspects our previously verified
    # XID: a new onlyvisible search cannot discover an unmapped window.
    assert verified_parent_xid is not None
    return 'Map State: IsViewable' in command('xwininfo', '-id', verified_parent_xid)


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


receipts = []
result = {'passed': False, 'agent_parent_window_interaction_tested': False,
          'commit': os.environ['SOURCE_PREVIEW_COMMIT'],
          'manifest_sha256': os.environ['SOURCE_PREVIEW_MANIFEST_SHA256']}
log = (OUT / 'native-parent.log').open('w')
process = subprocess.Popen([str(WORK / 'source-preview-ui-acceptance')], stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
try:
    wait_for(lambda: ui().get('native_glfw') and ui().get('tray_attached') and ui().get('tray_sender'))
    assert ui()['pid'] == process.pid
    result['parent_canvas'] = wait_for(parent_nonblank)
    no_capture(0, 'parent-before-dialog')
    open_dialog()
    no_capture(0, 'dialog-before-consent')
    fill_form('0' * 64)
    click('start')
    wait_for(lambda: status('Approve the exact display'))
    no_capture(0, 'start-without-consent')
    assert snapshot_counts()['prepare_calls'] == 0
    click('consent')
    wait_for(lambda: hit('consent').get('checked', False))
    no_capture(0, 'checkbox-without-start')
    assert snapshot_counts()['prepare_calls'] == 0
    click('start')
    wait_for(lambda: status('verified preview viewer is unavailable') and hit('start')['enabled'])
    assert not hit('consent').get('checked', False)
    no_capture(0, 'untrusted-manifest-rejected')
    enter('manifest', os.environ['SOURCE_PREVIEW_MANIFEST_SHA256'])
    children, sockets = start_and_view(1, 'dialog-stop')
    click('stop')
    wait_for(lambda: status('Preview stopped.'))
    joined(1, children, sockets, 'dialog-stop')
    click('start')
    wait_for(lambda: status('Approve the exact display'))
    no_capture(1, 'retry-needs-new-consent')
    children, sockets = start_and_view(2, 'dialog-close')
    click('close-dialog')
    wait_for(lambda: not ui()['dialog_open'])
    joined(2, children, sockets, 'dialog-close')
    open_dialog()
    fill_form(os.environ['SOURCE_PREVIEW_MANIFEST_SHA256'])
    children, sockets = start_and_view(3, 'parent-close')
    wm_close()
    wait_for(lambda: not ui()['dialog_open'] and not mapped())
    assert process.poll() is None, 'close did not retain the parent in tray'
    joined(3, children, sockets, 'parent-close')
    tray_event('Open USBridge Agent')
    wait_for(mapped)
    open_dialog()
    no_capture(3, 'tray-reopen-without-consent')
    click('close-dialog')
    wait_for(lambda: not ui()['dialog_open'])
    final = snapshot('before-tray-quit')
    assert final['prepare_calls'] == 4, 'unexpected preparation without a fresh approved Start'
    tray_event('Quit')
    assert process.wait(timeout=10) == 0
    assert not media_processes()
    result.update(passed=True, agent_parent_window_interaction_tested=True,
                  native_glfw=True, shipped_window_settings_dialog=True,
                  native_pointer_keyboard_events=True, read_only_hit_target_metadata=True,
                  actual_native_viewer=True, five_second_presented_pixels_each_run=True,
                  no_capture_before_consent_and_start=True, invalid_manifest_rejected=True,
                  consent_consumed=True, repeated_start_did_not_overlap=True,
                  dialog_stop_joined=True, dialog_close_joined=True,
                  parent_wm_close_to_tray_joined=True, tray_reopen_did_not_capture=True,
                  real_dbus_tray_quit=True, private_fixture_tray_host=True,
                  owned_processes_and_transport_sockets_gone=True, launches=final,
                  generated_video_and_private_pcm_codecs_verified=True, media_processes_per_launch=4,
                  capture_display=':96', parent_and_viewer_display=':97',
                  isolated_state=True, isolated_loopback_network=True,
                  production_engine_started=False, production_engine_entrypoint_tested=False,
                  full_desktop_tray_rendering_tested=False, user_desktop_captured=False,
                  audio='synthetic silence', input_consent=False, pixels_saved=False)
except BaseException as error:
    result['error'] = str(error)
    raise
finally:
    (OUT / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
    if (WORK / 'ui.json').exists():
        (OUT / 'last-ui.json').write_bytes((WORK / 'ui.json').read_bytes())
    if process.poll() is None:
        # Failure cleanup, never evidence for a passing Stop/Close assertion.
        os.killpg(process.pid, signal.SIGTERM)
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGKILL); process.wait(timeout=5)
    if not result['passed']:
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
    log.close()
    x.XCloseDisplay(xd)
print(json.dumps(result))
