package main

import (
	"context"
	"os"
	"os/signal"
	"usbridge_agent/internal/localcomponents"
	"usbridge_agent/internal/sourcebroker"
)

func runSourceBroker(state, directory, manifest string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return sourcebroker.RunStdio(ctx, localcomponents.Options{StateDir: state, Directory: directory, ManifestSHA256: manifest}, os.Stdin, os.Stdout)
}
