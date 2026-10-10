package sourcebroker

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"runtime"
	"time"
)

type capabilities struct {
	Component        string   `json:"component"`
	Profile          string   `json:"profile"`
	SchemaVersion    int      `json:"schema_version"`
	Platform         string   `json:"platform"`
	Version          string   `json:"version"`
	Capabilities     []string `json:"capabilities"`
	Role             string   `json:"role"`
	OSAttached       bool     `json:"os_attached"`
	SessionSupported bool     `json:"session_supported"`
}
type boundedBuffer struct{ bytes.Buffer }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > MaxMessage-b.Len() {
		return 0, errors.New("capability output too large")
	}
	return b.Buffer.Write(p)
}
func checkCapabilities(ctx context.Context, binary string) error {
	probe, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probe, binary, "--capabilities")
	var output boundedBuffer
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		return errors.New("source-broker capability probe failed")
	}
	var caps capabilities
	if decode(output.Bytes(), &caps) != nil {
		return errors.New("invalid source-broker capability manifest")
	}
	if caps.Component != "source-broker" || caps.Profile != Profile || caps.SchemaVersion != 1 || caps.Platform != runtime.GOOS+"/"+runtime.GOARCH || caps.Version == "" || caps.Role != Role || caps.OSAttached || !caps.SessionSupported {
		return errors.New("source-broker does not support this platform's v1 session protocol")
	}
	required := map[string]bool{"tls13-pinned": false, "usbip-transfer": false, "usbip-unlink": false}
	for _, c := range caps.Capabilities {
		if _, ok := required[c]; !ok || required[c] {
			return errors.New("unrecognized source-broker capability")
		}
		required[c] = true
	}
	for _, ok := range required {
		if !ok {
			return errors.New("missing source-broker capability")
		}
	}
	return nil
}
