//go:build !linux

package app

// sunshineEncoderPin is Linux-only: see sunshine_encoder_linux.go.
func sunshineEncoderPin() string { return "" }
