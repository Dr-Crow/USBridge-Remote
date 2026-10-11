//go:build windows && vm330_signature_probe

package main

import "io"

func (p *child) drainDiagnostics() (int64, error) {
	raw, err := io.ReadAll(io.LimitReader(p.stderr, 65537))
	defer clear(raw)
	observed := classifySignatureStartup(raw, err)
	p.signatureDiagnostic = &observed
	return int64(len(raw)), err
}
