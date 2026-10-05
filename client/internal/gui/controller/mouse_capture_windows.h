#ifndef USBRIDGE_MOUSE_CAPTURE_WINDOWS_H
#define USBRIDGE_MOUSE_CAPTURE_WINDOWS_H

#include <stdint.h>

// Starts raw relative mouse capture on the given HWND (passed as uintptr_t,
// same convention as the macOS/metal cgo boundaries elsewhere in this
// package). Registers Raw Input (WM_INPUT, same RIDEV_INPUTSINK idiom as
// vk_video_impl_windows.c) scoped to that one window, and observes WM_INPUT
// via an in-process WH_GETMESSAGE hook on the window's own thread -- not a
// subclass of its WNDPROC, so Fyne/GLFW's own message handling is left
// completely alone. WH_GETMESSAGE with an explicit thread id is a local,
// in-process hook: Windows has no permission-prompt system for raw mouse
// input the way macOS does, so this never shows one either.
//
// Returns 1 on success, 0 on failure.
int usbridge_mouse_capture_start(uintptr_t hwndPtr);

// Unhooks and unregisters Raw Input, and restores the cursor. Safe to call
// even if start failed or was never called.
void usbridge_mouse_capture_stop(void);

#endif
