package gui

import "testing"

func TestWithZipExt(t *testing.T) {
	if got := withZipExt(`C:\tmp\out`); got != `C:\tmp\out.zip` {
		t.Fatalf("got %q", got)
	}
	if got := withZipExt(`/tmp/out.ZIP`); got != `/tmp/out.ZIP` {
		t.Fatalf("already zip: %q", got)
	}
}
