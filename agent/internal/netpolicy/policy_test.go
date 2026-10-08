package netpolicy

import (
	"errors"
	"testing"
)

func TestStrictDefaultAndStartupConfiguration(t *testing.T) {
	t.Setenv(Environment, "")
	original := configured.Load()
	t.Cleanup(func() { configured.Store(original) })
	Configure(false)
	if Strict() || RequireOnline("account") != nil {
		t.Fatal("default unexpectedly strict")
	}
	Configure(true)
	if !errors.Is(RequireOnline("account"), ErrStrictLAN) {
		t.Fatal("cloud request permitted")
	}
	Configure(false)
	t.Setenv(Environment, "1")
	if !Strict() {
		t.Fatal("explicit launch override ignored")
	}
}

func TestLocalURLRejectsPublicDNSCredentialsAndPlainLAN(t *testing.T) {
	for _, raw := range []string{
		"https://usbridge.io/", "https://example.local/", "https://8.8.8.8/",
		"https://user:password@192.168.1.1/", "http://192.168.1.1/",
		"file:///tmp/components", "https://192.168.1.1/#fragment",
	} {
		if _, err := LocalURL(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	for _, raw := range []string{
		"https://192.168.1.1:8443/components/", "https://10.1.2.3/",
		"https://[fd00::1]/", "http://127.0.0.1:1234/", "https://[::1]/",
	} {
		if _, err := LocalURL(raw); err != nil {
			t.Fatalf("%q: %v", raw, err)
		}
	}
}
