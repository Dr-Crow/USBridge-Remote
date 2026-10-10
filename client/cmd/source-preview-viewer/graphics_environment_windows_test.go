//go:build windows

package main

import (
	"syscall"
	"testing"
	"unsafe"
)

func TestWindowsSoftwareGraphicsOverridesAreFixed(t *testing.T) {
	t.Setenv("GALLIUM_DRIVER", "untrusted-driver")
	t.Setenv("LIBGL_ALWAYS_SOFTWARE", "false")
	t.Setenv("MESA_LOG_FILE", "must-not-survive")
	if err := configurePreviewGraphics(); err != nil {
		t.Fatal(err)
	}
	get := syscall.NewLazyDLL("kernel32.dll").NewProc("GetEnvironmentVariableW")
	for name, want := range map[string]string{"GALLIUM_DRIVER": "llvmpipe", "LIBGL_ALWAYS_SOFTWARE": "true", "MESA_LOG_FILE": ""} {
		key, err := syscall.UTF16PtrFromString(name)
		if err != nil {
			t.Fatal(err)
		}
		var value [64]uint16
		n, _, _ := get.Call(uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(&value[0])), uintptr(len(value)))
		if n >= uintptr(len(value)) || syscall.UTF16ToString(value[:n]) != want {
			t.Fatal("Win32 graphics policy differs")
		}
	}
}
