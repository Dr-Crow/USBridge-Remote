package logcap

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// A writer that keeps its append handle open (like the redirected
// stdout or a streamer) carries on at the start after a turn-over.
func TestCapTurnsOverUnderAnOpenAppender(t *testing.T) {
	p := filepath.Join(t.TempDir(), "app.log")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	chunk := bytes.Repeat([]byte("x"), 1<<20)
	for i := 0; i < 11; i++ {
		f.Write(chunk)
	}
	Cap(p)
	if st, _ := os.Stat(p); st.Size() != 0 {
		t.Fatalf("log not emptied: %d bytes", st.Size())
	}
	if st, _ := os.Stat(p + ".old"); st == nil || st.Size() != 11<<20 {
		t.Fatalf(".old not the previous log")
	}
	f.Write([]byte("after\n"))
	if data, _ := os.ReadFile(p); string(data) != "after\n" {
		t.Fatalf("writer didn't carry on at the start: %d bytes", len(data))
	}
	Cap(p) // under the limit: untouched
	if data, _ := os.ReadFile(p); string(data) != "after\n" {
		t.Fatal("small log touched")
	}
}
