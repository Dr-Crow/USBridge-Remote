package streamhost

import (
	"net"
	"testing"
	"time"
)

func TestWaitPortsFreeReturnsOnceTheOldBackendLetsGo(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	pc, err := net.ListenPacket("udp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	tcpPort := ln.Addr().(*net.TCPAddr).Port
	udpPort := pc.LocalAddr().(*net.UDPAddr).Port

	// Still held: must wait out the timeout and say so.
	if WaitPortsFree([]int{tcpPort}, []int{udpPort}, 300*time.Millisecond) {
		t.Fatal("reported free while both ports are held")
	}

	// Released a moment later: must return soon after, not at the timeout.
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = ln.Close()
		_ = pc.Close()
	}()
	start := time.Now()
	if !WaitPortsFree([]int{tcpPort}, []int{udpPort}, 10*time.Second) {
		t.Fatal("ports never reported free")
	}
	if waited := time.Since(start); waited > 2*time.Second {
		t.Fatalf("took %s to notice the release", waited)
	}
}
