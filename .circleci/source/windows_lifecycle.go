// Native Windows actual agent/source acceptance. No RTSP PLAY or capture occurs.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 4 || runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		return errors.New("native Windows acceptance requires AGENT COMPONENT_DIRECTORY OUTPUT")
	}
	agent, components, output := os.Args[1], os.Args[2], os.Args[3]
	manifest, err := os.ReadFile(filepath.Join(components, "manifest.json"))
	if err != nil {
		return err
	}
	pin := sha256.Sum256(manifest)
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	// This executable is only a regular-file stand-in for FFmpeg. Because no
	// PLAY is sent, it must never be started as an encoder or capture source.
	for _, mode := range []string{"eof", "teardown", "eof", "teardown", "reject-input", "reject-x11"} {
		if err := session(agent, components, hex.EncodeToString(pin[:]), executable, mode); err != nil {
			return fmt.Errorf("%s: %w", mode, err)
		}
	}
	data, err := os.ReadFile(agent)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(data)
	result := map[string]any{"passed": true, "native_platform": runtime.GOOS + "/" + runtime.GOARCH, "actual_agent_supervisor": true, "real_source_streamer": true, "manifest_sha256": hex.EncodeToString(pin[:]), "agent_sha256": hex.EncodeToString(hash[:]), "eof_restart_joined": true, "encrypted_options_teardown": true, "unsupported_input_rejected": true, "x11_display_rejected": true, "system_only_path": true, "capture_tested": false, "media_tested": false, "hardware_tested": false}
	raw, _ := json.MarshalIndent(result, "", "  ")
	if err := os.WriteFile(output, append(raw, '\n'), 0600); err != nil {
		return err
	}
	fmt.Println(string(raw))
	return nil
}

func session(agent, components, pin, ffmpeg, mode string) error {
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	idBytes := make([]byte, 8)
	if _, err := rand.Read(idBytes); err != nil {
		return err
	}
	id := "native-" + hex.EncodeToString(idBytes)
	config := map[string]any{"schema_version": 1, "owner": "native-ci-operator", "session_id": id, "key_b64": base64.StdEncoding.EncodeToString(key), "key_id": 1000, "peer_ip": "127.0.0.1", "video_port": 0, "audio_port": 0, "display": "desktop", "capture_consent": true, "ffmpeg": ffmpeg, "width": 128, "height": 72, "fps": 30, "pixel_format": "yuv420p", "packet_size": 1024, "audio_mode": "silence", "max_seconds": 15}
	if mode == "reject-input" {
		config["input_consent"] = true
	}
	if mode == "reject-x11" {
		config["display"] = ":99"
	}
	state, err := os.MkdirTemp("", "agent-source-native-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(state)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, agent, "--source-streamer-mode", "--source-component-directory", components, "--source-manifest-sha256", pin, "--source-state-dir", state)
	cmd.WaitDelay = 2 * time.Second
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(name, "PATH") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	system := os.Getenv("SystemRoot")
	if !filepath.IsAbs(system) {
		return errors.New("invalid SystemRoot")
	}
	cmd.Env = append(cmd.Env, "PATH="+filepath.Join(system, "System32")+";"+system)
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return errors.New("agent executable startup failed")
	}
	waited := false
	defer func() {
		if !waited {
			in.Close()
			cmd.Process.Kill()
			cmd.Wait()
		}
	}()
	raw, _ := json.Marshal(config)
	if _, err := in.Write(append(raw, '\n')); err != nil {
		return errors.New("send private native launch")
	}
	if strings.HasPrefix(mode, "reject-") {
		in.Close()
		remaining, _ := io.ReadAll(io.LimitReader(out, 65537))
		err = cmd.Wait()
		waited = true
		if err == nil || len(remaining) != 0 {
			return errors.New("unsupported native request was accepted")
		}
		if bytes.Contains(stderr.Bytes(), []byte(config["key_b64"].(string))) {
			return errors.New("launch key leaked")
		}
		return nil
	}
	reader := bufio.NewReaderSize(out, 65537)
	line, err := reader.ReadSlice('\n')
	if err != nil || len(line) > 65536 {
		return errors.New("bounded native readiness missing")
	}
	var ready struct {
		Event        string   `json:"event"`
		Schema       int      `json:"schema_version"`
		Session      string   `json:"session_id"`
		RTSP         string   `json:"rtsp_address"`
		Control      string   `json:"control_address"`
		Capabilities []string `json:"capabilities"`
	}
	if json.Unmarshal(line, &ready) != nil || ready.Event != "ready" || ready.Schema != 1 || ready.Session != id {
		return errors.New("native readiness identity mismatch")
	}
	want := map[string]bool{"rtsp-encrypted": true, "video-windows-gdi-h264": true, "audio-silence": true, "control-enet": true}
	for _, capability := range ready.Capabilities {
		if !want[capability] {
			return errors.New("unexpected native capability")
		}
		delete(want, capability)
	}
	if len(want) != 0 {
		return errors.New("native capability missing")
	}
	for _, address := range []string{ready.RTSP, ready.Control} {
		host, _, err := net.SplitHostPort(address)
		if err != nil || host != "127.0.0.1" {
			return errors.New("native listener escaped loopback")
		}
	}
	if mode == "teardown" {
		for i, method := range []string{"OPTIONS", "TEARDOWN"} {
			if err := rtsp(ready.RTSP, key, id, method, uint32(i+1)); err != nil {
				return err
			}
		}
	}
	in.Close()
	terminal, err := reader.ReadSlice('\n')
	if err != nil || len(terminal) > 65536 {
		return errors.New("native terminal event missing")
	}
	var stopped struct {
		Event   string `json:"event"`
		Reason  string `json:"reason"`
		Session string `json:"session_id"`
		Stats   struct {
			VideoFrames  uint64 `json:"video_frames"`
			AudioPackets uint64 `json:"audio_packets"`
		} `json:"stats"`
	}
	if json.Unmarshal(terminal, &stopped) != nil || stopped.Event != "stopped" || stopped.Reason != "completed" || stopped.Session != id || stopped.Stats.VideoFrames != 0 || stopped.Stats.AudioPackets != 0 {
		return errors.New("native no-capture terminal mismatch")
	}
	remaining, _ := io.ReadAll(io.LimitReader(reader, 65537))
	err = cmd.Wait()
	waited = true
	if err != nil || len(remaining) != 0 {
		return errors.New("native child did not exit cleanly")
	}
	if bytes.Contains(stderr.Bytes(), []byte(config["key_b64"].(string))) {
		return errors.New("launch key leaked")
	}
	return nil
}

