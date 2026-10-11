//go:build windows

package main

import "context"

func (p *child) signatureWaitContext(ctx context.Context) error {
	if _, _, err := signatureReceive(ctx, p.done); err != nil {
		return err
	}
	return naturalChildExit(p.exitCode, p.owner == nil || p.owner.closed.Load())
}

func (p *child) signatureNextContext(ctx context.Context) ([]byte, error) {
	packet, ok, err := signatureReceive(ctx, p.packets)
	if err != nil {
		clear(packet.line)
		return nil, err
	}
	if !ok {
		return nil, failure("protocol_ended_early")
	}
	return packet.line, packet.err
}

func (p *child) signatureFinishProtocolContext(ctx context.Context) error {
	packet, ok, err := signatureReceive(ctx, p.packets)
	clear(packet.line)
	if err != nil {
		return err
	}
	if ok {
		return failure("extra_child_output")
	}
	drainErr, _, err := signatureReceive(ctx, p.drained)
	if err != nil {
		return err
	}
	return drainErr
}
