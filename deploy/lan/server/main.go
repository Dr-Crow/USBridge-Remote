// LAN hosting contains no capture, USB drivers, entitlement issuer or relay.
package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
)

type filePin struct {
	Path       string `json:"path"`
	SHA256     string `json:"sha256"`
	Size       int64  `json:"size"`
	Executable bool   `json:"executable,omitempty"`
}
type component struct {
	Name     string    `json:"name"`
	Platform string    `json:"platform"`
	Version  string    `json:"version"`
	Profile  string    `json:"profile"`
	Entry    string    `json:"entry"`
	Files    []filePin `json:"files"`
}
type manifest struct {
	Schema     int         `json:"schema"`
	Components []component `json:"components"`
}
type host struct {
	web, mirror *os.Root
	pins        map[string]filePin
	manifest    []byte
	origins     []string
}

func localOrigin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("agent origin must be HTTPS with a literal local IP")
	}
	ip, err := netip.ParseAddr(u.Hostname())
	if err != nil || !(ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()) {
		return "", errors.New("agent origin must use a literal local IP")
	}
	if port := u.Port(); port != "" {
		if _, err := netip.ParseAddrPort(u.Host); err != nil {
			return "", errors.New("invalid origin port")
		}
	}
	return "https://" + u.Host, nil
}

func safePath(p string) bool {
	if p == "" || p == "." || path.Clean(p) != p || strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\:%") {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if strings.HasPrefix(part, ".") || strings.TrimSpace(part) != part {
			return false
		}
	}
	return true
}

// Reject symlinks even when they would resolve within the mounted root.
func regular(root *os.Root, p string) (*os.File, error) {
	if !safePath(p) {
		return nil, errors.New("invalid asset path")
	}
	parts := strings.Split(p, "/")
	for i := range parts {
		st, err := root.Lstat(strings.Join(parts[:i+1], "/"))
		if err != nil {
			return nil, err
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("symlink forbidden")
		}
	}
	f, err := root.Open(p)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("regular file required")
	}
	return f, nil
}

