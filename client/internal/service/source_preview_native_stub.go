//go:build (!linux && !windows) || android || !cgo

package service

import (
	"fmt"
	"os"
)

func sourcePreviewSupported() bool { return false }
func sourcePreviewStartStream(w *MoonlightCgoWrapper, cfg sourcePreviewNativeConfig) moonlightStartStream {
	return func(string, []byte, string, string, int, int, int, int, int, int, *os.File, *os.File, func(error)) error {
		return fmt.Errorf("source preview requires a native Linux or Windows client")
	}
}
