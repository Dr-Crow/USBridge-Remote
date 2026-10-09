//go:build linux && !android && cgo

package platform

/*
#cgo pkg-config: opus libpulse-simple
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <opus/opus.h>
#include <pulse/simple.h>
#include <pulse/error.h>

typedef struct {
    pa_simple *pa;
    OpusEncoder *enc;
    short pcm[960];
} mic_t;

static mic_t *mic_open(char *err, int errlen) {
    int e = 0;
    pa_sample_spec ss = { .format = PA_SAMPLE_S16LE, .rate = 48000, .channels = 1 };
    // Small fragments: a frame is read as soon as it is captured.
    pa_buffer_attr attr = { .maxlength = (uint32_t)-1, .tlength = (uint32_t)-1,
        .prebuf = (uint32_t)-1, .minreq = (uint32_t)-1, .fragsize = sizeof(((mic_t*)0)->pcm) };
    mic_t *m = calloc(1, sizeof(*m));
    if (!m) { snprintf(err, errlen, "out of memory"); return NULL; }
    m->pa = pa_simple_new(NULL, "USBridge", PA_STREAM_RECORD, NULL, "Microphone to the remote PC",
                          &ss, NULL, &attr, &e);
    if (!m->pa) { snprintf(err, errlen, "PulseAudio: %s", pa_strerror(e)); free(m); return NULL; }
    m->enc = opus_encoder_create(48000, 1, OPUS_APPLICATION_VOIP, &e);
    if (!m->enc) { snprintf(err, errlen, "opus_encoder_create: %d", e); pa_simple_free(m->pa); free(m); return NULL; }
    opus_encoder_ctl(m->enc, OPUS_SET_BITRATE(48000));
    opus_encoder_ctl(m->enc, OPUS_SET_SIGNAL(OPUS_SIGNAL_VOICE));
    return m;
}

// One 20ms frame: blocks until it is captured, then encodes it into out.
static int mic_read_encode(mic_t *m, unsigned char *out, int outlen) {
    int e = 0;
    if (pa_simple_read(m->pa, m->pcm, sizeof(m->pcm), &e) < 0) return -1;
    return opus_encode(m->enc, m->pcm, 960, out, outlen);
}

static void mic_close(mic_t *m) {
    pa_simple_free(m->pa);
    opus_encoder_destroy(m->enc);
    free(m);
}
*/
import "C"

import (
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"
)

type micCapture struct {
	stop atomic.Bool
	done chan struct{}
	once sync.Once
}

func (c *micCapture) Stop() {
	c.once.Do(func() {
		c.stop.Store(true)
		<-c.done
	})
}

// MicSupported reports whether StartMicCapture can work in this build.
func MicSupported() bool { return true }

// StartMicCapture records the default microphone (PulseAudio/PipeWire) and
// hands each 20ms frame to onFrame Opus-encoded (48kHz mono, at most 200
// bytes) with a running sequence number, from its own goroutine.
func StartMicCapture(onFrame func(seq uint16, opus []byte)) (UplinkCapture, error) {
	var errBuf [256]C.char
	m := C.mic_open(&errBuf[0], C.int(len(errBuf)))
	if m == nil {
		return nil, fmt.Errorf("%s", C.GoString(&errBuf[0]))
	}
	c := &micCapture{done: make(chan struct{})}
	go func() {
		defer close(c.done)
		defer C.mic_close(m)
		out := make([]byte, 200)
		var seq uint16
		for !c.stop.Load() {
			n := C.mic_read_encode(m, (*C.uchar)(unsafe.Pointer(&out[0])), C.int(len(out)))
			if n < 0 {
				return
			}
			seq++
			if n > 0 {
				onFrame(seq, append([]byte(nil), out[:n]...))
			}
		}
	}()
	return c, nil
}
