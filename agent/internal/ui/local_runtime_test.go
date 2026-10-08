package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"strings"
	"testing"
	"usbridge_agent/internal/entitlement"
)

func TestLocalRuntimeBadgeRequiresConfirmedPreparation(t *testing.T) {
	for _, tc := range []struct {
		st   entitlement.Status
		want string
	}{
		{entitlement.Status{}, ""},
		{entitlement.Status{RustShineStaged: true}, ""},
		{entitlement.Status{LocalRuntimeConfigured: true}, "Pending"},
		{entitlement.Status{LocalRuntimeActive: true}, "Enabled"},
		{entitlement.Status{LocalRuntimeActive: true, LocalRuntimeStreamerPrepared: true}, "Patched"},
		{entitlement.Status{LocalRuntimeConfigured: true, LocalRuntimeUSBPrepared: true}, "Pending"},
		{entitlement.Status{LocalRuntimeStreamerPrepared: true}, ""},
	} {
		if got := localRuntimeBadge(tc.st); got != tc.want {
			t.Fatalf("%+v: got %q want %q", tc.st, got, tc.want)
		}
	}
}

func TestBlankProtocolBadgeHidesBackgroundAndCentersTitle(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	row := newProtocolPickRow(protocolOptions[1], false, nil, nil, nil)
	row.SetBadge("")
	row.Resize(fyne.NewSize(220, 56))
	r := row.CreateRenderer().(*protocolPickRowRenderer)
	defer r.Destroy()
	r.Layout(row.Size())
	if r.badgeBg.Visible() || r.sub.Visible() {
		t.Fatal("empty badge still visible")
	}
	if r.title.Position().Y != 20 {
		t.Fatalf("blank badge title not centered: %v", r.title.Position())
	}
	row.badge = "Patched"
	r.Refresh()
	if !r.badgeBg.Visible() || !r.sub.Visible() || r.sub.Text != "Patched" {
		t.Fatal("nonempty badge missing")
	}
	row.badge = "  "
	r.Refresh()
	if r.badgeBg.Visible() || r.sub.Visible() {
		t.Fatal("whitespace badge visible after refresh")
	}
}

func TestLocalRuntimeDiagnosticsShowRestartAndPreparationLimits(t *testing.T) {
	for _, st := range []entitlement.Status{
		{LocalRuntimeConfigured: true}, {LocalRuntimeActive: true},
	} {
		got := localRuntimeDiagnostics(st)
		if !strings.Contains(got, "restart engine") || !strings.Contains(got, "does not confirm") {
			t.Fatalf("incomplete diagnostic: %s", got)
		}
		if strings.Contains(got, "patched copy prepared") {
			t.Fatal("diagnostics fabricated preparation")
		}
	}
}
