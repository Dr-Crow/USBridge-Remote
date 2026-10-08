package config

import "testing"

func TestLocalWebClientURL(t *testing.T) {
	for _, value := range []string{"https://example.com/", "https://8.8.8.8/", "https://user:pass@192.168.1.2/", "https://192.168.1.2/?token=x", "https://192.168.1.2/#token", "http://192.168.1.2/"} {
		if _, err := ValidateLocalWebClientURL(value); err == nil {
			t.Errorf("accepted %q", value)
		}
		if got := (Config{LocalWebClientURL: value}).WebClientURL(false); got != "" {
			t.Errorf("invalid URL fell back to %q", got)
		}
	}
	for _, value := range []string{"", "https://192.168.1.2/client/", "http://127.0.0.1:8080/", "https://[fd00::1]/"} {
		if _, err := ValidateLocalWebClientURL(value); err != nil {
			t.Errorf("rejected %q: %v", value, err)
		}
	}
	if got := (Config{}).WebClientURL(true); got != "" {
		t.Fatal("local mode exposed vendor link")
	}
	if got := (Config{}).WebClientURL(false); got != "https://web.usbridge.io" {
		t.Fatal("legacy online link changed")
	}
}
