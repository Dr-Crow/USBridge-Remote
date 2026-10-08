// Package localcomponents stages administrator-selected components without any
// vendor lookup, license issuance or public fallback. Runtime modification and
// component launch consent remain the caller's separate responsibility.
package localcomponents

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"usbridge_agent/internal/netpolicy"
)

const maxManifest = 1 << 20
const maxFile = 512 << 20

var stageMu sync.Mutex
var prepared sync.Map // process-only state/name -> verified installation

type installation struct {
	result  Result
	options Options
	name    string
}

func cacheKey(state, name string) string { return filepath.Clean(state) + "\x00" + name }
func PreparedPath(state, name string) string {
	if v, ok := prepared.Load(cacheKey(state, name)); ok {
		return v.(installation).result.Binary
	}
	return ""
}
func PreparedResult(state, name string) Result {
	if v, ok := prepared.Load(cacheKey(state, name)); ok {
		return v.(installation).result
	}
	return Result{}
}
func IsPreparedPath(p string) bool {
	ok := false
	prepared.Range(func(_, v any) bool {
		if v.(installation).result.Binary == p {
			ok = true
			return false
		}
		return true
	})
	return ok
}

// VerifyPrepared rehashes the original installation immediately before launch.
// UI/status reads use PreparedPath and never perform disk or network I/O.
func VerifyPrepared(p string) error {
	var found *installation
	prepared.Range(func(_, v any) bool {
		i := v.(installation)
		if i.result.Binary == p {
			found = &i
			return false
		}
		return true
	})
	if found == nil {
		return errors.New("component has no verified local installation")
	}
	_, err := Resolve(context.Background(), Options{StateDir: found.options.StateDir, ManifestSHA256: found.options.ManifestSHA256}, found.name)
	return err
}
func remember(r Result, o Options, name string, raw []byte) Result {
	o.ManifestSHA256 = digest(raw)
	prepared.Store(cacheKey(o.StateDir, name), installation{r, o, name})
	return r
}

type File struct {
	Path       string `json:"path"`
	SHA256     string `json:"sha256"`
	Size       int64  `json:"size"`
	Executable bool   `json:"executable,omitempty"`
}
type Component struct {
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Version  string `json:"version"`
	Profile  string `json:"profile"`
	Entry    string `json:"entry"`
	Files    []File `json:"files"`
}
type Manifest struct {
	Schema     int         `json:"schema"`
	Components []Component `json:"components"`
}
type Options struct{ StateDir, Directory, Bundle, Mirror, ManifestSHA256, CAFile string }
type Result struct{ Binary, Version, Profile, PreviousDirectory string }

func safePath(p string) bool {
	if p == "" || p == "." || path.Clean(p) != p || strings.ContainsAny(p, "\\:%") || !filepath.IsLocal(p) {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return false
		}
		base := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" {
			return false
		}
		if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
			return false
		}
	}
	return true
}

