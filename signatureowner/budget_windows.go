//go:build windows && amd64

// SPDX-License-Identifier: GPL-3.0-only
package signatureowner

import "context"

func (p *child) waitContext(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.done:
		return naturalChildExit(p.exitCode, p.owner == nil || p.owner.closed.Load())
	}
}
func (p *child) nextContext(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case packet, ok := <-p.packets:
		if !ok {
			return nil, failure("protocol_ended_early")
		}
		return packet.line, packet.err
	}
}
func (p *child) finishProtocolContext(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case packet, ok := <-p.packets:
		if ok {
			clear(packet.line)
			return failure("extra_child_output")
		}
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-p.drained:
		return err
	}
}
