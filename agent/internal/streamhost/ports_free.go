package streamhost

import (
	"net"
	"strconv"
	"time"
)

// WaitPortsFree waits until nothing holds the given ports any more -- no
// TCP listener answers on 127.0.0.1 and each UDP port can be bound -- or
// timeout passes, and reports which. Called after stopping a backend and
// before starting the next one: a killed process can keep its sockets for a
// moment while Windows tears it down, and a new backend started meanwhile
// fails to bind the shared GameStream ports or ends up fighting the old
// one for them. Waiting on the ports themselves replaces a fixed sleep
// that was either too short or needlessly long.
func WaitPortsFree(tcp, udp []int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if portsFree(tcp, udp) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func portsFree(tcp, udp []int) bool {
	for _, p := range tcp {
		if p <= 0 {
			continue
		}
		conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(p)), 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return false
		}
	}
	for _, p := range udp {
		if p <= 0 {
			continue
		}
		pc, err := net.ListenPacket("udp", ":"+strconv.Itoa(p))
		if err != nil {
			return false
		}
		_ = pc.Close()
	}
	return true
}
