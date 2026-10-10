// Package sourcepreview defines the private, one-use launch descriptor for an
// already locally authorized, same-host preview. It does not grant capture
// permission. Only the parent session manager may issue descriptors.
package sourcepreview

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultWidth           = 128
	DefaultHeight          = 72
	SchemaVersion          = 1
	Profile                = "source-preview-v1"
	MaxDescriptorBytes     = 4096
	MaxLifetime            = 30 * time.Second
	PacketSize             = 1056 // The pinned Moonlight client subtracts 32: ANNOUNCE 1024.
	AppVersion             = "7.1.431.-1"
	ServerCodecModeSupport = 1 // SCM_H264
	VideoFormat            = 1 // VIDEO_FORMAT_H264, 4:2:0, SDR only.
)

// Descriptor deliberately cannot be serialized or formatted with its key.
// Passing a pointer also prevents callers from copying its consume state.
type Descriptor struct {
	mu         sync.Mutex
	consumed   bool
	sessionID  string
	expiresAt  time.Time
	connection Connection
}

// Connection is a transient copy for the native renderer. Never persist it.
type Connection struct {
	RTSPURL                         string
	Key                             [16]byte
	KeyID                           uint32
	Width, Height, FPS, BitrateKbps int
}

func (Connection) String() string     { return "source preview connection [redacted]" }
func (c Connection) GoString() string { return c.String() }
func (Connection) MarshalJSON() ([]byte, error) {
	return nil, errors.New("source preview connection is not serializable")
}
func (*Descriptor) String() string     { return "source preview descriptor [redacted]" }
func (d *Descriptor) GoString() string { return d.String() }
func (*Descriptor) MarshalJSON() ([]byte, error) {
	return nil, errors.New("source preview descriptor is not serializable")
}
func (d *Descriptor) SessionID() string    { return d.sessionID }
func (d *Descriptor) ExpiresAt() time.Time { return d.expiresAt }

// Destroy erases this descriptor's key and makes it unusable. Go and native
// libraries may retain copies; this is not a whole-process zeroization claim.
func (d *Descriptor) Destroy() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	clear(d.connection.Key[:])
	d.consumed = true
}

// Consume moves the launch key out exactly once, including under concurrent use.
func (d *Descriptor) Consume(now time.Time) (Connection, error) {
	if d == nil {
		return Connection{}, errors.New("missing source preview descriptor")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.consumed {
		return Connection{}, errors.New("source preview descriptor already consumed")
	}
	d.consumed = true
	defer clear(d.connection.Key[:])
	if !now.Before(d.expiresAt) {
		return Connection{}, errors.New("source preview descriptor expired")
	}
	return d.connection, nil
}

type wireDescriptor struct {
	SchemaVersion int       `json:"schema_version"`
	Profile       string    `json:"profile"`
	SessionID     string    `json:"session_id"`
	RTSPURL       string    `json:"rtsp_url"`
	KeyB64        string    `json:"key_b64"`
	KeyID         uint32    `json:"key_id"`
	Width         int       `json:"width"`
	Height        int       `json:"height"`
	FPS           int       `json:"fps"`
	BitrateKbps   int       `json:"bitrate_kbps"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// Decode accepts one bounded JSON object. Errors never include any input bytes,
// including syntax errors, duplicate names or unknown field values.
func Decode(data []byte, now time.Time) (*Descriptor, error) {
	invalid := errors.New("invalid source preview descriptor")
	if len(data) == 0 || len(data) > MaxDescriptorBytes {
		return nil, invalid
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, invalid
	}
	allowed := map[string]bool{"schema_version": true, "profile": true, "session_id": true, "rtsp_url": true, "key_b64": true, "key_id": true, "width": true, "height": true, "fps": true, "bitrate_kbps": true, "expires_at": true}
	seen := make(map[string]bool)
	for dec.More() {
		tok, err = dec.Token()
		name, ok := tok.(string)
		if err != nil || !ok || !allowed[name] || seen[name] {
			return nil, invalid
		}
		seen[name] = true
		var raw json.RawMessage
		if dec.Decode(&raw) != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return nil, invalid
		}
	}
	if tok, err = dec.Token(); err != nil || tok != json.Delim('}') {
		return nil, invalid
	}
	if _, err = dec.Token(); err != io.EOF {
		return nil, invalid
	}
	// Require every v1 field, including key_id (zero is a valid uint32).
	for _, name := range []string{"schema_version", "profile", "session_id", "rtsp_url", "key_b64", "key_id", "width", "height", "fps", "bitrate_kbps", "expires_at"} {
		if !seen[name] {
			return nil, invalid
		}
	}
	var wire wireDescriptor
	dec = json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if dec.Decode(&wire) != nil {
		return nil, invalid
	}
	if wire.SchemaVersion != SchemaVersion || wire.Profile != Profile {
		return nil, invalid
	}
	if len(wire.SessionID) < 16 || len(wire.SessionID) > 128 {
		return nil, invalid
	}
	for _, c := range wire.SessionID {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return nil, invalid
		}
	}
	if err := ValidateRTSPURL(wire.RTSPURL); err != nil {
		return nil, invalid
	}
	if wire.Width < 2 || wire.Width > 640 || wire.Width%2 != 0 || wire.Height < 2 || wire.Height > 360 || wire.Height%2 != 0 || wire.FPS < 1 || wire.FPS > 30 || wire.BitrateKbps < 1 || wire.BitrateKbps > 20000 {
		return nil, invalid
	}
	if !now.Before(wire.ExpiresAt) || wire.ExpiresAt.Sub(now) > MaxLifetime {
		return nil, invalid
	}
	key, err := base64.StdEncoding.Strict().DecodeString(wire.KeyB64)
	if err != nil || len(key) != 16 {
		clear(key)
		return nil, invalid
	}
	defer clear(key)
	c := Connection{RTSPURL: wire.RTSPURL, KeyID: wire.KeyID, Width: wire.Width, Height: wire.Height, FPS: wire.FPS, BitrateKbps: wire.BitrateKbps}
	copy(c.Key[:], key)
	return &Descriptor{sessionID: wire.SessionID, expiresAt: wire.ExpiresAt, connection: c}, nil
}

// ValidateRTSPURL prohibits DNS, IPv6, credentials, query strings, fragments,
// paths and plaintext transport. The port is the supervisor's dynamic listener.
func ValidateRTSPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "rtspenc" || u.Hostname() != "127.0.0.1" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return errors.New("source preview requires encrypted IPv4 loopback RTSP")
	}
	p, err := strconv.Atoi(u.Port())
	if err != nil || p < 1 || p > 65535 || raw != fmt.Sprintf("rtspenc://127.0.0.1:%d", p) || strings.ContainsAny(raw, "\r\n\x00") {
		return errors.New("invalid source preview RTSP endpoint")
	}
	return nil
}
