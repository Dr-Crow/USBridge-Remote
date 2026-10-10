//go:build (linux || windows) && !android && cgo

// source-preview-viewer is a one-use child of the locally authorized preview
// manager. It receives credentials only on a private stdin pipe. No general
// connect flag, pairing flow, saved configuration or automatic reconnect exists.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"image"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"usbridge-client/internal/service"
	"usbridge-client/internal/sourcepreview"
)

func main() {
	if clearPreviewEnvironment() != nil {
		os.Exit(2)
	}
	if len(os.Args) != 2 || os.Args[1] != "--source-preview-stdin" {
		os.Exit(2)
	}
	info, err := os.Stdin.Stat()
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		os.Exit(2)
	}
	out, err := privateEventWriter()
	if err != nil {
		os.Exit(2)
	}
	defer out.Close()
	reader := bufio.NewReaderSize(os.Stdin, sourcepreview.MaxDescriptorBytes+1)
	line, err := reader.ReadSlice('\n')
	if err != nil || len(line) > sourcepreview.MaxDescriptorBytes {
		clear(line)
		os.Exit(2)
	}
	descriptor, err := sourcepreview.Decode(line, time.Now())
	clear(line)
	if err != nil {
		os.Exit(2)
	}
	defer descriptor.Destroy()
	e := &events{out: json.NewEncoder(out), sessionID: descriptor.SessionID()}
	if !previewDisplayAvailable() {
		e.stopped("display_unavailable")
		return
	}

	ctx, cancel := context.WithDeadline(context.Background(), descriptor.ExpiresAt())
	defer cancel()
	var reasonMu sync.Mutex
	reason := "closed"
	stop := func(why string) {
		reasonMu.Lock()
		if ctx.Err() == nil {
			reason = why
		}
		reasonMu.Unlock()
		cancel()
	}
	go func() {
		// EOF ends the lease; any extra byte is invalid protocol and ends it too.
		_, err := reader.ReadByte()
		if err == io.EOF {
			stop("lease_ended")
		} else {
			stop("lease_protocol_error")
		}
	}()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() {
		select {
		case <-signals:
			stop("interrupted")
		case <-ctx.Done():
		}
	}()

	diagnostic, finishDiagnostic := beginWindowDiagnostics()
	a := app.New()
	window := a.NewWindow("Source preview (experimental, this computer)")
	picture := canvas.NewImageFromImage(image.NewRGBA(image.Rect(0, 0, sourcepreview.DefaultWidth, sourcepreview.DefaultHeight)))
	picture.FillMode = canvas.ImageFillContain
	picture.SetMinSize(fyne.NewSize(640, 360))
	label := widget.NewLabel("Connecting · View only · Silent audio · Up to 30 seconds")
	window.SetContent(container.NewBorder(label, widget.NewButton("Stop preview", func() { stop("closed") }), nil, nil, picture))
	window.Resize(fyne.NewSize(680, 440))
	window.SetCloseIntercept(func() { stop("closed") })
	window.Show()
	startupFailure := previewWindowFailure(window)
	finishDiagnostic()
	if startupFailure != "" {
		e.startupFailed(diagnostic.failure(startupFailure))
		return
	}
	renderer := service.NewSourcePreviewService()
	renderer.SetOnFrameReceived(func(frame image.Image) {
		if ctx.Err() != nil || frame == nil || frame.Bounds().Empty() {
			return
		}
		fyne.Do(func() {
			if ctx.Err() != nil {
				return
			}
			picture.Image = frame
			picture.Refresh()
			e.displayed()
		})
	})
	renderer.SetOnStateChanged(func(state string) {
		if state == "connected" && ctx.Err() == nil {
			e.connected()
			fyne.Do(func() { label.SetText("Source preview · View only · Silent audio · Up to 30 seconds") })
		}
	})
	renderer.SetOnError(func(error) { stop("renderer_error") })
	a.Lifecycle().SetOnStarted(func() {
		go func() {
			if err := renderer.ConnectToSourcePreview(ctx, descriptor, func() { stop("renderer_stopped") }); err != nil {
				stop("renderer_error")
			}
		}()
		go func() {
			<-ctx.Done()
			_ = renderer.Disconnect()
			fyne.Do(func() { window.SetCloseIntercept(nil); window.Close() })
		}()
	})
	a.Run()
	cancel()
	_ = renderer.Disconnect()
	reasonMu.Lock()
	if ctx.Err() == context.DeadlineExceeded {
		reason = "expired"
	}
	why := reason
	reasonMu.Unlock()
	e.stopped(why)
}
