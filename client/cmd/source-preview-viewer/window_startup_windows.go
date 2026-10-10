//go:build windows && cgo

package main

import (
	"os"
	"syscall"
	"unsafe"

	"fyne.io/fyne/v2/driver"
)

// Called synchronously on Fyne's main thread immediately after Show, before
// starting a renderer or network connection. Inspect only Fyne's own HWND.
func previewWindowFailure(window any) string {
	native, ok := window.(driver.NativeWindow)
	if !ok {
		return "window_driver_unavailable"
	}
	var handle uintptr
	native.RunNative(func(value any) {
		if context, ok := value.(driver.WindowsWindowContext); ok {
			handle = context.HWND
		}
	})
	if handle == 0 {
		return "window_unavailable"
	}
	u := syscall.NewLazyDLL("user32.dll")
	var pid uint32
	thread, _, _ := u.NewProc("GetWindowThreadProcessId").Call(handle, uintptr(unsafe.Pointer(&pid)))
	if thread == 0 || pid != uint32(os.Getpid()) {
		return "window_identity_failed"
	}
	var title [256]uint16
	n, _, _ := u.NewProc("GetWindowTextW").Call(handle, uintptr(unsafe.Pointer(&title[0])), uintptr(len(title)))
	if n == 0 || n >= uintptr(len(title)-1) || syscall.UTF16ToString(title[:n]) != "Source preview (experimental, this computer)" {
		return "window_identity_failed"
	}
	visible, _, _ := u.NewProc("IsWindowVisible").Call(handle)
	if visible == 0 {
		return "window_not_visible"
	}
	return ""
}
