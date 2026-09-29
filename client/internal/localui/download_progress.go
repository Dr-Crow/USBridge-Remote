package localui

// DownloadProgress and ProgressFunc live in their own build-tag-free file
// (unlike the rest of download.go, which is //go:build !(js && wasm)) so
// cross-platform callers -- scripts_tab_widget.go's startModelsDownload,
// which compiles on every platform including wasm -- can reference the
// type in a func literal's signature without the whole call site needing
// its own wasm/native split. The wasm build never actually calls
// DownloadModels (see local_ui_download_wasm.go's doc comment: the browser
// fetches the model lazily through a different path entirely), so this
// type is otherwise unused there, just not a compile error.

// DownloadProgress reports progress on the model currently downloading,
// plus how many of the total files are already done -- enough for a UI to
// render both a per-file byte progress bar and an overall "file 2 of 3".
type DownloadProgress struct {
	File                      string
	FileDownloaded, FileTotal int64
	FilesDone, FilesTotal     int
}

// ProgressFunc is called periodically while DownloadModels runs. May be
// called from a background goroutine -- callers touching UI state must
// hop back to the UI thread themselves (see fyne.Do elsewhere in this
// codebase).
type ProgressFunc func(DownloadProgress)
