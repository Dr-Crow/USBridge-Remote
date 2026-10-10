// Package sourcestreamer supervises an opt-in, source-built research streamer.
// It is a separate contract from the stock streamer and never modifies vendor
// binaries, issues entitlements, or implicitly enables a capture or USB device.
package sourcestreamer

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"usbridge_agent/internal/localcomponents"
)

const Profile = "source-streamer-v1"
const MaxMessage = 64 << 10

var localDisplay = regexp.MustCompile(`^:[0-9]{1,5}(\.[0-9]{1,2})?$`)
var identifier = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

// Launch is passed over a private pipe. KeyB64 must come from the current
// explicitly authorized session; it must not be persisted, printed, or reused.
// Version 1 deliberately supports loopback and synthesized silence only.
type Launch struct {
	SchemaVersion  int    `json:"schema_version"`
	Owner          string `json:"owner"`
	SessionID      string `json:"session_id"`
	KeyB64         string `json:"key_b64"`
	KeyID          uint32 `json:"key_id"`
	PeerIP         string `json:"peer_ip"`
	VideoPort      int    `json:"video_port"`
	AudioPort      int    `json:"audio_port"`
	Display        string `json:"display"`
	CaptureConsent bool   `json:"capture_consent"`
	FFmpeg         string `json:"ffmpeg"`
	Width          int    `json:"width"`
	Height         int    `json:"height"`
	FPS            int    `json:"fps"`
	PixelFormat    string `json:"pixel_format"`
	PacketSize     int    `json:"packet_size"`
	AudioMode      string `json:"audio_mode"`
	MaxSeconds     int    `json:"max_seconds"`
}

func (r Launch) Validate() error {
	if r.SchemaVersion != 1 || !identifier.MatchString(r.Owner) || !identifier.MatchString(r.SessionID) {
		return errors.New("invalid source-streamer protocol or session identity")
	}
	key, err := base64.StdEncoding.DecodeString(r.KeyB64)
	if err != nil || len(key) != 16 {
		return errors.New("source-streamer needs a fresh 16-byte session key")
	}
	if !r.CaptureConsent {
		return errors.New("source-streamer requires explicit capture consent")
	}
	if r.PeerIP != "127.0.0.1" {
		return errors.New("source-streamer v1 only authorizes IPv4 loopback peers")
	}
	if r.VideoPort < 1 || r.VideoPort > 65535 || r.AudioPort < 1 || r.AudioPort > 65535 || r.VideoPort == r.AudioPort {
		return errors.New("invalid source-streamer media ports")
	}
	if !localDisplay.MatchString(r.Display) {
		return errors.New("source-streamer needs an explicit local X11 display")
	}
	if !filepath.IsAbs(r.FFmpeg) || strings.ContainsAny(r.FFmpeg, "\x00\r\n") {
		return errors.New("source-streamer needs an absolute trusted FFmpeg path")
	}
	if r.Width < 16 || r.Width > 7680 || r.Height < 16 || r.Height > 4320 || r.Width%2 != 0 || r.Height%2 != 0 || r.FPS < 1 || r.FPS > 120 {
		return errors.New("source-streamer video bounds exceeded")
	}
	if r.PixelFormat != "yuv420p" && r.PixelFormat != "yuv444p" {
		return errors.New("unsupported source-streamer pixel format")
	}
	if r.PacketSize < 256 || r.PacketSize > 1400 || r.AudioMode != "silence" || r.MaxSeconds < 1 || r.MaxSeconds > 300 {
		return errors.New("unsupported source-streamer media or duration limits")
	}
	return nil
}

func DecodeLaunch(r io.Reader) (Launch, error) {
	var request Launch
	raw, err := io.ReadAll(io.LimitReader(r, MaxMessage+1))
	if err != nil {
		return request, errors.New("read source-streamer launch request")
	}
	if len(raw) > MaxMessage {
		return request, errors.New("source-streamer launch request exceeds limit")
	}
	if err := strictJSON(raw, &request); err != nil {
		return Launch{}, errors.New("invalid source-streamer launch JSON")
	}
	return request, request.Validate()
}
func strictJSON(raw []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	if dec.Decode(new(any)) != io.EOF {
		return errors.New("trailing JSON data")
	}
	return nil
}

type Ready struct {
	SchemaVersion  int      `json:"schema_version"`
	Event          string   `json:"event"`
	SessionID      string   `json:"session_id"`
	RTSPAddress    string   `json:"rtsp_address"`
	ControlAddress string   `json:"control_address"`
	Capabilities   []string `json:"capabilities"`
}

