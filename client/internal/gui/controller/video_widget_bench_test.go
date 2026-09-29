package controller

import (
	"testing"
	"time"
)

// stubBenchmarkStart replaces the real stream start for one test.
func stubBenchmarkStart(t *testing.T, fn func(vw *VideoWidget)) {
	t.Helper()
	prev := benchmarkStartStreamFn
	benchmarkStartStreamFn = fn
	t.Cleanup(func() { benchmarkStartStreamFn = prev })
}

// The benchmark must start the stream straight from the saved settings:
// going through the Start button's handler opened the video settings
// dialog mid-benchmark (covering and, on Windows, hiding the measured
// stream; closing it never started the stream).
func TestBenchmarkStartStream_NoSettingsDialog(t *testing.T) {
	vw := &VideoWidget{}
	vw.userStoppedVideo.Store(true)
	calls := 0
	stubBenchmarkStart(t, func(w *VideoWidget) {
		calls++
		if w != vw {
			t.Errorf("started a different widget")
		}
		// Simulate the stream coming up and presenting its first frame.
		go func() {
			time.Sleep(30 * time.Millisecond)
			w.videoTraceID.Add(1)
			w.videoTraceFirstFrame.Store(time.Now().UnixNano())
		}()
	})

	startup, err := vw.BenchmarkStartStream(2 * time.Second)
	if err != nil {
		t.Fatalf("BenchmarkStartStream: %v", err)
	}
	if calls != 1 {
		t.Fatalf("stream started %d times, want 1", calls)
	}
	if vw.startDialog != nil {
		t.Fatal("the video settings dialog was created")
	}
	if vw.userStoppedVideo.Load() {
		t.Fatal("userStoppedVideo still set, the reconcile would keep the stream off")
	}
	if startup < 20*time.Millisecond || startup > time.Second {
		t.Fatalf("startup = %v, want ~30ms (time to the first frame)", startup)
	}
}

// A first-frame stamp left over from an earlier stream must not count.
func TestBenchmarkStartStream_IgnoresStaleFirstFrame(t *testing.T) {
	vw := &VideoWidget{}
	vw.videoTraceID.Store(5)
	vw.videoTraceFirstFrame.Store(time.Now().Add(-time.Minute).UnixNano())
	stubBenchmarkStart(t, func(w *VideoWidget) {
		// New trace, but its stamp is still the old stream's.
		w.videoTraceID.Add(1)
	})
	if _, err := vw.BenchmarkStartStream(150 * time.Millisecond); err == nil {
		t.Fatal("stale first frame accepted as this stream's")
	}
}

func TestBenchmarkStartStream_TimesOutWithoutFrame(t *testing.T) {
	vw := &VideoWidget{}
	stubBenchmarkStart(t, func(*VideoWidget) {})
	start := time.Now()
	if _, err := vw.BenchmarkStartStream(100 * time.Millisecond); err == nil {
		t.Fatal("no error without a first frame")
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("returned after %v, want the 100ms timeout", d)
	}
}

func TestBenchmarkStartStream_StopsWhenClosing(t *testing.T) {
	vw := &VideoWidget{}
	stubBenchmarkStart(t, func(w *VideoWidget) { w.isClosing.Store(true) })
	if _, err := vw.BenchmarkStartStream(5 * time.Second); err == nil {
		t.Fatal("no error for a closing widget")
	}
}
