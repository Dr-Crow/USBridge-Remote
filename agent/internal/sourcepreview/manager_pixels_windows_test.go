//go:build windows && source_preview_manager_acceptance

// SPDX-License-Identifier: GPL-3.0-only
package sourcepreview

// These test-only primitives preserve the public Windows viewer acceptance
// observer: exact owned PID/title, memory DIB only, and no screen-DC fallback.
import (
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

const managerViewerTitle = "Source preview (experimental, this computer)"

var managerUser32 = syscall.NewLazyDLL("user32.dll")
var managerGDI32 = syscall.NewLazyDLL("gdi32.dll")
var managerEnumWindows = managerUser32.NewProc("EnumWindows")
var managerWindowPID = managerUser32.NewProc("GetWindowThreadProcessId")
var managerWindowVisible = managerUser32.NewProc("IsWindowVisible")
var managerWindowText = managerUser32.NewProc("GetWindowTextW")
var managerGetClientRect = managerUser32.NewProc("GetClientRect")
var managerPrintWindow = managerUser32.NewProc("PrintWindow")
var managerIsWindow = managerUser32.NewProc("IsWindow")
var managerCreateDC = managerGDI32.NewProc("CreateCompatibleDC")
var managerCreateDIB = managerGDI32.NewProc("CreateDIBSection")
var managerSelectObject = managerGDI32.NewProc("SelectObject")
var managerDeleteObject = managerGDI32.NewProc("DeleteObject")
var managerDeleteDC = managerGDI32.NewProc("DeleteDC")
var managerGdiFlush = managerGDI32.NewProc("GdiFlush")

func managerVerifyWindow(hwnd uintptr, pid uint32) bool {
	var actual uint32
	managerWindowPID.Call(hwnd, uintptr(unsafe.Pointer(&actual)))
	if actual != pid {
		return false
	}
	var title [256]uint16
	n, _, _ := managerWindowText.Call(hwnd, uintptr(unsafe.Pointer(&title[0])), uintptr(len(title)))
	return n > 0 && n < uintptr(len(title)-1) && syscall.UTF16ToString(title[:n]) == managerViewerTitle
}
func managerOwnedWindow(pid uint32) (uintptr, error) {
	hwnd, _, err := managerOwnedWindowSnapshot(pid)
	return hwnd, err
}

// Counts are limited to the exact owned process. Never retain or publish a
// window title, HWND, PID, or any property of another process's windows.
type managerWindowSnapshot struct{ Owned, Visible, TitleMatches int }

func managerOwnedWindowSnapshot(pid uint32) (uintptr, managerWindowSnapshot, error) {
	var found uintptr
	var snapshot managerWindowSnapshot
	ambiguous := false
	overflow := false
	callback := syscall.NewCallback(func(hwnd, lparam uintptr) uintptr {
		var owner uint32
		managerWindowPID.Call(hwnd, uintptr(unsafe.Pointer(&owner)))
		if owner != pid {
			return 1
		}
		snapshot.Owned++
		if snapshot.Owned > 128 {
			overflow = true
			return 0
		}
		visible, _, _ := managerWindowVisible.Call(hwnd)
		if visible != 0 {
			snapshot.Visible++
		}
		titleMatches := managerVerifyWindow(hwnd, pid)
		if titleMatches {
			snapshot.TitleMatches++
		}
		if visible == 0 || !titleMatches {
			return 1
		}
		if found != 0 {
			ambiguous = true
		} else {
			found = hwnd
		}
		return 1
	})
	ok, _, _ := managerEnumWindows.Call(callback, 0)
	if ok == 0 || ambiguous || overflow {
		return 0, snapshot, managerFailure("owned_window_ambiguous")
	}
	return found, snapshot, nil
}

type managerRect struct{ Left, Top, Right, Bottom int32 }
type managerBitmapHeader struct {
	Size                   uint32
	Width, Height          int32
	Planes, BitCount       uint16
	Compression, SizeImage uint32
	XPels, YPels           int32
	ClrUsed, ClrImportant  uint32
}

func managerPrintOwnedWindow(hwnd uintptr, pid uint32) (int, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// No GetDC, BitBlt, desktop/window-manager screen capture or screen fallback.
	// CreateCompatibleDC(NULL) creates only a memory DC. Its only pixel source is
	// PrintWindow called on the exact owned HWND after identity revalidation.
	if !managerVerifyWindow(hwnd, pid) {
		return 0, managerFailure("owned_window_identity_changed")
	}
	var r managerRect
	ok, _, _ := managerGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	w, h := int(r.Right-r.Left), int(r.Bottom-r.Top)
	if ok == 0 || w < 640 || h < 360 || w > 2048 || h > 1536 {
		return 0, managerFailure("owned_window_dimensions")
	}
	dc, _, _ := managerCreateDC.Call(0)
	if dc == 0 {
		return 0, managerFailure("memory_dc_failed")
	}
	defer managerDeleteDC.Call(dc)
	header := managerBitmapHeader{Size: 40, Width: int32(w), Height: -int32(h), Planes: 1, BitCount: 32}
	var bits unsafe.Pointer
	dib, _, _ := managerCreateDIB.Call(dc, uintptr(unsafe.Pointer(&header)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if dib == 0 || bits == nil {
		return 0, managerFailure("owned_dib_failed")
	}
	defer managerDeleteObject.Call(dib)
	old, _, _ := managerSelectObject.Call(dc, dib)
	if old == 0 || old == ^uintptr(0) {
		return 0, managerFailure("owned_dib_select_failed")
	}
	defer managerSelectObject.Call(dc, old)
	pixels := unsafe.Slice((*byte)(bits), w*h*4)
	clear(pixels)
	ok, _, _ = managerPrintWindow.Call(hwnd, dc, 0x1|0x2)
	if ok == 0 {
		return 0, managerFailure("printwindow_unavailable")
	}
	flushed, _, _ := managerGdiFlush.Call()
	if flushed == 0 {
		return 0, managerFailure("owned_dib_flush_failed")
	}
	if !managerVerifyWindow(hwnd, pid) {
		return 0, managerFailure("owned_window_identity_changed")
	}
	counts := [3]int{}
	for y := h * 2 / 5; y < h*3/5; y += 7 {
		for x := w * 2 / 5; x < w*3/5; x += 7 {
			i := (y*w + x) * 4
			counts[managerPixelColor(pixels[i], pixels[i+1], pixels[i+2])]++
		}
	}
	total := counts[0] + counts[1] + counts[2]
	if total < 16 {
		return 0, managerFailure("owned_pixels_missing")
	}
	for color := 1; color <= 2; color++ {
		if counts[color]*100 >= total*85 {
			return color, nil
		}
	}
	return 0, nil
}
func managerBoundedPixels(hwnd uintptr, pid uint32) (int, error) {
	type result struct {
		color int
		err   error
	}
	ch := make(chan result, 1)
	go func() { c, e := managerPrintOwnedWindow(hwnd, pid); ch <- result{c, e} }()
	select {
	case r := <-ch:
		return r.color, r.err
	case <-time.After(2 * time.Second):
		return 0, managerFailure("printwindow_timeout")
	}
}

// Samples are BGRA bytes from the owned DIB, never a screen DC.
func managerPixelColor(b, g, r byte) int {
	near := func(v byte, w int) bool { return int(v) >= w-28 && int(v) <= w+28 }
	if near(r, 22) && near(g, 75) && near(b, 180) {
		return 1
	}
	if near(r, 219) && near(g, 99) && near(b, 49) {
		return 2
	}
	return 0
}
