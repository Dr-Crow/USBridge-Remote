package api

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestUSBBridgeServerJoinedBeforePreamble(t *testing.T) {
	server := new(Server)
	b, err := server.StartUSBPassBridge(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c, err := net.Dial("tcp4", b.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err = c.Write([]byte("API")); err != nil {
		t.Fatal(err)
	}
	closeBridge(t, b)
	c.SetReadDeadline(time.Now().Add(time.Second))
	var one [1]byte
	if _, err = c.Read(one[:]); err == nil {
		t.Fatal("preamble connection survived close")
	}
}

func TestUSBBridgeServerRejectsOversizedPreamble(t *testing.T) {
	b, err := new(Server).StartUSBPassBridge(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer closeBridge(t, b)
	c, err := net.Dial("tcp4", b.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err = c.Write([]byte(strings.Repeat("x", usbPassBridgePreambleMaxLen))); err != nil {
		t.Fatal(err)
	}
	c.SetReadDeadline(time.Now().Add(time.Second))
	var one [1]byte
	if _, err = c.Read(one[:]); err != io.EOF {
		t.Fatal("oversized unterminated preamble retained connection")
	}
}

func TestUSBBridgeServerAPIRelayRetires(t *testing.T) {
	upstream, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	_, portText, _ := net.SplitHostPort(upstream.Addr().String())
	port, _ := strconv.Atoi(portText)
	s := new(Server)
	s.SetSelfHTTPPort(port)
	b, err := s.StartUSBPassBridge(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer closeBridge(t, b)
	backendDone := make(chan error, 1)
	go func() {
		c, err := upstream.Accept()
		if err != nil {
			backendDone <- err
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(time.Second))
		buf := make([]byte, 5)
		if _, err = io.ReadFull(c, buf); err != nil {
			backendDone <- err
			return
		}
		if string(buf) != "hello" {
			backendDone <- fmt.Errorf("buffered bytes changed")
			return
		}
		_, err = c.Write([]byte("world"))
		backendDone <- err
	}()
	client, err := net.Dial("tcp4", b.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.SetDeadline(time.Now().Add(time.Second))
	if _, err = client.Write([]byte("API\nhello")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 5)
	if _, err = io.ReadFull(client, buf); err != nil || string(buf) != "world" {
		t.Fatal("relay response changed")
	}
	if err = <-backendDone; err != nil {
		t.Fatal(err)
	}
	closeBridge(t, b)
}

func TestUSBBridgeServerStatusDoesNotInferBrokerAvailability(t *testing.T) {
	s := new(Server)
	if s.USBPassBridgeState() != "not_started" {
		t.Fatal("constructor advertised bridge")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.StartUSBPassBridge(ctx); err == nil || s.USBPassBridgeState() != "unavailable" {
		t.Fatal("failed start advertised bridge")
	}
	b, err := s.StartUSBPassBridge(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.USBPassBridgeState() != "ready" {
		t.Fatal("live listener status missing")
	}
	if _, err = s.StartUSBPassBridge(context.Background()); err == nil {
		t.Fatal("duplicate listener started")
	}
	if s.USBPassBridgeState() != "ready" {
		t.Fatal("duplicate start hid original owner")
	}
	closeBridge(t, b)
	if s.USBPassBridgeState() != "closed" {
		t.Fatal("retired bridge still advertised")
	}
}
