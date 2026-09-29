//go:build !(js && wasm)

package localui

// Downloads the local ui.parse ONNX models on demand instead of shipping
// them inside every platform's installer. The files themselves stay
// committed under models/ (see that directory's README -- a plain `git
// clone` of this open repo must still build/run the local offload path
// with no extra steps), but scripts/build_{macos,windows,linux}.sh no
// longer copy them into the packaged app: at ~88MB combined, bundling them
// unconditionally into EVERY install cost real download size and disk
// space for users who never turn on "Local models" or AI Vision, the only
// two features that need them.
//
// Fetched straight from raw.githubusercontent.com at a PINNED commit SHA
// (not a branch -- a branch can move, a commit can't), the same repo/path
// the files already live at. No new hosting, no release process, no
// separate versioning scheme to keep in sync: bumping the model files
// means re-pinning modelsCommitSHA (and the sizes/hashes below) to the
// commit that changed them, nothing else.
//
// SHA-256-verified end to end, same spirit as internal/update's signed
// manifest -- a compromised/MITM'd download can at best serve bytes that
// fail their own recorded hash, never something this code will accept and
// hand to onnxruntime.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// modelsCommitSHA pins exactly which commit's copy of models/*.onnx these
// URLs/hashes describe -- see this file's own doc comment for why a commit,
// not a branch.
const modelsCommitSHA = "eee2fc418f4f462a1fd0b67b82ee3ccad0bc9028"

// Model describes one downloadable ONNX weight file.
type Model struct {
	Filename string
	SHA256   string
	Size     int64 // expected byte count, for a progress bar before the response headers arrive
}

// Models is the fixed set of files DownloadModels fetches and NewParser's
// default Config expects to find in the model directory.
var Models = []Model{
	{Filename: "icon_detect.onnx", SHA256: "2c4e4414c329a6d9dc967f2757d02c53b1bdb76d1f903e199c880c9d5ae9d515", Size: 80428860},
	{Filename: "dbnet.onnx", SHA256: "9aae32bbd3e70ff035e493e51f15ae254a56dec923b74124199a6520605bd2cd", Size: 2434297},
	{Filename: "svtr.onnx", SHA256: "1c8d08a309032e31df69e21ea08d8bde643e48e8d37716ed3ceac8802457a6d0", Size: 8989019},
}

// TotalModelBytes is the combined download size of every file in Models --
// what a "download models (~88 MB)" button should show before the first
// byte of the first file has even arrived.
func TotalModelBytes() int64 {
	var total int64
	for _, m := range Models {
		total += m.Size
	}
	return total
}

// modelURLFunc is a var (not a plain func) purely so download_test.go can
// point it at an httptest server instead of the real
// raw.githubusercontent.com -- production code should never reassign it.
var modelURLFunc = func(filename string) string {
	return fmt.Sprintf("https://raw.githubusercontent.com/USBridge-Technologies/USBridge-Remote/%s/client/internal/localui/models/%s", modelsCommitSHA, filename)
}

// ModelsPresent reports whether every file in Models already exists at its
// expected size under dir -- cheap enough (three os.Stat calls, no hashing)
// to call on every UI render deciding whether to show a checkbox or a
// "download models" button.
func ModelsPresent(dir string) bool {
	return modelsPresentIn(dir, Models)
}

func modelsPresentIn(dir string, models []Model) bool {
	for _, m := range models {
		fi, err := os.Stat(filepath.Join(dir, m.Filename))
		if err != nil || fi.Size() != m.Size {
			return false
		}
	}
	return true
}

// DownloadModels fetches every file in Models into dir (created if
// missing), verifying each against its pinned SHA-256 before it becomes
// visible under its real filename -- a network failure, a cancelled
// context, or a hash mismatch never leaves a partial/corrupt file where
// ModelsPresent or NewParser would find it. Already-present, correctly-
// sized files are skipped (safe to call again after a partial failure
// without re-downloading what already succeeded).
func DownloadModels(ctx context.Context, dir string, onProgress ProgressFunc) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create model directory %s: %w", dir, err)
	}
	client := &http.Client{} // no per-request Timeout: ctx governs cancellation: an 88MB fetch on a slow link can legitimately take minutes.
	for i, m := range Models {
		if fi, err := os.Stat(filepath.Join(dir, m.Filename)); err == nil && fi.Size() == m.Size {
			if onProgress != nil {
				onProgress(DownloadProgress{File: m.Filename, FileDownloaded: m.Size, FileTotal: m.Size, FilesDone: i, FilesTotal: len(Models)})
			}
			continue
		}
		if err := downloadOneModel(ctx, client, dir, m, i, len(Models), onProgress); err != nil {
			return fmt.Errorf("%s: %w", m.Filename, err)
		}
	}
	return nil
}

func downloadOneModel(ctx context.Context, client *http.Client, dir string, m Model, idx, total int, onProgress ProgressFunc) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelURLFunc(m.Filename), nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}

	// Download into a temp file in the SAME directory (so the final rename
	// is same-filesystem and atomic, not a cross-device copy) with a name
	// ModelsPresent/NewParser will never match -- a process crash or power
	// loss mid-download leaves only an orphaned .part-* file, never a
	// truncated file at the real path that a later run would mistake for
	// complete.
	tmp, err := os.CreateTemp(dir, m.Filename+".part-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	succeeded := false
	defer func() {
		tmp.Close()
		if !succeeded {
			os.Remove(tmpPath)
		}
	}()

	total64 := resp.ContentLength
	if total64 <= 0 {
		total64 = m.Size
	}

	hasher := sha256.New()
	buf := make([]byte, 256*1024)
	var written int64
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := tmp.Write(buf[:n]); werr != nil {
				return werr
			}
			hasher.Write(buf[:n])
			written += int64(n)
			if onProgress != nil {
				onProgress(DownloadProgress{File: m.Filename, FileDownloaded: written, FileTotal: total64, FilesDone: idx, FilesTotal: total})
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	got := hex.EncodeToString(hasher.Sum(nil))
	if got != m.SHA256 {
		return fmt.Errorf("sha256 mismatch (got %s, want %s) -- refusing to install, download may be corrupt or tampered with", got, m.SHA256)
	}

	if err := os.Rename(tmpPath, filepath.Join(dir, m.Filename)); err != nil {
		return err
	}
	succeeded = true
	return nil
}
