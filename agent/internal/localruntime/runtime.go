// Package localruntime implements an explicit, opt-in local research mode.
// It modifies private copies of pinned component binaries, never the downloaded
// originals. Local test tokens are never used for vendor downloads or services.
package localruntime

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

const Environment = "USBRIDGE_LOCAL_RUNTIME"
const vendorKey = "BOr0FAQyGQhmg6CZdgcRDekKrP2A++60WKvhW52tV58="

func Enabled() bool { return os.Getenv(Environment) == "1" }

type Spec struct{ Binary, Token string }
type session struct {
	files   map[string][32]byte
	dir, hw string
	private ed25519.PrivateKey
	public  ed25519.PublicKey
}

var state = struct {
	sync.Mutex
	sessions map[string]*session
}{sessions: make(map[string]*session)}

// Prepare refuses unknown versions and keeps the public download trust chain
// separate from this explicitly modified, unprivileged runtime copy.
func Prepare(source, stateDir, component, hw string) (Spec, error) {
	if !Enabled() {
		return Spec{}, fmt.Errorf("local runtime mode is disabled")
	}
	if hw == "" || stateDir == "" {
		return Spec{}, fmt.Errorf("local runtime requires hardware ID and state directory")
	}
	key := runtime.GOOS + "/" + runtime.GOARCH + "/" + component
	expected, ok := pinned[key]
	if !ok {
		return Spec{}, fmt.Errorf("local runtime unsupported for %s", key)
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return Spec{}, err
	}
	hash := sha256.Sum256(data)
	if hex.EncodeToString(hash[:]) != expected {
		return Spec{}, fmt.Errorf("local runtime refuses an unknown %s binary; only audited v0.3.131 is supported", component)
	}
	state.Lock()
	defer state.Unlock()
	s := state.sessions[stateDir]
	if s == nil {
		parent := filepath.Join(stateDir, "local-runtime")
		if err := os.MkdirAll(parent, 0700); err != nil {
			return Spec{}, err
		}
		dir, err := os.MkdirTemp(parent, "session-")
		if err != nil {
			return Spec{}, err
		}
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			os.RemoveAll(dir)
			return Spec{}, err
		}
		s = &session{dir: dir, hw: hw, private: priv, public: pub, files: make(map[string][32]byte)}
		state.sessions[stateDir] = s
	}
	if s.hw != hw {
		return Spec{}, fmt.Errorf("local runtime hardware identity changed during this process")
	}
	if err := s.writeToken(time.Now()); err != nil {
		return Spec{}, err
	}
	dir := filepath.Join(s.dir, component)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return Spec{}, err
	}
	name := "usbridge-streamer"
	if component == "usb-broker" {
		name = "usbridge-usb-broker"
	}
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	dest := filepath.Join(dir, name)
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		changed, err := rekey(data, s.public)
		if err != nil {
			return Spec{}, err
		}
		if err := os.WriteFile(dest, changed, 0700); err != nil {
			return Spec{}, err
		}
		if runtime.GOOS == "windows" && component == "rustshine" {
			// The inspected Windows release has one external codec DLL. Keep it
			// beside the copy; do not copy config, token, or credential files.
			dll, err := os.ReadFile(filepath.Join(filepath.Dir(source), "libopus-0.dll"))
			if err != nil {
				os.Remove(dest)
				return Spec{}, err
			}
			dllHash := sha256.Sum256(dll)
			if hex.EncodeToString(dllHash[:]) != "c12f9a671a106c649a5517d28061670aa57c805d242b80764853a7551099498d" {
				os.Remove(dest)
				return Spec{}, fmt.Errorf("unrecognized codec DLL")
			}
			s.files[filepath.Join(dir, "libopus-0.dll")] = dllHash
			if err := os.WriteFile(filepath.Join(dir, "libopus-0.dll"), dll, 0600); err != nil {
				os.Remove(dest)
				return Spec{}, err
			}
		}
		if runtime.GOOS == "darwin" {
			if out, err := exec.Command("/usr/bin/codesign", "--force", "--sign", "-", dest).CombinedOutput(); err != nil {
				os.Remove(dest)
				return Spec{}, fmt.Errorf("ad-hoc sign local runtime: %v: %s", err, out)
			}
		}
		finalBytes, err := os.ReadFile(dest)
		if err != nil {
			return Spec{}, err
		}
		s.files[dest] = sha256.Sum256(finalBytes)
		log.Printf("[local-runtime] EXPERIMENTAL: %s uses a modified copy of audited v0.3.131; vendor original retained", component)
	} else if err != nil {
		return Spec{}, err
	}
	expectedCopy, ok := s.files[dest]
	if !ok {
		return Spec{}, fmt.Errorf("unrecognized local runtime copy")
	}
	current, err := os.ReadFile(dest)
	if err != nil {
		return Spec{}, err
	}
	if sha256.Sum256(current) != expectedCopy {
		return Spec{}, fmt.Errorf("local runtime copy changed unexpectedly")
	}
	if runtime.GOOS == "windows" && component == "rustshine" {
		dllPath := filepath.Join(dir, "libopus-0.dll")
		dll, err := os.ReadFile(dllPath)
		if err != nil {
			return Spec{}, err
		}
		if sha256.Sum256(dll) != s.files[dllPath] {
			return Spec{}, fmt.Errorf("local codec DLL changed unexpectedly")
		}
	}
	return Spec{Binary: dest, Token: filepath.Join(s.dir, "entitlement.token")}, nil
}

func rekey(data []byte, pub ed25519.PublicKey) ([]byte, error) {
	if len(pub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid local public key")
	}
	if bytes.Count(data, []byte(vendorKey)) != 1 {
		return nil, fmt.Errorf("expected exactly one audited verification-key constant")
	}
	return bytes.Replace(data, []byte(vendorKey), []byte(base64.StdEncoding.EncodeToString(pub)), 1), nil
}

func (s *session) writeToken(now time.Time) error {
	payload, err := json.Marshal(struct {
		Provider string `json:"provider"`
		Sub      string `json:"sub"`
		Tier     string `json:"tier"`
		Iat      int64  `json:"iat"`
		Exp      int64  `json:"exp"`
	}{"desktop-device", s.hw, "pro", now.Unix(), now.Add(24 * time.Hour).Unix()})
	if err != nil {
		return err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	token := "usbent1." + encoded + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(s.private, []byte(encoded)))
	f, err := os.CreateTemp(s.dir, ".token-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.WriteString(token)
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, filepath.Join(s.dir, "entitlement.token"))
}

// Renew keeps local runtime sessions usable during a long research session.
// It makes no network requests and never changes the genuine vendor token.
func Renew(ctx context.Context) {
	if !Enabled() {
		return
	}
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			state.Lock()
			for _, s := range state.sessions {
				if err := s.writeToken(now); err != nil {
					log.Printf("[local-runtime] token renewal failed: %v", err)
				}
			}
			state.Unlock()
		}
	}
}

// Close removes only directories created by this process. No downloaded vendor
// archive, source executable, configuration, or persistent user key is removed.
func Close() {
	state.Lock()
	defer state.Unlock()
	for dir, s := range state.sessions {
		for i := range s.private {
			s.private[i] = 0
		}
		_ = os.RemoveAll(s.dir)
		delete(state.sessions, dir)
	}
}
