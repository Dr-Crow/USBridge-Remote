//go:build darwin && !ios

#ifndef USBRIDGE_MOUSE_CAPTURE_DARWIN_H
#define USBRIDGE_MOUSE_CAPTURE_DARWIN_H

#include <stdint.h>

// Starts local (app-scoped) raw relative mouse capture on the given
// NSWindow*, passed as a uintptr_t (same convention as metal_video_create in
// internal/service/metal_video_impl_darwin.m) so this header stays C, not
// Objective-C, and the Go side never needs an unsafe.Pointer conversion.
// Uses NSEvent's *local* event monitor -- not a global CGEventTap/NSEvent
// global monitor -- so it needs no Accessibility or Input Monitoring
// permission on macOS: local monitors only ever see events already
// addressed to this app's own windows.
//
// Returns 1 on success, 0 on failure (e.g. window is nil).
int usbridge_mouse_capture_start(uintptr_t nsWindowPtr);

// Removes the event monitor and restores normal cursor association/
// visibility. Safe to call even if start failed or was never called.
void usbridge_mouse_capture_stop(void);

#endif
