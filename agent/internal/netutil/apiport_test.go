package netutil

import "testing"

func TestWithAPIPort(t *testing.T) {
	for _, c := range []struct {
		host string
		port int
		want string
	}{
		{"192.168.1.5", 8080, "192.168.1.5"},
		{"192.168.1.5", 0, "192.168.1.5"},
		{"192.168.1.5", 9090, "192.168.1.5:9090"},
		{"kvm.device.usbridge.io", 9090, "kvm.device.usbridge.io:9090"},
		{"fd7a::1", 9090, "[fd7a::1]:9090"},
		{"192.168.1.5:7000", 9090, "192.168.1.5:7000"},
		{"", 9090, ""},
	} {
		if got := WithAPIPort(c.host, c.port); got != c.want {
			t.Errorf("WithAPIPort(%q, %d) = %q, want %q", c.host, c.port, got, c.want)
		}
	}
}
