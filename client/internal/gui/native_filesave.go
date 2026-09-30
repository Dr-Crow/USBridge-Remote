package gui

import (
	"path/filepath"
	"strings"
)

func withZipExt(path string) string {
	if strings.EqualFold(filepath.Ext(path), ".zip") {
		return path
	}
	return path + ".zip"
}
