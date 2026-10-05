//go:build !linux

package benchvideo

import (
	"errors"
	"time"
)

// moveToOutput is only implemented for KWin (see output_linux.go).
func moveToOutput(pid int, prefix string, timeout time.Duration) (string, error) {
	return "", errors.New("moving the test video to a compositor output is not supported on this platform")
}