func validHash(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == sha256.Size
}
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func component(raw []byte, name, platform string, expected string) (Component, error) {
	if expected != "" && (!validHash(expected) || !strings.EqualFold(digest(raw), expected)) {
		return Component{}, errors.New("component manifest checksum mismatch")
	}
	var m Manifest
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if err := d.Decode(&m); err != nil {
		return Component{}, fmt.Errorf("manifest: %w", err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return Component{}, errors.New("manifest has trailing data")
	}
	if m.Schema != 1 {
		return Component{}, errors.New("unsupported component manifest schema")
	}
	seen := map[string]bool{}
	var found *Component
	for _, c := range m.Components {
		key := c.Name + "/" + c.Platform
		if seen[key] || !safePath(c.Name) || strings.Contains(c.Name, "/") || c.Version == "" || c.Profile == "" {
			return Component{}, errors.New("invalid/duplicate component identity")
		}
		seen[key] = true
		if !safePath(c.Entry) || len(c.Files) == 0 {
			return Component{}, errors.New("invalid component entry/files")
		}
		paths := map[string]bool{}
		entry := false
		for _, f := range c.Files {
			if !safePath(f.Path) || strings.EqualFold(f.Path, "manifest.json") || paths[strings.ToLower(f.Path)] || !validHash(f.SHA256) || f.Size <= 0 || f.Size > maxFile {
				return Component{}, errors.New("invalid/duplicate component file")
			}
			paths[strings.ToLower(f.Path)] = true
			entry = entry || f.Path == c.Entry
		}
		if !entry {
			return Component{}, errors.New("entry is absent from verified files")
		}
		if c.Name == name && c.Platform == platform {
			copy := c
			found = &copy
		}
	}
	if found == nil {
		return Component{}, fmt.Errorf("manifest has no %s for %s", name, platform)
	}
	return *found, nil
}

type source interface {
	read(context.Context, string, int64) ([]byte, error)
	close() error
}
type directory struct{ root *os.Root }

func openDirectory(p string) (source, error) {
	st, err := os.Lstat(p)
	if err != nil {
		return nil, err
	}
	if st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
		return nil, errors.New("component directory must be a real directory")
	}
	r, err := os.OpenRoot(p)
	if err != nil {
		return nil, err
	}
	return directory{r}, nil
}
func boundedRead(r io.Reader, max int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, errors.New("component file exceeds declared size")
	}
	return b, nil
}
func (s directory) read(_ context.Context, p string, max int64) ([]byte, error) {
	if !safePath(p) {
		return nil, errors.New("unsafe component path")
	}
	parts := strings.Split(p, "/")
	for i := range parts {
		st, err := s.root.Lstat(filepath.FromSlash(strings.Join(parts[:i+1], "/")))
		if err != nil {
			return nil, err
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("component symlinks are forbidden")
		}
	}
	f, err := s.root.Open(filepath.FromSlash(p))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, errors.New("component is not a regular file")
	}
	return boundedRead(f, max)
}
func (s directory) close() error { return s.root.Close() }

type bundle struct {
	zip   *zip.ReadCloser
	files map[string]*zip.File
}

func openBundle(p string) (source, error) {
	st, err := os.Lstat(p)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, errors.New("bundle must be a regular file")
	}
	z, err := zip.OpenReader(p)
	if err != nil {
		return nil, err
	}
	s := bundle{z, map[string]*zip.File{}}
	seen := map[string]bool{}
	for _, f := range z.File {
		name := strings.TrimSuffix(f.Name, "/")
		if !safePath(name) || f.Mode()&os.ModeSymlink != 0 || seen[strings.ToLower(name)] {
			z.Close()
			return nil, errors.New("unsafe/symlink/duplicate ZIP entry")
		}
		seen[strings.ToLower(name)] = true
		if !f.FileInfo().IsDir() {
			s.files[f.Name] = f
		}
	}
	return s, nil
}
func (s bundle) read(_ context.Context, p string, max int64) ([]byte, error) {
	f := s.files[p]
	if f == nil {
		return nil, fmt.Errorf("bundle missing %s", p)
	}
	if f.UncompressedSize64 > uint64(max) {
		return nil, errors.New("ZIP entry exceeds declared size")
	}
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return boundedRead(r, max)
}
func (s bundle) close() error { return s.zip.Close() }

type mirror struct {
	base   string
	client *http.Client
}

func (s mirror) read(ctx context.Context, p string, max int64) ([]byte, error) {
	if !safePath(p) {
		return nil, errors.New("unsafe mirror path")
	}
	u, err := url.JoinPath(s.base, p)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("local mirror HTTP %d", resp.StatusCode)
	}
	return boundedRead(resp.Body, max)
}
func (s mirror) close() error { s.client.CloseIdleConnections(); return nil }

func selectedSource(o Options) (source, error) {
	if o.Directory != "" {
		return openDirectory(o.Directory)
	}
	if o.Bundle != "" {
		return openBundle(o.Bundle)
	}
	if o.Mirror != "" {
		u, err := netpolicy.LocalURL(o.Mirror)
		if err != nil {
			return nil, err
		}
		if u.RawQuery != "" {
			return nil, errors.New("mirror base must not have a query")
		}
		if !validHash(o.ManifestSHA256) {
			return nil, errors.New("LAN mirror requires pinned manifest SHA256")
		}
		var ca []byte
		if o.CAFile != "" {
			ca, err = os.ReadFile(o.CAFile)
			if err != nil {
				return nil, err
			}
		}
		c, err := netpolicy.LocalHTTPClient(ca)
		if err != nil {
			return nil, err
		}
		return mirror{o.Mirror, c}, nil
	}
	return nil, errors.New("component absent: select a local directory, ZIP bundle or explicit LAN mirror; public fallback is disabled")
}
func verify(ctx context.Context, s source, c Component) error {
	for _, f := range c.Files {
		b, err := s.read(ctx, f.Path, f.Size)
		if err != nil {
			return err
		}
		if int64(len(b)) != f.Size || !strings.EqualFold(digest(b), f.SHA256) {
			return fmt.Errorf("component checksum/size mismatch: %s", f.Path)
		}
	}
	return nil
}

