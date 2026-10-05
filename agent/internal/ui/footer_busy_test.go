package ui

import (
	"testing"

	"usbridge_agent/internal/entitlement"
)

func TestFooterDownloadHint(t *testing.T) {
	const base = "Changing protocol..."
	cases := []struct {
		st   entitlement.Status
		want string
	}{
		{entitlement.Status{}, base},
		{entitlement.Status{DownloadInProgress: true, DownloadName: "Sunshine", Progress: -1}, "Downloading Sunshine..."},
		{entitlement.Status{DownloadInProgress: true, DownloadName: "Punktfunk", Progress: 0.424}, "Downloading Punktfunk... 42%"},
		{entitlement.Status{DownloadInProgress: true, Progress: 1.2}, "Downloading USBridge Streamer... 100%"},
	}
	for _, c := range cases {
		if got := footerDownloadHint(c.st, base); got != c.want {
			t.Errorf("footerDownloadHint(%+v) = %q, want %q", c.st, got, c.want)
		}
	}
}
