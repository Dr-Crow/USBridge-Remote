// SPDX-License-Identifier: GPL-3.0-only
// This test-only executable substitutes owned, pre-generated synthetic media
// for the frozen source's encoder. It cannot capture a desktop: it imports no
// capture, codec, network, syscall, plugin or process-launch packages.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Required build-time pins. Empty/default builds fail closed before output.
var videoBlueSHA256, videoOrangeSHA256, audioSilenceSHA256 string

const (
	blueName       = "fixture-blue.h264"
	orangeName     = "fixture-orange.h264"
	silenceName    = "fixture-silence.ogg"
	frameCount     = 900
	audioPageCount = 6000
	pcmSamples     = 1439760 // 29.995 s, plus the codec's final padding packet.
	pcmBytes       = pcmSamples * 2 * 2
	maxRuntime     = 30 * time.Second
)

// Frozen public source 2e07af3484369bc68bc8091d5a04969867eff0f9,
// source/media/windows_capture.go windowsGDIArgs(..., live=true).
// These strings are a recognition contract only. They are NEVER executed.
var videoArgs = []string{"-hide_banner", "-nostdin", "-v", "error", "-f", "gdigrab", "-draw_mouse", "0", "-offset_x", "0", "-offset_y", "0", "-video_size", "128x72", "-framerate", "30", "-i", "desktop", "-an", "-pix_fmt", "yuv420p", "-c:v", "libx264", "-qp", "0", "-preset", "ultrafast", "-tune", "zerolatency", "-x264-params", "aud=1:repeat-headers=1:keyint=1:min-keyint=1:scenecut=0", "-threads", "1", "-f", "h264", "pipe:1"}

// Frozen public source source/audio/pcm.go EncodePCMStream.
var audioArgs = []string{"-hide_banner", "-nostdin", "-v", "error", "-f", "s16le", "-ar", "48000", "-ac", "2", "-i", "pipe:0", "-vn", "-c:a", "libopus", "-b:a", "128k", "-application", "lowdelay", "-frame_duration", "5", "-vbr", "off", "-mapping_family", "0", "-f", "ogg", "-page_duration", "5000", "-flush_packets", "1", "pipe:1"}

type role uint8

const (
	invalidRole role = iota
	videoRole
	audioRole
)

func selectRole(args []string) role {
	if slices.Equal(args, videoArgs) {
		return videoRole
	}
	if slices.Equal(args, audioArgs) {
		return audioRole
	}
	return invalidRole
}

func main() {
	r, a, err := prepare(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "synthetic fixture failed")
		os.Exit(2)
	}
	// The 30-second media lease starts only after bounded asset verification.
	// It also bounds blocked handles. No descendants can outlive this process.
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), maxRuntime)
	defer cancel()
	go func() { done <- emit(ctx, r, a, os.Stdin, os.Stdout, wallClock{}) }()
	select {
	case err := <-done:
		if err != nil {
			fmt.Fprintln(os.Stderr, "synthetic fixture failed")
			os.Exit(2)
		}
	case <-ctx.Done():
		// Expiring a finite test lease is not a codec error and needs no media log.
	}
}

func prepare(args []string) (role, assets, error) {
	r := selectRole(args)
	if r == invalidRole {
		return r, assets{}, errors.New("unsupported fixture role")
	}
	exe, err := os.Executable()
	if err != nil {
		return r, assets{}, errors.New("fixture path unavailable")
	}
	a, err := loadAssets(filepath.Dir(exe), assetPins{videoBlueSHA256, videoOrangeSHA256, audioSilenceSHA256})
	return r, a, err
}

type pacingClock interface {
	Now() time.Time
	Wait(context.Context, time.Time) error
}
type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now() }
func (wallClock) Wait(ctx context.Context, at time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	timer := time.NewTimer(time.Until(at))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func emit(ctx context.Context, r role, a assets, input io.Reader, output io.Writer, clock pacingClock) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	start := clock.Now()
	if r == videoRole {
		for i := 0; i < frameCount; i++ {
			if err := clock.Wait(ctx, start.Add(time.Duration(i)*time.Second/30)); err != nil {
				return err
			}
			frame := a.blue
			if (i/30)%2 != 0 {
				frame = a.orange
			}
			if err := writeAll(output, frame); err != nil {
				return err
			}
		}
		return nil
	}
	if r != audioRole {
		return errors.New("unsupported fixture role")
	}
	// Each header is an independent Ogg page. Headers do not wait for PCM input.
	for _, page := range a.pages[:2] {
		if err := writeAll(output, page); err != nil {
			return err
		}
	}
	var pcm [960]byte // Exactly 5 ms of 48 kHz, s16le, stereo PCM.
	remaining := pcmBytes
	for i, page := range a.pages[2:] {
		if err := clock.Wait(ctx, start.Add(time.Duration(i)*5*time.Millisecond)); err != nil {
			return err
		}
		n := min(remaining, len(pcm))
		if _, err := io.ReadFull(input, pcm[:n]); err != nil {
			return errors.New("fixture PCM ended")
		}
		remaining -= n
		// The frozen source supplies silence. Data is discarded, never encoded,
		// echoed, saved, or interpreted as a path; reads remain strictly bounded.
		if err := writeAll(output, page); err != nil {
			return err
		}
	}
	if remaining != 0 {
		return errors.New("fixture PCM count invalid")
	}
	return nil
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) != 0 {
		n, err := w.Write(data)
		if err != nil {
			return errors.New("fixture output closed")
		}
		if n <= 0 || n > len(data) {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
