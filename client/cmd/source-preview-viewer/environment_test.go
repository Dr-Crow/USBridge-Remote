package main

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestClearPreviewEnvironmentRemovesEveryDiagnostic(t *testing.T) {
	// Restore preexisting prefixed variables too, not just this test's additions.
	original := os.Environ()
	t.Cleanup(func() {
		for _, entry := range original {
			name, value, _ := strings.Cut(entry, "=")
			if strings.HasPrefix(strings.ToUpper(name), "USBRIDGE_") {
				_ = os.Setenv(name, value)
			}
		}
	})
	keys := []string{"USBRIDGE_FRAME_DUMP_DIR", "USBRIDGE_FRAME_DUMP_EVERY_N", "USBRIDGE_PYROWAVE_DUMP", "USBRIDGE_SKIP_DECODE", "USBRIDGE_HWDEC", "USBRIDGE_LOG_FRAME_JITTER", "USBRIDGE_LOG_RTP_STATS", "USBRIDGE_PLAYOUT_BUFFER", "USBRIDGE_FUTURE_UNKNOWN_SWITCH", "usbridge_case_variant"}
	for _, key := range keys {
		t.Setenv(key, "private-value-not-for-logging")
	}
	t.Setenv("USBRIDGE_EMPTY_SWITCH", "")
	preserved := map[string]string{"DISPLAY": ":91", "XAUTHORITY": "/tmp/preview-test-Xauthority", "USBRIDGE": "not-the-prefix", "OTHER_USBRIDGE_DUMP": "preserve"}
	for key, value := range preserved {
		t.Setenv(key, value)
	}
	if err := clearPreviewEnvironment(); err != nil {
		t.Fatal(err)
	}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(strings.ToUpper(name), "USBRIDGE_") {
			t.Fatal("prefixed variable survived")
		}
	}
	for key, want := range preserved {
		if got := os.Getenv(key); got != want {
			t.Fatalf("non-prefixed runtime variable %s changed", key)
		}
	}
	if err := clearPreviewEnvironment(); err != nil {
		t.Fatal("scrub is not idempotent")
	}
}
func TestClearPreviewEnvironmentUsesNamesOnly(t *testing.T) {
	var removed []string
	err := clearPreviewEnvironmentWith([]string{"DISPLAY=:91", "USBRIDGE_FRAME_DUMP_DIR=/private/a=b", "USBRIDGE_FUTURE=secret", "USBRIDGE=retain", "USBRIDGE_="}, func(name string) error { removed = append(removed, name); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(removed, []string{"USBRIDGE_FRAME_DUMP_DIR", "USBRIDGE_FUTURE", "USBRIDGE_"}) {
		t.Fatal("incorrect prefix/name filtering")
	}
}
func TestClearPreviewEnvironmentFailsClosedWithoutLeaking(t *testing.T) {
	err := clearPreviewEnvironmentWith([]string{"USBRIDGE_FRAME_DUMP_DIR=/private-sensitive-path"}, func(string) error { return errors.New("private-sensitive-path") })
	if err == nil {
		t.Fatal("unset failure ignored")
	}
	if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "USBRIDGE") {
		t.Fatal("scrub error exposes environment data")
	}
}

func TestOnlyLiteralLocalDisplay(t *testing.T) {
	for _, value := range []string{":0", ":96", ":97.0", ":123.2", ":0001.02"} {
		if !validLocalDisplay(value) {
			t.Errorf("rejected literal local display %q", value)
		}
	}
	for _, value := range []string{"", ":", ":.0", ":1.", ":1.0.0", "localhost:0", "127.0.0.1:0", "remote.example:0", "tcp/localhost:0", "unix/:0", "unix:0", ":-1", ":+1", ":1/../2", ":１", ":1\n", ":1  ", strings.Repeat("9", 40)} {
		if validLocalDisplay(value) {
			t.Errorf("accepted non-local display %q", value)
		}
	}
}
