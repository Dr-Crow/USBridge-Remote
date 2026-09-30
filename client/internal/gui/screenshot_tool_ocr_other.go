//go:build !darwin

package gui

import "fmt"

// ocrHelperAvailable is always false outside macOS -- "Copy Text from
// Screen" is a Live Text-style feature built on Vision.framework, which
// has no equivalent wired up on other platforms yet (see
// showScreenshotToolMenu's doc comment).
func ocrHelperAvailable() bool { return false }

func recognizeTextInPNG(_ []byte) (string, error) {
	return "", fmt.Errorf("copy text from screen is only available on macOS")
}
