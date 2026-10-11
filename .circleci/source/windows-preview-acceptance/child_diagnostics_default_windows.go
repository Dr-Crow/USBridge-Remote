//go:build windows && !vm331_signature_probe

package main

import "io"

func (p *child) drainDiagnostics() (int64, error) {
	return io.Copy(io.Discard, io.LimitReader(p.stderr, 65537))
}
