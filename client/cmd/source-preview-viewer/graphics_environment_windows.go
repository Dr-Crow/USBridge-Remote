//go:build windows

package main

import "os"

func configurePreviewGraphics() error {
	if err := clearGraphicsEnvironmentWith(os.Environ(), os.Unsetenv); err != nil {
		return err
	}
	// The pinned Mesa WGL implementation reads GetEnvironmentVariableA rather
	// than a separately initialized CRT environment. Go's Setenv updates it.
	// A system OpenGL implementation can ignore these Mesa-specific variables.
	return setSoftwareGraphicsWith(os.Setenv)
}
