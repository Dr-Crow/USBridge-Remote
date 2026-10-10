//go:build windows && cgo

package main

import (
	"fyne.io/fyne/v2/driver"
	"testing"
)

type missingNativeWindow struct{ context any }

func (w missingNativeWindow) RunNative(f func(any)) { f(w.context) }

func TestWindowStartupRejectsMissingNativeWindow(t *testing.T) {
	if previewWindowFailure(struct{}{}) != "window_driver_unavailable" {
		t.Fatal("missing driver accepted")
	}
	for _, context := range []any{driver.WindowsWindowContext{}, driver.UnknownContext{}} {
		if previewWindowFailure(missingNativeWindow{context}) != "window_unavailable" {
			t.Fatal("missing native window accepted")
		}
	}
}
