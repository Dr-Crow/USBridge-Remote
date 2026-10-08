package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
	"usbridge_agent/internal/config"
	"usbridge_agent/internal/entitlement"
	"usbridge_agent/internal/netpolicy"
)

func TestRuntimeProvisioningRequiresSavedSelectionAndConsent(t *testing.T) {
	for _, tc := range []struct {
		name              string
		selected, consent bool
		want              int32
	}{
		{"unselected", false, false, 0}, {"selection-without-consent", true, false, 0},
		{"consent-without-selection", false, true, 0}, {"requested-missing", true, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(netpolicy.Environment, "")
			netpolicy.Configure(false)
			netpolicy.ConfigureRuntimeLocal(true)
			defer netpolicy.ConfigureRuntimeLocal(false)
			var calls atomic.Int32
			called := make(chan struct{}, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				select {
				case called <- struct{}{}:
				default:
				}
				if r.URL.Path != "/v1/desktop-license/refresh" {
					t.Errorf("unexpected setup endpoint: %s", r.URL.Path)
				}
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			defer server.Close()
			prior := entitlement.TestSetBackendBaseURL(server.URL)
			defer entitlement.TestSetBackendBaseURL(prior)
			a := newTestApp(t, "")
			a.cfg.StreamerConsent = tc.consent
			if tc.selected {
				a.cfg.PreferredBackend = "rustshine"
			}
			if err := config.Save(a.cfgPath, a.cfg); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go func() { defer close(done); a.requestedProvisioningWatchdog(ctx) }()
			if tc.want > 0 {
				select {
				case <-called:
				case <-time.After(2 * time.Second):
					t.Error("requested setup did not run")
				}
			} else {
				time.Sleep(30 * time.Millisecond)
			}
			cancel()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("provisioning did not stop")
			}
			if got := calls.Load(); got != tc.want {
				t.Fatalf("network calls=%d want%d", got, tc.want)
			}
		})
	}
}
