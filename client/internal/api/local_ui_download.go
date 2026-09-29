//go:build !(js && wasm)

package api

import (
	"context"

	"usbridge-client/internal/localui"
)

// LocalUIModelsPresent reports whether the ONNX models are already on disk
// at the default model directory -- scripts_tab_widget.go's Scripts&AI tab
// checks this to decide whether to show the "Local models" checkbox or a
// "Download models (~88 MB)" button in its place (see
// internal/localui/download.go's own doc comment for why these are no
// longer bundled into every install). wasm has its own always-true stub
// (local_ui_download_wasm.go) -- the browser build fetches the model
// on-demand through a completely different path (ai_vision.js), never
// through this file's os.Stat-based check.
func LocalUIModelsPresent() bool {
	return localui.ModelsPresent(localUIDefaultModelDir())
}

// DownloadLocalUIModelsToDefaultDir fetches the ONNX models (see
// internal/localui/download.go) into the same default model directory
// InitLocalUIParseFromConfig reads from when cfg.LocalUIParseModelDir is
// left unset -- so a download that finishes here is immediately usable by
// the very next LazyInitLocalUIParse call, no extra config plumbing.
func DownloadLocalUIModelsToDefaultDir(ctx context.Context, onProgress localui.ProgressFunc) error {
	return localui.DownloadModels(ctx, localUIDefaultModelDir(), onProgress)
}