// Resolve never launches code. Files are verified in a fresh directory before
// activation; the prior complete installation remains available for rollback.
func Resolve(ctx context.Context, o Options, name string) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if !safePath(name) || strings.Contains(name, "/") || o.StateDir == "" {
		return Result{}, errors.New("invalid component/state directory")
	}
	stageMu.Lock()
	defer stageMu.Unlock()
	prepared.Delete(cacheKey(o.StateDir, name))
	platform := runtime.GOOS + "/" + runtime.GOARCH
	dest := filepath.Join(o.StateDir, "local-components", name)
	if s, err := openDirectory(dest); err == nil {
		raw, e := s.read(ctx, "manifest.json", maxManifest)
		if e == nil {
			c, e := component(raw, name, platform, o.ManifestSHA256)
			if e == nil && verify(ctx, s, c) == nil {
				s.close()
				return remember(Result{Binary: filepath.Join(dest, filepath.FromSlash(c.Entry)), Version: c.Version, Profile: c.Profile}, o, name, raw), nil
			}
		}
		s.close()
	}
	s, err := selectedSource(o)
	if err != nil {
		return Result{}, err
	}
	defer s.close()
	raw, err := s.read(ctx, "manifest.json", maxManifest)
	if err != nil {
		return Result{}, err
	}
	c, err := component(raw, name, platform, o.ManifestSHA256)
	if err != nil {
		return Result{}, err
	}
	base := filepath.Dir(dest)
	if err := os.MkdirAll(base, 0700); err != nil {
		return Result{}, err
	}
	if st, err := os.Lstat(base); err != nil || st.Mode()&os.ModeSymlink != 0 {
		return Result{}, errors.New("staging base must not be a symlink")
	}
	next, err := os.MkdirTemp(base, ".next-"+name+"-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(next)
	for _, f := range c.Files {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		b, err := s.read(ctx, f.Path, f.Size)
		if err != nil {
			return Result{}, err
		}
		if int64(len(b)) != f.Size || !strings.EqualFold(digest(b), f.SHA256) {
			return Result{}, fmt.Errorf("component checksum/size mismatch: %s", f.Path)
		}
		p := filepath.Join(next, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			return Result{}, err
		}
		mode := os.FileMode(0644)
		if f.Path == c.Entry || f.Executable {
			mode = 0755
		}
		if err := os.WriteFile(p, b, mode); err != nil {
			return Result{}, err
		}
	}
	if err := os.WriteFile(filepath.Join(next, "manifest.json"), raw, 0600); err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	backup := dest + ".previous"
	previous := ""
	if _, err := os.Lstat(dest); err == nil {
		if err := os.RemoveAll(backup); err != nil {
			return Result{}, err
		}
		if err := os.Rename(dest, backup); err != nil {
			return Result{}, fmt.Errorf("old component remains active: %w", err)
		}
		previous = backup
	}
	if err := os.Rename(next, dest); err != nil {
		if _, e := os.Stat(backup); e == nil {
			if restore := os.Rename(backup, dest); restore != nil {
				return Result{}, fmt.Errorf("activate: %v; restore: %w", err, restore)
			}
		}
		return Result{}, err
	}
	return remember(Result{Binary: filepath.Join(dest, filepath.FromSlash(c.Entry)), Version: c.Version, Profile: c.Profile, PreviousDirectory: previous}, o, name, raw), nil
}

// InspectManifest validates metadata without staging, executing or contacting a source.
func InspectManifest(raw []byte, name, platform, expected string) (Component, error) {
	if len(raw) > maxManifest {
		return Component{}, errors.New("component manifest exceeds size limit")
	}
	return component(raw, name, platform, expected)
}
