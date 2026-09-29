//go:build js && wasm

package api

import (
	"context"

	"usbridge-client/internal/localui"
)

// LocalUIModelsPresent is always true under wasm: the browser build has no
// local filesystem to check against and no separate "download models"
// step -- ai_vision.js's ensureSession fetches the model lazily on first
// real use instead (see local_ui_init_wasm.go's own doc comment). Reporting
// true here keeps scripts_tab_widget.go's "Local models" control a plain
// toggle on the web build, never the native-only download button.
func LocalUIModelsPresent() bool { return true }

// DownloadLocalUIModelsToDefaultDir is unreachable under wasm --
// LocalUIModelsPresent's permanent true means scripts_tab_widget.go never
// wires up the download button in the first place. Present only so this
// package compiles identically on every platform.
func DownloadLocalUIModelsToDefaultDir(ctx context.Context, onProgress localui.ProgressFunc) error {
	return nil
}
