package app

import (
	"context"
	"runtime"
	"testing"
	"time"
)

func TestAWDLDisableDuringStreamingSupported(t *testing.T) {
	a := newTestApp(t, "")
	got := a.AWDLDisableDuringStreamingSupported()
	want := runtime.GOOS == "darwin"
	if got != want {
		t.Errorf("AWDLDisableDuringStreamingSupported() = %v, want %v (runtime.GOOS=%s)", got, want, runtime.GOOS)
	}
}

func TestAWDLDisableDuringStreamingEnabled_DefaultsFalse(t *testing.T) {
	a := newTestApp(t, "")
	if a.AWDLDisableDuringStreamingEnabled() {
		t.Error("AWDLDisableDuringStreamingEnabled() on a fresh config = true, want false (opt-in)")
	}
}

// TestSetAWDLDisableDuringStreaming_Off exercises only the enabled=false
// path: SetAWDLDisableDuringStreaming(true) would call
// netutil.EnsureAWDLSudoRule, which pops a real admin-password prompt on
// darwin -- not something a test may trigger. The false path persists the
// setting and best-effort restores awdl0 (netutil.SetAWDLDown(false),
// whose failure -- e.g. no sudo rule ever installed -- is only logged, per
// SetAWDLDisableDuringStreaming's own doc comment), so it's safe to run
// unconditionally.
func TestSetAWDLDisableDuringStreaming_Off(t *testing.T) {
	a := newTestApp(t, "")
	a.cfg.DisableAWDLDuringStreaming = true

	if err := a.SetAWDLDisableDuringStreaming(false); err != nil {
		t.Fatalf("SetAWDLDisableDuringStreaming(false) = %v, want nil", err)
	}
	if a.AWDLDisableDuringStreamingEnabled() {
		t.Error("AWDLDisableDuringStreamingEnabled() after SetAWDLDisableDuringStreaming(false) = true, want false")
	}
}

func TestAWDLSudoersPreview_NonDarwinEmpty(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("darwin has a real preview -- see netutil's own awdl_darwin_test.go")
	}
	a := newTestApp(t, "")
	if got := a.AWDLSudoersPreview(); got != "" {
		t.Errorf("AWDLSudoersPreview() on %s = %q, want empty", runtime.GOOS, got)
	}
}

// TestAwdlWatchdog_NoopWhenUnsupported confirms awdlWatchdog returns
// immediately (rather than looping forever) on platforms where
// AWDLDisableDuringStreamingSupported is false, so Run() can always start
// it unconditionally.
func TestAwdlWatchdog_NoopWhenUnsupported(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("darwin's awdlWatchdog doesn't return immediately -- covered by the poll-loop shape, not this test")
	}
	a := newTestApp(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		a.awdlWatchdog(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("awdlWatchdog did not return promptly on a platform where AWDL is unsupported")
	}
}

// TestAwdlWatchdog_StopsOnContextCancel confirms the poll loop itself (the
// branch actually reached on darwin) exits once its context is cancelled,
// without ever needing AWDLDisableDuringStreamingEnabled to be true --
// since enabling it for real would hit the same admin-password-prompt
// concern as the Set test above.
func TestAwdlWatchdog_StopsOnContextCancel(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("only darwin's awdlWatchdog enters the poll loop this test exercises")
	}
	a := newTestApp(t, "")
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		a.awdlWatchdog(ctx)
		close(done)
	}()

	// Let at least one poll tick pass (with AWDL disabled in config, so the
	// tick's only effect is a no-op restore() call, never touching sudo)
	// before cancelling.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(awdlPollInterval + time.Second):
		t.Fatal("awdlWatchdog did not stop after its context was cancelled")
	}
}
