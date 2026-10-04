//go:build linux && !android

// Raw relative mouse capture for Capture mode on X11.
//
// go/build's filename-based matching treats "_linux" C/H files as valid for
// GOOS=android too (Android's kernel is Linux) -- unlike mouse_capture_linux.go,
// this file has no build-tag comment of its own to override that, so without
// the explicit tag above it gets pulled into Android builds and fails on
// missing X11/Xlib.h. Same fix v4l2camera_impl_linux.c/.h already carry.
//
// There is no XInput2 raw-motion extension linked into this build (only
// -lX11, same as vk_video_impl_linux.c), so this uses the same "virtual
// infinite" technique as the macOS engine's conceptual cousin: read each
// MotionNotify's root-relative position, diff it against a fixed center
// point to get a delta, then XWarpPointer back to that center so the next
// sample is always relative to the same origin. The cursor itself is hidden
// via a blank XDefineCursor, so the recentring is never visible.
//
// This opens its own Xlib Display connection (XOpenDisplay), exactly like
// vk_video_impl_linux.c's overlay does, and selects plain (non-exclusive)
// PointerMotionMask/ButtonPress/ButtonReleaseMask on the *existing* Fyne
// window -- no new window is created. Those event classes are shareable
// between clients, so GLFW's own connection keeps receiving the same events
// too; the Go-side TouchpadWrapper guards (inCaptureMode in video_mouse_
// handler.go) are what stop them from being double-processed, not anything
// on this side.
//
// IMPORTANT Xlib footgun this code works around: if one thread is blocked
// in XNextEvent on a Display and another thread calls XCloseDisplay on that
// same Display, Xlib's default IOErrorHandler treats the resulting failure
// as fatal and calls exit() on the WHOLE PROCESS -- not just this engine.
// So stop() never closes the display out from under the event thread; it
// first sends a ClientMessage to wake XNextEvent, joins the thread (which
// sees the event, finds g_active already false, and returns on its own),
// and only then closes the display.

#ifdef __linux__

#include <X11/Xlib.h>
#include <string.h>
#include <pthread.h>
#include <stdint.h>

#include "mouse_capture_linux.h"

extern void goMouseCaptureMove(int dx, int dy);
extern void goMouseCaptureButton(int button, int pressed);
extern void goMouseCaptureScroll(int delta);

static Display *g_dpy = NULL;
static Window g_win = 0;
static Cursor g_blank_cursor = None;
static pthread_t g_thread;
static int g_thread_running = 0;
static volatile int g_active = 0;
static int g_center_x = 0, g_center_y = 0;
// Guards against reacting to the MotionNotify our own XWarpPointer below
// generates -- each real sample we forward is immediately followed by one
// warp-back-to-center, so exactly one subsequent MotionNotify is ours.
static int g_suppress_next_motion = 0;

static Cursor usbridge_make_blank_cursor(Display *dpy, Window win) {
    char data[1] = {0};
    Pixmap blank = XCreateBitmapFromData(dpy, win, data, 1, 1);
    if (blank == None) {
        return None;
    }
    XColor black;
    memset(&black, 0, sizeof(black));
    Cursor cursor = XCreatePixmapCursor(dpy, blank, blank, &black, &black, 0, 0);
    XFreePixmap(dpy, blank);
    return cursor;
}

static void *usbridge_event_thread(void *arg) {
    (void)arg;
    while (g_active) {
        XEvent ev;
        XNextEvent(g_dpy, &ev); // blocks until a real event or our own wake ClientMessage
        if (!g_active) {
            break;
        }
        switch (ev.type) {
        case MotionNotify: {
            if (g_suppress_next_motion) {
                g_suppress_next_motion = 0;
                break;
            }
            int dx = ev.xmotion.x_root - g_center_x;
            int dy = ev.xmotion.y_root - g_center_y;
            if (dx != 0 || dy != 0) {
                goMouseCaptureMove(dx, dy);
            }
            g_suppress_next_motion = 1;
            XWarpPointer(g_dpy, None, g_win, 0, 0, 0, 0, g_center_x, g_center_y);
            XFlush(g_dpy);
            break;
        }
        case ButtonPress:
        case ButtonRelease: {
            int pressed = (ev.type == ButtonPress) ? 1 : 0;
            // Button numbers match Limelight.h's BUTTON_LEFT/MIDDLE/RIGHT
            // (1/2/3) directly -- X11's own Button1/2/3 already line up.
            switch (ev.xbutton.button) {
            case Button1: goMouseCaptureButton(1, pressed); break;
            case Button2: goMouseCaptureButton(2, pressed); break;
            case Button3: goMouseCaptureButton(3, pressed); break;
            case Button4: if (pressed) goMouseCaptureScroll(1);  break; // wheel up
            case Button5: if (pressed) goMouseCaptureScroll(-1); break; // wheel down
            default: break;
            }
            break;
        }
        default:
            break; // including our own wake ClientMessage
        }
    }
    return NULL;
}

int usbridge_mouse_capture_start(uintptr_t xwindow) {
    if (g_active) {
        return 1;
    }
    if (xwindow == 0) {
        return 0;
    }

    Display *dpy = XOpenDisplay(NULL);
    if (!dpy) {
        return 0;
    }

    Window win = (Window)xwindow;
    XWindowAttributes attrs;
    if (!XGetWindowAttributes(dpy, win, &attrs)) {
        XCloseDisplay(dpy);
        return 0;
    }
    // Only used as a fixed recentring origin -- doesn't need to be exact.
    g_center_x = attrs.x + attrs.width / 2;
    g_center_y = attrs.y + attrs.height / 2;

    XSelectInput(dpy, win, PointerMotionMask | ButtonPressMask | ButtonReleaseMask);

    Cursor blank = usbridge_make_blank_cursor(dpy, win);
    if (blank != None) {
        XDefineCursor(dpy, win, blank);
    }

    g_dpy = dpy;
    g_win = win;
    g_blank_cursor = blank;
    g_suppress_next_motion = 0;
    g_active = 1;

    if (pthread_create(&g_thread, NULL, usbridge_event_thread, NULL) != 0) {
        g_active = 0;
        if (g_blank_cursor != None) {
            XUndefineCursor(dpy, win);
            XFreeCursor(dpy, g_blank_cursor);
            g_blank_cursor = None;
        }
        XCloseDisplay(dpy);
        g_dpy = NULL;
        g_win = 0;
        return 0;
    }
    g_thread_running = 1;

    XFlush(dpy);
    return 1;
}

void usbridge_mouse_capture_stop(void) {
    if (!g_active) {
        return;
    }
    g_active = 0;

    if (g_dpy != NULL && g_win != 0) {
        XClientMessageEvent wake;
        memset(&wake, 0, sizeof(wake));
        wake.type = ClientMessage;
        wake.window = g_win;
        wake.message_type = XInternAtom(g_dpy, "USBRIDGE_MOUSE_CAPTURE_WAKE", False);
        wake.format = 32;
        XSendEvent(g_dpy, g_win, False, NoEventMask, (XEvent *)&wake);
        XFlush(g_dpy);
    }

    if (g_thread_running) {
        pthread_join(g_thread, NULL);
        g_thread_running = 0;
    }

    if (g_dpy != NULL) {
        if (g_blank_cursor != None) {
            XUndefineCursor(g_dpy, g_win);
            XFreeCursor(g_dpy, g_blank_cursor);
            g_blank_cursor = None;
        }
        XCloseDisplay(g_dpy);
        g_dpy = NULL;
    }
    g_win = 0;
}

#endif