func newHost(webDir, mirrorDir string, origins []string) (*host, error) {
	h := &host{pins: map[string]filePin{}}
	for _, raw := range origins {
		o, err := localOrigin(raw)
		if err != nil {
			return nil, err
		}
		h.origins = append(h.origins, o)
	}
	root, err := os.OpenRoot(webDir)
	if err != nil {
		return nil, err
	}
	h.web = root
	fail := func(err error) (*host, error) { h.Close(); return nil, err }
	for _, p := range []string{"index.html", "gui.html", "app.wasm", "wasm_exec.js", "runtime-policy.js", "bootstrap.js", "ai_vision.js"} {
		f, e := regular(h.web, p)
		if e != nil {
			return fail(fmt.Errorf("required web asset %s: %w", p, e))
		}
		st, e := f.Stat()
		f.Close()
		if e != nil || st.Size() == 0 {
			return fail(errors.New("empty web asset"))
		}
	}
	if mirrorDir == "" {
		return h, nil
	}
	h.mirror, err = os.OpenRoot(mirrorDir)
	if err != nil {
		return fail(err)
	}
	f, err := regular(h.mirror, "manifest.json")
	if err != nil {
		return fail(err)
	}
	raw, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	f.Close()
	if err != nil || len(raw) > 1<<20 {
		return fail(errors.New("manifest read/size failure"))
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var m manifest
	if err := dec.Decode(&m); err != nil {
		return fail(err)
	}
	if dec.Decode(new(any)) != io.EOF || m.Schema != 1 || len(m.Components) == 0 {
		return fail(errors.New("invalid manifest schema"))
	}
	for _, c := range m.Components {
		if c.Name == "" || c.Platform == "" || c.Version == "" || len(c.Files) == 0 {
			return fail(errors.New("incomplete component"))
		}
		entry := false
		for _, p := range c.Files {
			hash, e := hex.DecodeString(p.SHA256)
			if !safePath(p.Path) || strings.EqualFold(p.Path, "manifest.json") || e != nil || len(hash) != 32 || p.Size <= 0 || p.Size > 512<<20 {
				return fail(errors.New("invalid component file pin"))
			}
			key := strings.ToLower(p.Path)
			if prior, ok := h.pins[key]; ok && prior != p {
				return fail(errors.New("conflicting/case-colliding file pins"))
			}
			h.pins[key] = p
			if c.Entry == p.Path {
				entry = true
			}
		}
		if !entry {
			return fail(errors.New("component entry must be declared"))
		}
	}
	h.manifest = raw
	return h, nil
}

func (h *host) Close() {
	if h.web != nil {
		h.web.Close()
	}
	if h.mirror != nil {
		h.mirror.Close()
	}
}

func (h *host) connectSources() string {
	sources := append([]string{}, h.origins...)
	for _, origin := range h.origins {
		sources = append(sources, "wss"+strings.TrimPrefix(origin, "https"))
	}
	return strings.Join(sources, " ")
}

func (h *host) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
	w.Header().Set("Cross-Origin-Embedder-Policy", "require-corp")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; media-src 'self' blob:; worker-src 'self' blob:; connect-src 'self' "+h.connectSources()+"; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", 405)
		return
	}
	if r.URL.RawQuery != "" || strings.Contains(r.URL.EscapedPath(), "%") {
		http.NotFound(w, r)
		return
	}
	switch r.URL.Path {
	case "/healthz":
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			io.WriteString(w, `{"status":"ok","strictLAN":true}`)
		}
		return
	case "/runtime-config.js":
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		b, _ := json.Marshal(struct {
			Strict  bool     `json:"strictLAN"`
			Origins []string `json:"agentOrigins"`
		}{true, h.origins})
		if r.Method == http.MethodGet {
			fmt.Fprintf(w, "globalThis.USBridgeRuntimeConfig=Object.freeze(%s);\n", b)
		}
		return
	case "/components/manifest.json":
		if h.mirror == nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		http.ServeContent(w, r, "manifest.json", time.Time{}, bytes.NewReader(h.manifest))
		return
	}
	p := strings.TrimPrefix(r.URL.Path, "/")
	root := h.web
	var pin *filePin
	if strings.HasPrefix(p, "components/") {
		p = strings.TrimPrefix(p, "components/")
		root = h.mirror
		v, ok := h.pins[strings.ToLower(p)]
		if root == nil || !ok || p != v.Path {
			http.NotFound(w, r)
			return
		}
		pin = &v
	} else {
		if p == "" {
			p = "index.html"
		}
		// Only runtime asset types/locations. No directory listing or test sources.
		ext := path.Ext(p)
		if !safePath(p) || !(p == "index.html" || p == "gui.html" || p == "app.wasm" || p == "wasm_exec.js" || p == "runtime-policy.js" || p == "bootstrap.js" || p == "ai_vision.js" || (strings.HasPrefix(p, "models/") && ext == ".onnx") || (strings.HasPrefix(p, "vendor/ort/") && (ext == ".wasm" || ext == ".mjs"))) {
			http.NotFound(w, r)
			return
		}
	}
	f, err := regular(root, p)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	if pin != nil {
		st, _ := f.Stat()
		if st.Size() != pin.Size {
			http.Error(w, "component verification failed", 409)
			return
		}
		sum := sha256.New()
		if _, err = io.Copy(sum, io.LimitReader(f, pin.Size+1)); err != nil || !strings.EqualFold(hex.EncodeToString(sum.Sum(nil)), pin.SHA256) {
			http.Error(w, "component verification failed", 409)
			return
		}
		f.Seek(0, io.SeekStart)
	}
	contentType := mime.TypeByExtension(path.Ext(p))
	if path.Ext(p) == ".wasm" {
		contentType = "application/wasm"
	}
	if path.Ext(p) == ".mjs" {
		contentType = "text/javascript"
	}
	if path.Ext(p) == ".onnx" {
		contentType = "application/octet-stream"
	}
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	http.ServeContent(w, r, path.Base(p), time.Time{}, f)
}

func main() {
	addr := flag.String("listen", ":8443", "HTTPS listen address")
	web := flag.String("web", "/web", "source-built static assets")
	mirror := flag.String("components", "", "optional read-only manifest directory")
	cert := flag.String("cert", "", "explicit TLS certificate PEM")
	key := flag.String("key", "", "explicit TLS private-key PEM")
	origins := flag.String("agent-origins", "", "comma-separated private-IP HTTPS agent origins")
	flag.Parse()
	if *cert == "" || *key == "" {
		log.Fatal("TLS certificate and key are required")
	}
	pair, err := tls.LoadX509KeyPair(*cert, *key)
	if err != nil {
		log.Fatal("TLS certificate/key unavailable or invalid")
	}
	var allowed []string
	if *origins != "" {
		allowed = strings.Split(*origins, ",")
	}
	h, err := newHost(*web, *mirror, allowed)
	if err != nil {
		log.Fatal(err)
	}
	defer h.Close()
	server := &http.Server{Addr: *addr, Handler: h, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 2 * time.Minute, IdleTimeout: 30 * time.Second, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}}}
	log.Print("source LAN HTTPS host starting; capture/USB remain on native hosts")
	log.Fatal(server.ListenAndServeTLS("", ""))
}
