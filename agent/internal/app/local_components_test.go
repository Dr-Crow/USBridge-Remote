package app

import (
	"context"
	"strings"
	"testing"
	"usbridge_agent/internal/config"
	"usbridge_agent/internal/netpolicy"
)

func TestLocalComponentResolutionPreservesSeparateConsent(t *testing.T) {
	t.Setenv(netpolicy.Environment, "1")
	c := config.Config{StateDir: t.TempDir(), LocalComponentMirror: "https://example.invalid/"}
	for _, name := range []string{"rustshine", "broker"} {
		if err := prepareLocalComponent(context.Background(), c, name); err == nil || !strings.Contains(err.Error(), "consent") {
			t.Fatalf("%s consent: %v", name, err)
		}
	}
}
