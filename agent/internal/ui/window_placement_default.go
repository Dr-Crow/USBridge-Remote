//go:build !windows

package ui

import "fyne.io/fyne/v2"

func nativeWindowFrame(fyne.Window) (windowFrame, bool) { return windowFrame{}, false }

func nativeSetWindowFrame(fyne.Window, windowFrame) bool { return false }

func nativeWindowFrameIsVisible(windowFrame) bool { return false }
