package sourcebroker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv("USBRIDGE_BROKER_TEST_HELPER"); mode != "" && len(os.Args) > 1 && os.Args[1] == "--capabilities" {
		caps := capabilities{"source-broker", Profile, 1, runtime.GOOS + "/" + runtime.GOARCH, "fixture", []string{"tls13-pinned", "usbip-transfer", "usbip-unlink"}, Role, false, true}
		if mode == "unsupported" {
			caps.SessionSupported = false
		}
		if mode == "bad-capability" {
			caps.Capabilities = append(caps.Capabilities, "os-attachment")
		}
		json.NewEncoder(os.Stdout).Encode(caps)
		os.Exit(0)
	}

	if mode := os.Getenv("USBRIDGE_BROKER_TEST_HELPER"); mode != "" && len(os.Args) > 1 && os.Args[1] == "--session-stdin" {
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadBytes('\n')
		var launch Launch
		if json.Unmarshal(line, &launch) != nil {
			os.Exit(9)
		}
		encoder := json.NewEncoder(os.Stdout)
		ready := Event{Event: "ready", ProtocolVersion: 1, Role: Role, Capacity: launch.Capacity, Device: &Device{BusID: launch.BusID, Bus: 1, Number: 2, VendorID: 0x1234, ProductID: 0x5678}}
		switch mode {
		case "attached":
			ready.OSAttached = true
		case "wrong-role":
			ready.Role = "os-importer"
		case "wrong-device":
			ready.Device.BusID = "2-3"
		case "hang":
			io.Copy(io.Discard, reader)
			os.Exit(0)
		}
		encoder.Encode(ready)
		if mode == "session-closed" {
			encoder.Encode(Event{Event: "error", Error: "session_closed"})
			os.Exit(2)
		}
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			var command Command
			if json.Unmarshal(scanner.Bytes(), &command) != nil {
				os.Exit(7)
			}
			switch command.Op {
			case "transfer":
				encoder.Encode(Event{Event: "complete", ID: command.ID, ActualLength: command.Length})
			case "cancel":
				encoder.Encode(Event{Event: "error", ID: command.ID, Error: "unknown_transfer"})
			case "close":
				encoder.Encode(Event{Event: "stopped"})
				os.Exit(0)
			}
		}
		encoder.Encode(Event{Event: "stopped"})
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func validLaunch() Launch {
	return Launch{1, "127.0.0.1:34001", filepath.Join(os.TempDir(), "client.pem"), filepath.Join(os.TempDir(), "client.key"), filepath.Join(os.TempDir(), "ca.pem"), "broker.local", strings.Repeat("a", 64), "1-2", true, 4}
}
func TestLaunchValidation(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Launch)
	}{
		{"consent", func(r *Launch) { r.Consent = false }}, {"version", func(r *Launch) { r.Version = 2 }},
		{"hostname", func(r *Launch) { r.Address = "localhost:1234" }}, {"public", func(r *Launch) { r.Address = "8.8.8.8:1234" }},
		{"wildcard", func(r *Launch) { r.Address = "0.0.0.0:1234" }}, {"port", func(r *Launch) { r.Address = "127.0.0.1:0" }},
		{"key-path", func(r *Launch) { r.KeyFile = "relative.key" }}, {"pin", func(r *Launch) { r.ServerPin = "not-a-pin" }},
		{"server", func(r *Launch) { r.ServerName = "" }}, {"bus", func(r *Launch) { r.BusID = "*" }}, {"capacity", func(r *Launch) { r.Capacity = 17 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := validLaunch()
			tt.change(&r)
			if r.Validate() == nil {
				t.Fatal("unsafe request accepted")
			}
		})
	}
	if err := validLaunch().Validate(); err != nil {
		t.Fatal(err)
	}
}
func TestCommandsAndEventsBounded(t *testing.T) {
	for _, command := range []Command{{Op: "transfer", ID: "a", Direction: 1, Endpoint: 1, Length: 64}, {Op: "cancel", ID: "a"}, {Op: "close"}} {
		if err := command.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, command := range []Command{{Op: "transfer", ID: "a", Direction: 2}, {Op: "transfer", ID: "a", Length: MaxTransfer + 1}, {Op: "transfer", ID: "a", Length: 1}, {Op: "transfer", ID: "a", Direction: 1, Data: "YQ=="}, {Op: "cancel", ID: "a", Data: "YQ=="}, {Op: "close", ID: "a"}, {Op: "inject", ID: "a"}} {
		if command.Validate() == nil {
			t.Fatal("unsafe transfer accepted")
		}
	}
	if (Event{Event: "complete", ID: "a", ActualLength: MaxTransfer + 1}).validateStatus() == nil {
		t.Fatal("oversized completion accepted")
	}
	if (Event{Event: "stopped", OSAttached: true}).validateStatus() == nil {
		t.Fatal("false OS-attachment accepted")
	}
	var launch Launch
	if decode([]byte(`{"version":1,"unknown":true}`), &launch) == nil {
		t.Fatal("unknown JSON accepted")
	}
}
func TestProcessLifecycle(t *testing.T) {
	t.Setenv("USBRIDGE_BROKER_TEST_HELPER", "valid")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		in := strings.NewReader("{\"op\":\"transfer\",\"id\":\"request1\",\"direction\":1,\"endpoint\":1,\"length\":64}\n{\"op\":\"close\"}\n")
		var out bytes.Buffer
		err := runBinary(ctx, binary, validLaunch(), in, &out)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		var events []Event
		scan := bufio.NewScanner(&out)
		for scan.Scan() {
			var event Event
			if json.Unmarshal(scan.Bytes(), &event) != nil {
				t.Fatal("bad output")
			}
			events = append(events, event)
		}
		if len(events) != 3 || events[0].Event != "ready" || events[1].ID != "request1" || events[2].Event != "stopped" {
			t.Fatalf("wrong events: %+v", events)
		}
	}
}
func TestProcessFailsClosed(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"attached", "wrong-role", "wrong-device", "hang"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("USBRIDGE_BROKER_TEST_HELPER", mode)
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			if err := runBinary(ctx, binary, validLaunch(), strings.NewReader(""), io.Discard); err == nil {
				t.Fatal("unsafe child accepted")
			}
		})
	}
}

func TestCapabilityProbe(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"valid", "unsupported", "bad-capability"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("USBRIDGE_BROKER_TEST_HELPER", mode)
			err := checkCapabilities(context.Background(), binary)
			if (err == nil) != (mode == "valid") {
				t.Fatal("unexpected probe result:", err)
			}
		})
	}
}

func TestIdleSessionLossPreservesTerminalEvent(t *testing.T) {
	t.Setenv("USBRIDGE_BROKER_TEST_HELPER", "session-closed")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		in, writer := io.Pipe()
		var out bytes.Buffer
		err := runBinary(ctx, binary, validLaunch(), in, &out)
		writer.Close()
		in.Close()
		cancel()
		if err == nil || !strings.Contains(err.Error(), "remote session closed") {
			t.Fatalf("idle loss result: %v", err)
		}
		var events []Event
		scanner := bufio.NewScanner(&out)
		for scanner.Scan() {
			var event Event
			if err := decode(scanner.Bytes(), &event); err != nil {
				t.Fatal(err)
			}
			events = append(events, event)
		}
		if len(events) != 2 || events[0].Event != "ready" || events[1].Event != "error" || events[1].Error != "session_closed" {
			t.Fatalf("lost typed terminal event: %+v", events)
		}
	}
}
