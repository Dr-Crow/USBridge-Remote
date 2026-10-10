//go:build windows && cgo

package main

/*
#cgo LDFLAGS: -luser32
#include <stdio.h>
#include <io.h>
#include <stdint.h>
#include <windows.h>

// Redirect the actual UCRT descriptors too: assigning Go's os.Stdout or only
// SetStdHandle does not affect printf/fprintf from native decoder libraries.
static int preview_redirect_crt_output(void) {
    if (!freopen("NUL", "wb", stdout)) return -1;
    if (!freopen("NUL", "wb", stderr)) return -1;
    intptr_t out = _get_osfhandle(_fileno(stdout));
    intptr_t err = _get_osfhandle(_fileno(stderr));
    if (out == -1 || err == -1) return -1;
    if (!SetHandleInformation((HANDLE)out, HANDLE_FLAG_INHERIT, 0)) return -1;
    if (!SetHandleInformation((HANDLE)err, HANDLE_FLAG_INHERIT, 0)) return -1;
    return 0;
}

// Used by the native subprocess regression test so it exercises this UCRT,
// rather than a separately loaded CRT that may have its own FILE objects.
static int preview_event_handle_is_private(uintptr_t handle) {
    DWORD flags = 0;
    return GetHandleInformation((HANDLE)handle, &flags) && !(flags & HANDLE_FLAG_INHERIT);
}

static int preview_interactive_window_station(void) {
    USEROBJECTFLAGS flags = {0};
    DWORD needed = 0;
    HWINSTA station = GetProcessWindowStation();
    return station && GetUserObjectInformationW(station, UOI_FLAGS, &flags, sizeof(flags), &needed)
        && (flags.dwFlags & WSF_VISIBLE);
}

static void preview_crt_output_probe(void) {
    fputs("native-stdout-must-not-escape\n", stdout);
    fputs("native-stderr-must-not-escape\n", stderr);
    fflush(stdout);
    fflush(stderr);
}
*/
import "C"

import (
	"errors"
	"io"
	"log"
	"os"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/windows"
)

func privateEventWriter() (*os.File, error) {
	failed := errors.New("could not isolate preview output")
	original, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	if err != nil || original == 0 || original == windows.InvalidHandle {
		return nil, failed
	}
	if kind, e := windows.GetFileType(original); e != nil || kind != windows.FILE_TYPE_PIPE {
		return nil, failed
	}
	process := windows.CurrentProcess()
	var duplicate windows.Handle
	// No inherited copy of the event channel may reach any descendant.
	if windows.DuplicateHandle(process, original, process, &duplicate, 0, false, windows.DUPLICATE_SAME_ACCESS) != nil {
		return nil, failed
	}
	out := os.NewFile(uintptr(duplicate), "preview-events")
	success := false
	defer func() {
		if !success {
			_ = out.Close()
		}
	}()
	if windows.SetHandleInformation(duplicate, windows.HANDLE_FLAG_INHERIT, 0) != nil {
		return nil, failed
	}
	// Disable inheritance on the original handles as well, even if UCRT's
	// original descriptors do not own them and freopen leaves them open.
	originals := make(map[windows.Handle]bool)
	for _, id := range []uint32{windows.STD_OUTPUT_HANDLE, windows.STD_ERROR_HANDLE} {
		h, e := windows.GetStdHandle(id)
		if e != nil || h == 0 || h == windows.InvalidHandle {
			continue
		}
		originals[h] = true
		if windows.SetHandleInformation(h, windows.HANDLE_FLAG_INHERIT, 0) != nil {
			return nil, failed
		}
	}
	if C.preview_redirect_crt_output() != 0 {
		return nil, failed
	}
	// In a GUI-subsystem process UCRT can start with detached (-2) file
	// descriptors, so freopen need not have closed the inherited Win32 pipes.
	// Revoke any leftover pipe handle before opening replacements. A handle
	// reused by freopen now names NUL (FILE_TYPE_CHAR) and must be left alone.
	// This also prevents a logger holding the old Go *os.File from leaking.
	for h := range originals {
		if kind, e := windows.GetFileType(h); e == nil && kind == windows.FILE_TYPE_PIPE {
			if windows.CloseHandle(h) != nil {
				return nil, failed
			}
		}
	}
	nullOut, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return nil, failed
	}
	nullErr, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		_ = nullOut.Close()
		return nil, failed
	}
	defer func() {
		if !success {
			_ = nullOut.Close()
			_ = nullErr.Close()
		}
	}()
	if windows.SetHandleInformation(windows.Handle(nullOut.Fd()), windows.HANDLE_FLAG_INHERIT, 0) != nil ||
		windows.SetHandleInformation(windows.Handle(nullErr.Fd()), windows.HANDLE_FLAG_INHERIT, 0) != nil ||
		windows.SetStdHandle(windows.STD_OUTPUT_HANDLE, windows.Handle(nullOut.Fd())) != nil ||
		windows.SetStdHandle(windows.STD_ERROR_HANDLE, windows.Handle(nullErr.Fd())) != nil {
		return nil, failed
	}
	os.Stdout, os.Stderr = nullOut, nullErr
	log.SetOutput(io.Discard)
	logrus.SetOutput(io.Discard)
	success = true
	return out, nil
}

// Windows has no environment-controlled display address; Fyne creates a
// window in the child's current interactive desktop. No global hooks are used.
func previewDisplayAvailable() bool { return C.preview_interactive_window_station() != 0 }
func previewCRTOutputProbe()        { C.preview_crt_output_probe() }

func previewEventHandleIsPrivate(f *os.File) bool {
	return C.preview_event_handle_is_private(C.uintptr_t(f.Fd())) != 0
}
