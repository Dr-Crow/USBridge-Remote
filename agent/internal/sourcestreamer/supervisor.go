// Package sourcestreamer supervises an opt-in, source-built research streamer.
// It is a separate contract from the stock streamer and never modifies vendor
// binaries, issues entitlements, or implicitly enables a capture or USB device.
package sourcestreamer

import (
	"bufio"
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
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"usbridge_agent/internal/componentjson"
	"usbridge_agent/internal/localcomponents"
)

const Profile = "source-streamer-v1"
const MaxMessage = 64 << 10

var ErrFrameTooLarge = errors.New("source-streamer frame exceeds the bounded packetizer limit")

var localDisplay = regexp.MustCompile(`^:[0-9]{1,5}(\.[0-9]{1,2})?$`)
var identifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var windowsFFmpeg = regexp.MustCompile(`^[A-Za-z]:[\\/].+\.[Ee][Xx][Ee]$`)

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
	InputConsent   bool   `json:"input_consent,omitempty"`
}

func (r Launch) Validate() error {
	return r.validatePlatform(runtime.GOOS)
}

func (r Launch) validatePlatform(platform string) error {
	if r.SchemaVersion != 1 || len(r.Owner) == 0 || len(r.Owner) > 256 || !identifier.MatchString(r.SessionID) {
		return errors.New("invalid source-streamer protocol or session identity")
	}
	key, err := base64.StdEncoding.Strict().DecodeString(r.KeyB64)
	if err != nil || len(key) != 16 || len(r.KeyB64) != base64.StdEncoding.EncodedLen(16) {
		return errors.New("source-streamer needs a fresh 16-byte session key")
	}
	if !r.CaptureConsent {
		return errors.New("source-streamer requires explicit capture consent")
	}
	if r.PeerIP != "127.0.0.1" {
		return errors.New("source-streamer v1 only authorizes IPv4 loopback peers")
	}
	if r.VideoPort < 0 || r.VideoPort > 65535 || r.AudioPort < 0 || r.AudioPort > 65535 || (r.VideoPort == r.AudioPort && r.VideoPort != 0) {
		return errors.New("invalid source-streamer media ports")
	}
	if platform == "windows" {
		if r.Display != "desktop" || r.InputConsent {
			return errors.New("Windows source-streamer requires desktop selection and does not support input")
		}
		if !windowsFFmpeg.MatchString(r.FFmpeg) {
			return errors.New("Windows source-streamer requires a local-drive FFmpeg executable")
		}
	} else if !localDisplay.MatchString(r.Display) {
		return errors.New("source-streamer needs an explicit local X11 display")
	}
	if (platform != "windows" && !filepath.IsAbs(r.FFmpeg)) || len(r.FFmpeg) > 4096 || strings.ContainsAny(r.FFmpeg, "\x00\r\n") {
		return errors.New("source-streamer needs an absolute trusted FFmpeg path")
	}
	if r.Width < 2 || r.Width > 1920 || r.Height < 2 || r.Height > 1080 || r.Width%2 != 0 || r.Height%2 != 0 || r.FPS < 1 || r.FPS > 120 {
		return errors.New("source-streamer video bounds exceeded")
	}
	if r.PixelFormat != "yuv420p" && r.PixelFormat != "yuv444p" {
		return errors.New("unsupported source-streamer pixel format")
	}
	if r.PacketSize < 64 || r.PacketSize > 1408 || r.PacketSize%16 != 0 || r.AudioMode != "silence" || r.MaxSeconds < 1 || r.MaxSeconds > 300 {
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
func strictJSON(raw []byte, out any) error { return componentjson.Decode(raw, out) }

type Stats struct {
	VideoFrames        uint64 `json:"video_frames"`
	VideoPackets       uint64 `json:"video_packets"`
	AudioPackets       uint64 `json:"audio_packets"`
	AudioParityPackets uint64 `json:"audio_parity_packets"`
}
type Stopped struct {
	SchemaVersion int    `json:"schema_version"`
	Event         string `json:"event"`
	SessionID     string `json:"session_id"`
	Reason        string `json:"reason"`
	FailureCode   string `json:"failure_code,omitempty"`
	Stats         *Stats `json:"stats"`
}

type Ready struct {
	SchemaVersion  int      `json:"schema_version"`
	Event          string   `json:"event"`
	SessionID      string   `json:"session_id"`
	RTSPAddress    string   `json:"rtsp_address"`
	ControlAddress string   `json:"control_address"`
	Capabilities   []string `json:"capabilities"`
}

func (s Stopped) validate(session string) error {
	if s.Stats == nil || s.SchemaVersion != 1 || s.Event != "stopped" || s.SessionID != session || (s.Reason != "completed" && s.Reason != "failed") {
		return errors.New("invalid source-streamer terminal status")
	}
	if s.FailureCode != "" && (s.Reason != "failed" || s.FailureCode != "frame_too_large") {
		return errors.New("invalid source-streamer failure code")
	}
	return nil
}

func (r Ready) validate(session string, inputConsent bool, videoCapability string) error {
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
	if videoCapability != "video-x11-h264" && videoCapability != "video-windows-gdi-h264" {
		return errors.New("unsupported source-streamer video platform")
	}
	if videoCapability == "video-windows-gdi-h264" && inputConsent {
		return errors.New("Windows source-streamer input is unsupported")
	}
	required := map[string]bool{"rtsp-encrypted": false, videoCapability: false, "audio-silence": false, "control-enet": false}
	inputAvailable := false
	for _, c := range r.Capabilities {
		if c == "input-x11-keyboard-mouse" {
			if !inputConsent || inputAvailable {
				return errors.New("source-streamer input lacks consent or is duplicated")
			}
			inputAvailable = true
			continue
		}
		if _, known := required[c]; !known || required[c] {
			return errors.New("source-streamer returned unsupported or duplicate capability")
		}
		required[c] = true
	}
	if inputConsent && !inputAvailable {
		return errors.New("source-streamer lacks consented input support")
	}
	for _, present := range required {
		if !present {
			return errors.New("source-streamer lacks a required v1 capability")
		}
	}
	return nil
}

type Session struct {
	Ready    Ready
	stdin    io.WriteCloser
	cmd      *exec.Cmd
	cancel   context.CancelFunc
	stop     sync.Once
	done     chan struct{}
	mu       sync.Mutex
	err      error
	terminal *Stopped
}

// Start requires a hash-pinned manifest in addition to the administrator's
// local source selection. Only the source-streamer-v1 profile may enter this
// protocol; stock binaries cannot accidentally be launched with these flags.
func Start(ctx context.Context, source localcomponents.Options, request Launch) (*Session, error) {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		return nil, errors.New("source-streamer v1 requires Linux X11 or Windows GDI with ENet")
	}
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
	configurePipeProcess(cmd)
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
	cmd.Cancel = func() error { _ = stdin.Close(); return nil }
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
	startupDeadline := time.AfterFunc(10*time.Second, cancel)
	defer startupDeadline.Stop()
	ready := make(chan Ready, 1)
	protocolError := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 4096), MaxMessage)
		first := true
		var protocolErr error
		for scanner.Scan() {
			if first {
				first = false
				var event Ready
				if err := strictJSON(scanner.Bytes(), &event); err != nil {
					protocolError <- errors.New("invalid source-streamer readiness JSON")
					cancel()
					break
				}
				videoCapability := "video-x11-h264"
				if request.Display == "desktop" {
					videoCapability = "video-windows-gdi-h264"
				}
				if err := event.validate(request.SessionID, request.InputConsent, videoCapability); err != nil {
					protocolError <- err
					cancel()
					break
				}
				ready <- event
				continue
			}
			var terminal Stopped
			if err := strictJSON(scanner.Bytes(), &terminal); err != nil || terminal.validate(request.SessionID) != nil {
				protocolErr = errors.New("invalid source-streamer terminal status")
				cancel()
				break
			}
			s.mu.Lock()
			duplicate := s.terminal != nil
			s.terminal = &terminal
			s.mu.Unlock()
			if duplicate {
				protocolErr = errors.New("duplicate source-streamer terminal status")
				cancel()
				break
			}
		}
		scanErr := scanner.Err()
		if scanErr != nil {
			cancel()
		}
		waitErr := cmd.Wait()
		s.mu.Lock()
		if protocolErr != nil {
			s.err = protocolErr
		} else if scanErr != nil {
			s.err = errors.New("source-streamer status exceeded protocol bounds")
		} else if s.terminal != nil && s.terminal.FailureCode == "frame_too_large" {
			s.err = ErrFrameTooLarge
		} else if s.terminal == nil || s.terminal.Reason != "completed" {
			s.err = errors.New("source-streamer did not report clean completion")
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
		_ = s.cmd.Process.Kill()
		s.cancel()
		<-s.done
	}
	return s.Wait()
}

func (s *Session) Terminal() (Stopped, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.terminal == nil {
		return Stopped{}, false
	}
	return *s.terminal, true
}