func rtsp(address string, key []byte, id, method string, sequence uint32) error {
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, 12)
	binary.LittleEndian.PutUint32(nonce, sequence)
	copy(nonce[10:], "CR")
	plain := []byte(fmt.Sprintf("%s * RTSP/1.0\r\nCSeq: %d\r\nSession: %s\r\n\r\n", method, sequence, id))
	sealed := gcm.Seal(nil, nonce, plain, nil)
	packet := make([]byte, 8)
	binary.BigEndian.PutUint32(packet, uint32(len(plain))|0x80000000)
	binary.BigEndian.PutUint32(packet[4:], sequence)
	packet = append(packet, sealed[len(sealed)-16:]...)
	packet = append(packet, sealed[:len(sealed)-16]...)
	conn, err := net.DialTimeout("tcp4", address, 3*time.Second)
	if err != nil {
		return errors.New("native RTSP connection failed")
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write(packet); err != nil {
		return errors.New("native RTSP write failed")
	}
	response, err := io.ReadAll(io.LimitReader(conn, 65537))
	if err != nil || len(response) < 24 || len(response) > 65536 {
		return errors.New("native encrypted RTSP reply missing")
	}
	if binary.BigEndian.Uint32(response) != uint32(len(response)-24)|0x80000000 {
		return errors.New("native RTSP reply length mismatch")
	}
	binary.LittleEndian.PutUint32(nonce, binary.BigEndian.Uint32(response[4:8]))
	copy(nonce[10:], "HR")
	sealed = append(append([]byte{}, response[24:]...), response[8:24]...)
	decoded, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil || !bytes.HasPrefix(decoded, []byte("RTSP/1.0 200 ")) {
		return errors.New("native encrypted RTSP rejected")
	}
	return nil
}
