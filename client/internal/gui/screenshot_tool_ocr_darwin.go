//go:build darwin

package gui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ocrHelperAvailable reports whether build_macos.sh managed to compile
// macos/ocr-helper/main.swift into this app bundle (see that file's doc
// comment) -- false when swiftc wasn't available on the build machine, not
// when the feature is merely unsupported on this OS (screenshot_tool_ocr_
// other.go covers that case with its own hidden menu item instead).
func ocrHelperAvailable() bool {
	_, err := ocrHelperPath()
	return err == nil
}

// ocrHelperPath resolves usbridge-ocr-helper relative to this running
// executable's own directory (Contents/MacOS/), matching findFFmpeg's own
// convention elsewhere in this package -- works from a translocated or
// arbitrarily-renamed .app without relying on a fixed install path.
func ocrHelperPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	path := filepath.Join(filepath.Dir(exe), "usbridge-ocr-helper")
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	return path, nil
}

// recognizeTextInPNG runs pngBytes through the bundled Vision.framework OCR
// helper and returns the recognized text, one line per detected text
// region in reading order -- the same engine behind Preview/Photos' Live
// Text "copy text from image". Writes pngBytes to a temp file first: the
// helper takes a file path, not stdin, because NSImage(contentsOfFile:) is
// far simpler than wiring up a data-based loader for what's already a
// throwaway file deleted right after.
func recognizeTextInPNG(pngBytes []byte) (string, error) {
	helper, err := ocrHelperPath()
	if err != nil {
		return "", fmt.Errorf("OCR helper not available in this build: %w", err)
	}

	tmp, err := os.CreateTemp("", "usbridge-ocr-*.png")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(pngBytes); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}

	out, err := exec.Command(helper, tmpPath).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("OCR helper failed: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("OCR helper failed: %w", err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}
