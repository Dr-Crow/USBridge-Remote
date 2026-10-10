package api

import "testing"

func TestUSBClientHostPort(t *testing.T) {
	for _, c := range []struct{ host, want string }{
		{"192.168.1.5", "http://192.168.1.5:8080"},
		{"192.168.1.5:9090", "http://192.168.1.5:9090"},
		{" kvm.local:9443 ", "http://kvm.local:9443"},
		{"[fe80::1]:9090", "http://[fe80::1]:9090"},
		{"fe80::1", "http://[fe80::1]:8080"},
	} {
		if got := NewUSBClient(c.host, 8080, 5).baseURL; got != c.want {
			t.Errorf("%q: %s, want %s", c.host, got, c.want)
		}
	}
}