func (r Ready) validate(session string) error {
	if r.SchemaVersion != 1 || r.Event != "ready" || r.SessionID != session {
		return errors.New("source-streamer readiness identity mismatch")
	}
	for _, addr := range []string{r.RTSPAddress, r.ControlAddress} {
		host, port, err := net.SplitHostPort(addr)
		n, parseErr := strconv.Atoi(port)
		if err != nil || parseErr != nil || host != "127.0.0.1" || n < 1 || n > 65535 {
			return errors.New("source-streamer advertised a non-loopback listener")
		}
	}
	if len(r.Capabilities) == 0 || len(r.Capabilities) > 32 {
		return errors.New("source-streamer returned invalid capabilities")
	}
	required := map[string]bool{"rtsp-encrypted": false, "video-x11-h264": false, "audio-silence": false, "control-enet": false}
	for _, c := range r.Capabilities {
		if _, known := required[c]; !known || required[c] {
			return errors.New("source-streamer returned unsupported or duplicate capability")
		}
		required[c] = true
	}
	for _, present := range required {
		if !present {
			return errors.New("source-streamer lacks a required v1 capability")
		}
	}
	return nil
}

type Session struct {
	Ready  Ready
	stdin  io.WriteCloser
	cmd    *exec.Cmd
	cancel context.CancelFunc
	stop   sync.Once
	done   chan struct{}
	mu     sync.Mutex
	err    error
}

// Start requires a hash-pinned manifest in addition to the administrator's
// local source selection. Only the source-streamer-v1 profile may enter this
// protocol; stock binaries cannot accidentally be launched with these flags.
func Start(ctx context.Context, source localcomponents.Options, request Launch) (*Session, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	if source.ManifestSHA256 == "" {
		return nil, errors.New("source-streamer needs a pinned component manifest SHA-256")
	}
	result, err := localcomponents.Resolve(ctx, source, "source-streamer")
	if err != nil {
		return nil, fmt.Errorf("resolve source-streamer: %w", err)
	}
	if result.Profile != Profile {
		return nil, errors.New("component does not implement source-streamer-v1")
	}
	if err := localcomponents.VerifyPrepared(result.Binary); err != nil {
		return nil, fmt.Errorf("verify source-streamer: %w", err)
	}
	return startBinary(ctx, result.Binary, request)
}

func startBinary(ctx context.Context, binary string, request Launch) (*Session, error) {
	processCtx, cancel := context.WithTimeout(ctx, time.Duration(request.MaxSeconds+15)*time.Second)
	cmd := exec.CommandContext(processCtx, binary, "--launch-stdin")
	// stdout is a bounded protocol channel. Child stderr is deliberately not
	// relayed because a malformed child could echo the session's secret request.
	cmd.Stderr = io.Discard
	cmd.Env = os.Environ()
	cmd.WaitDelay = 2 * time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		stdin.Close()
		cancel()
		return nil, errors.New("start source-streamer process")
	}
	s := &Session{stdin: stdin, cmd: cmd, cancel: cancel, done: make(chan struct{})}
	ready := make(chan Ready, 1)
	protocolError := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 4096), MaxMessage)
		first := true
		for scanner.Scan() {
			if first {
				first = false
				var event Ready
				if err := strictJSON(scanner.Bytes(), &event); err != nil {
					protocolError <- errors.New("invalid source-streamer readiness JSON")
					cancel()
					break
				}
				if err := event.validate(request.SessionID); err != nil {
					protocolError <- err
					cancel()
					break
				}
				ready <- event
			}
			// Later status frames are drained with the same bounded scanner. They are
			// not trusted to establish readiness or forwarded as arbitrary log output.
		}
		scanErr := scanner.Err()
		if scanErr != nil {
			cancel()
		}
		waitErr := cmd.Wait()
		s.mu.Lock()
		if scanErr != nil {
			s.err = errors.New("source-streamer status exceeded protocol bounds")
		} else if waitErr != nil {
			s.err = errors.New("source-streamer process failed")
		}
		s.mu.Unlock()
		cancel()
		close(s.done)
	}()
	payload, err := json.Marshal(request)
	if err == nil {
		payload = append(payload, '\n')
		_, err = stdin.Write(payload)
	}
	for i := range payload {
		payload[i] = 0
	}
	if err != nil {
		s.Stop()
		return nil, errors.New("send source-streamer launch request")
	}
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case event := <-ready:
		s.Ready = event
		return s, nil
	case err := <-protocolError:
		s.Stop()
		return nil, err
	case <-s.done:
		return nil, errors.New("source-streamer exited before readiness")
	case <-timer.C:
		s.Stop()
		return nil, errors.New("source-streamer readiness timed out")
	case <-ctx.Done():
		s.Stop()
		return nil, ctx.Err()
	}
}
func (s *Session) Done() <-chan struct{} { return s.done }
func (s *Session) Wait() error           { <-s.done; s.mu.Lock(); defer s.mu.Unlock(); return s.err }
func (s *Session) Stop() error {
	s.stop.Do(func() { s.stdin.Close() })
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	select {
	case <-s.done:
	case <-timer.C:
		s.cancel()
		<-s.done
	}
	return s.Wait()
}
