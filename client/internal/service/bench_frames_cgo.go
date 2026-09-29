//go:build cgo && (linux || darwin || ios || windows || android)

package service

/*
#include <stdint.h>

typedef struct {
    int32_t  frame_number;
    int32_t  frame_type;
    uint32_t size_bytes;
    uint16_t host_latency_tenths_ms;
    uint16_t _pad;
    uint64_t receive_us;
    uint64_t enqueue_us;
    uint64_t submit_us;
    uint64_t presentation_us;
} bench_frame_t;

// Bodies in bench_frames.c (declared again here rather than including
// bench_frames.h, which pulls in Limelight.h and its include path).
extern void     bench_frames_enable(int on);
extern int      bench_frames_drain(bench_frame_t *out, int max);
extern uint32_t bench_frames_dropped(void);
extern uint64_t bench_now_us(void);
*/
import "C"

import "unsafe"

func init() {
	benchFramesEnableFn = func(on bool) {
		v := C.int(0)
		if on {
			v = 1
		}
		C.bench_frames_enable(v)
	}
	benchFramesDrainFn = benchDrainCgo
	benchFramesDroppedFn = func() uint32 { return uint32(C.bench_frames_dropped()) }
	benchNowUsFn = func() uint64 { return uint64(C.bench_now_us()) }
}

const benchDrainBatch = 1024

func benchDrainCgo(dst []BenchFrame) []BenchFrame {
	var buf [benchDrainBatch]C.bench_frame_t
	for {
		n := int(C.bench_frames_drain((*C.bench_frame_t)(unsafe.Pointer(&buf[0])), benchDrainBatch))
		for i := 0; i < n; i++ {
			f := &buf[i]
			dst = append(dst, BenchFrame{
				Number:        int32(f.frame_number),
				IDR:           f.frame_type == 1, // FRAME_TYPE_IDR
				Size:          uint32(f.size_bytes),
				HostLatencyMs: float64(f.host_latency_tenths_ms) / 10,
				ReceiveUs:     uint64(f.receive_us),
				EnqueueUs:     uint64(f.enqueue_us),
				SubmitUs:      uint64(f.submit_us),
				PtsUs:         uint64(f.presentation_us),
			})
		}
		if n < benchDrainBatch {
			return dst
		}
	}
}
