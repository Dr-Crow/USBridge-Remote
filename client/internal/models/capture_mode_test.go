package models

import "testing"

func TestPreferredCaptureMode(t *testing.T) {
	mjpeg := func(w, h int, fps ...int) VideoCaptureMode {
		return VideoCaptureMode{Width: w, Height: h, FPS: fps, PixelFormat: "MJPG"}
	}
	yuyv := func(w, h int, fps ...int) VideoCaptureMode {
		return VideoCaptureMode{Width: w, Height: h, FPS: fps, PixelFormat: "YUYV"}
	}

	tests := []struct {
		name       string
		modes      []VideoCaptureMode
		w, h       int
		wantFormat string
		wantOK     bool
	}{
		{"MJPEG listed first -> YUYV",
			[]VideoCaptureMode{mjpeg(1920, 1080, 60, 50, 30), yuyv(1920, 1080, 60, 50, 30)}, 1920, 1080, "YUYV", true},
		{"YUYV listed first -> YUYV",
			[]VideoCaptureMode{yuyv(1920, 1080, 60), mjpeg(1920, 1080, 60)}, 1920, 1080, "YUYV", true},
		{"other sizes' YUYV doesn't count",
			[]VideoCaptureMode{yuyv(1280, 720, 60), mjpeg(1920, 1080, 60)}, 1920, 1080, "MJPG", true},
		{"only MJPEG at this size",
			[]VideoCaptureMode{mjpeg(1920, 1080, 60), yuyv(1280, 720, 60)}, 1920, 1080, "MJPG", true},
		{"only YUYV at this size",
			[]VideoCaptureMode{mjpeg(1280, 720, 60), yuyv(1920, 1080, 60)}, 1920, 1080, "YUYV", true},
		{"lowercase/alias format names",
			[]VideoCaptureMode{{Width: 1280, Height: 720, FPS: []int{60}, PixelFormat: "mjpeg"}, {Width: 1280, Height: 720, FPS: []int{60}, PixelFormat: "yuyv422"}}, 1280, 720, "yuyv422", true},
		{"size not offered",
			[]VideoCaptureMode{mjpeg(1920, 1080, 60)}, 3840, 2160, "", false},
		{"no format reported (single mode) still matches",
			[]VideoCaptureMode{{Width: 1920, Height: 1080, FPS: []int{60}}}, 1920, 1080, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := PreferredCaptureMode(tt.modes, tt.w, tt.h)
			if ok != tt.wantOK || got.PixelFormat != tt.wantFormat {
				t.Fatalf("got (%q, %v), want (%q, %v)", got.PixelFormat, ok, tt.wantFormat, tt.wantOK)
			}
		})
	}
}
