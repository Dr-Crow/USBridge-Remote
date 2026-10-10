//go:build linux && !android && cgo

package service

import "os"

func sourcePreviewSupported() bool { return true }

func sourcePreviewStartStream(w *MoonlightCgoWrapper, cfg sourcePreviewNativeConfig) moonlightStartStream {
	return func(url string, key []byte, appVersion, gfeVersion string, support, format, width, height, fps, bitrate int, video, audio *os.File, onStop func(error)) error {
		return w.startStream(url, key, appVersion, gfeVersion, support, format, width, height, fps, bitrate, video, audio, onStop,
			moonlightNativeOptions{keyID: cfg.keyID, packetSize: 1056, encryptedPreview: true})
	}
}
