#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-only
"""Independent Xlib event/state observer for our test-created Xvfb only."""
import ctypes as C
import json
import sys

lib = C.CDLL('libX11.so.6')
P, U, I, L = C.c_void_p, C.c_uint, C.c_int, C.c_ulong

def fn(name, result, *args):
    f = getattr(lib, name)
    f.restype = result
    f.argtypes = list(args)
    return f

class KeyEvent(C.Structure):
    _fields_ = [('type', I), ('serial', L), ('send_event', I), ('display', P),
                ('window', L), ('root', L), ('subwindow', L), ('time', L),
                ('x', I), ('y', I), ('x_root', I), ('y_root', I),
                ('state', U), ('code', U), ('same_screen', I)]
class Event(C.Union):
    _fields_ = [('type', I), ('key', KeyEvent), ('pad', C.c_long * 24)]

open_display = fn('XOpenDisplay', P, C.c_char_p)
close_display = fn('XCloseDisplay', I, P)
root_window = fn('XDefaultRootWindow', L, P)
create_window = fn('XCreateSimpleWindow', L, P, L, I, I, U, U, U, L, L)
map_window = fn('XMapWindow', I, P, L)
select_input = fn('XSelectInput', I, P, L, C.c_long)
focus = fn('XSetInputFocus', I, P, L, I, L)
sync = fn('XSync', I, P, I)
pending = fn('XPending', I, P)
next_event = fn('XNextEvent', I, P, C.POINTER(Event))
keycode = fn('XKeysymToKeycode', C.c_ubyte, P, L)
query_keys = fn('XQueryKeymap', I, P, C.POINTER(C.c_ubyte))
query_pointer = fn('XQueryPointer', I, P, L, C.POINTER(L), C.POINTER(L),
                   C.POINTER(I), C.POINTER(I), C.POINTER(I), C.POINTER(I), C.POINTER(U))

# The test passes its own fresh virtual display explicitly, never DISPLAY.
display = open_display(sys.argv[1].encode('ascii'))
if not display:
    raise RuntimeError('observer cannot open isolated display')
root = root_window(display)
window = create_window(display, root, 0, 0, 320, 240, 0, 0, 0)
select_input(display, window, (1 << 0) | (1 << 1) | (1 << 2) | (1 << 3) | (1 << 6))
map_window(display, window)
focus(display, window, 1, 0)
sync(display, 0)
print(json.dumps({'ready': True}), flush=True)
for line in sys.stdin:
    if line.strip() == 'quit':
        break
    if line.strip() != 'snapshot':
        raise ValueError('unknown test command')
    sync(display, 0)
    events = []
    while pending(display):
        event = Event()
        next_event(display, C.byref(event))
        if event.type in (2, 3, 4, 5):
            events.append([event.type, event.key.code, event.key.state])
    keys = (C.c_ubyte * 32)()
    query_keys(display, keys)
    r, child, rx, ry, wx, wy, mask = L(), L(), I(), I(), I(), I(), U()
    query_pointer(display, root, C.byref(r), C.byref(child), C.byref(rx), C.byref(ry),
                  C.byref(wx), C.byref(wy), C.byref(mask))
    def held(sym):
        code = keycode(display, sym)
        return bool(keys[code // 8] & (1 << (code % 8)))
    print(json.dumps({'x': rx.value, 'y': ry.value, 'mask': mask.value,
                      'a_held': held(ord('a')), 'ctrl_held': held(0xffe4),
                      'a_code': keycode(display, ord('a')), 'events': events}), flush=True)
close_display(display)
