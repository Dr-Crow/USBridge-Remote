// Package logcap keeps log files from growing without end: each one is a
// two-file ring of at most 2 x MaxBytes (<log> and <log>.old).
//
// The files are written by more than this process's logger -- on Windows
// stdout/stderr are redirected into the log, and streamers write their
// output straight into theirs -- so a wrapping io.Writer can't do it.
// Instead, a file over MaxBytes is copied to <log>.old and emptied in
// place; every writer opened it for appending, so it carries on at the
// start of the file.
package logcap

import (
	"io"
	"os"
	"sync"
	"time"
)

// MaxBytes is the size past which a log is turned over.
const MaxBytes = 10 << 20

const checkInterval = time.Minute

var (
	mu      sync.Mutex
	paths   = map[string]bool{}
	started bool
)

// Watch caps path from now on (and right away).
func Watch(path string) {
	mu.Lock()
	paths[path] = true
	if !started {
		started = true
		go func() {
			for range time.Tick(checkInterval) {
				mu.Lock()
				list := make([]string, 0, len(paths))
				for p := range paths {
					list = append(list, p)
				}
				mu.Unlock()
				for _, p := range list {
					Cap(p)
				}
			}
		}()
	}
	mu.Unlock()
	Cap(path)
}

// Cap turns path over if it's past MaxBytes.
func Cap(path string) {
	st, err := os.Stat(path)
	if err != nil || st.Size() <= MaxBytes {
		return
	}
	if in, err := os.Open(path); err == nil {
		if out, err := os.Create(path + ".old"); err == nil {
			io.Copy(out, in)
			out.Close()
		}
		in.Close()
	}
	os.Truncate(path, 0)
}
