package app

import (
	"context"
	"errors"
	"testing"
)

type testOwnedBridge struct {
	closed  int
	failure error
}

func (b *testOwnedBridge) Close(ctx context.Context) error {
	b.closed++
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("missing deadline")
	}
	return b.failure
}
func TestUSBBridgeEngineRollbackAndCloseFailure(t *testing.T) {
	primary, closeErr := errors.New("engine failed"), errors.New("close incomplete")
	for _, runErr := range []error{nil, primary} {
		for _, shutdownErr := range []error{nil, closeErr} {
			b := &testOwnedBridge{failure: shutdownErr}
			err := withUSBPassBridge(context.Background(), func(context.Context) (ownedUSBBridge, error) { return b, nil }, func() error { return runErr }, nil)
			if b.closed != 1 || (runErr != nil && !errors.Is(err, runErr)) || (shutdownErr != nil && !errors.Is(err, shutdownErr)) || (runErr == nil && shutdownErr == nil && err != nil) {
				t.Fatal("engine rollback or error provenance lost")
			}
		}
	}
}
func TestUSBBridgeEngineStartupDoesNotRunAfterFailure(t *testing.T) {
	sentinel := errors.New("failed start")
	err := withUSBPassBridge(context.Background(), func(context.Context) (ownedUSBBridge, error) { return nil, sentinel }, func() error { t.Fatal("engine ran without bridge owner"); return nil }, nil)
	if !errors.Is(err, sentinel) {
		t.Fatal("startup error changed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	b := &testOwnedBridge{}
	err = withUSBPassBridge(ctx, func(context.Context) (ownedUSBBridge, error) { cancel(); return b, nil }, func() error { t.Fatal("engine ran after canceled handoff"); return nil }, nil)
	if !errors.Is(err, context.Canceled) || b.closed != 1 {
		t.Fatal("canceled owner handoff leaked")
	}
}

func TestUSBBridgeEnginePartialStartupRetiresOwner(t *testing.T) {
	sentinel := errors.New("partial start failed")
	b := &testOwnedBridge{}
	err := withUSBPassBridge(context.Background(), func(context.Context) (ownedUSBBridge, error) { return b, sentinel }, func() error { t.Fatal("partial start ran engine"); return nil }, nil)
	if b.closed != 1 || !errors.Is(err, sentinel) {
		t.Fatal("partial owner lost")
	}
}

func TestUSBBridgeOptionalUnavailabilityPreservesDegradedEngine(t *testing.T) {
	failed := testBridgeUnavailable{}
	reported, ran := 0, 0
	err := withUSBPassBridge(context.Background(), func(context.Context) (ownedUSBBridge, error) { return nil, failed }, func() error { ran++; return nil }, func(err error) {
		if !errors.Is(err, failed) {
			t.Fatal("wrong report")
		}
		reported++
	})
	if err != nil || reported != 1 || ran != 1 {
		t.Fatal("optional bridge failure changed startup")
	}
	ctx, cancel := context.WithCancel(context.Background())
	err = withUSBPassBridge(ctx, func(context.Context) (ownedUSBBridge, error) { cancel(); return nil, context.Canceled }, func() error { t.Fatal("canceled startup degraded into running"); return nil }, func(error) { t.Fatal("cancellation treated as optional") })
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost")
	}
	b := &testOwnedBridge{}
	err = withUSBPassBridge(context.Background(), func(context.Context) (ownedUSBBridge, error) { return b, failed }, func() error { t.Fatal("partial startup continued"); return nil }, func(error) { t.Fatal("partial owner ignored") })
	if b.closed != 1 || !errors.Is(err, failed) {
		t.Fatal("partial owner was not retired")
	}
}

type testBridgeUnavailable struct{}

func (testBridgeUnavailable) Error() string              { return "listener unavailable" }
func (testBridgeUnavailable) USBBridgeUnavailable() bool { return true }

func TestUSBBridgeUncertainStartupCannotDegrade(t *testing.T) {
	for _, startupErr := range []error{errors.New("unknown startup"), errors.Join(testBridgeUnavailable{}, errors.New("cleanup failed"))} {
		err := withUSBPassBridge(context.Background(), func(context.Context) (ownedUSBBridge, error) { return nil, startupErr }, func() error { t.Fatal("uncertain startup ran engine"); return nil }, func(error) { t.Fatal("uncertain startup treated as unavailable") })
		if !errors.Is(err, startupErr) {
			t.Fatal("startup cause lost")
		}
	}
}
