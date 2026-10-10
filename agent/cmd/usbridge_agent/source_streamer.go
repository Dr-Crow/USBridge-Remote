package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"usbridge_agent/internal/localcomponents"
	"usbridge_agent/internal/sourcestreamer"
)

func runSourceStreamer(state, directory, manifest string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return sourcestreamer.RunStdio(ctx, localcomponents.Options{StateDir: state, Directory: directory, ManifestSHA256: manifest}, os.Stdin, &sourceProcessOutput{context: ctx, output: os.Stdout})
}
