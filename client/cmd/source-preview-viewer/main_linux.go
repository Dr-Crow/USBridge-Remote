//go:build linux && !android && cgo

package main

import (
	"github.com/sirupsen/logrus"
	"io"
	"os"
	"syscall"
)

// Preserve an event-only writer, then divert fd 1 and fd 2 before loading Fyne
// or native libraries. C printf/log output cannot corrupt the JSON channel or
// expose launch material. The host still independently enforces bounded output.
func privateEventWriter() (*os.File, error) {
	fd, err := syscall.Dup(1)
	if err != nil {
		return nil, err
	}
	syscall.CloseOnExec(fd)
	out := os.NewFile(uintptr(fd), "preview-events")
	null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		out.Close()
		return nil, err
	}
	defer null.Close()
	if err = syscall.Dup2(int(null.Fd()), 1); err == nil {
		err = syscall.Dup2(int(null.Fd()), 2)
	}
	if err != nil {
		out.Close()
		return nil, err
	}
	logrus.SetOutput(io.Discard)
	return out, nil
}

func previewDisplayAvailable() bool { return validLocalDisplay(os.Getenv("DISPLAY")) }
