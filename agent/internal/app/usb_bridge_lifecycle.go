package app

import (
	"context"
	"errors"
	"time"
)

type ownedUSBBridge interface{ Close(context.Context) error }

// Only Run, not App.New, starts the browser bridge. Even a later engine-start
// failure must stop and join it. Cancellation stops its I/O independently; this
// one final, bounded join reports uncertainty instead of claiming clean exit.
func withUSBPassBridge(ctx context.Context, start func(context.Context) (ownedUSBBridge, error), run func() error, unavailable func(error)) (result error) {
	if ctx == nil || start == nil || run == nil {
		return errors.New("usbpass bridge: missing engine owner")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	bridge, err := start(ctx)
	if bridge == nil {
		// The caller may explicitly preserve degraded operation when Listen
		// failed without creating an owner. Cancellation and partial owners
		// must never take this optional path.
		safeUnavailable, classified := err.(interface{ USBBridgeUnavailable() bool })
		if classified && safeUnavailable.USBBridgeUnavailable() && ctx.Err() == nil && unavailable != nil {
			unavailable(err)
			if ended := ctx.Err(); ended != nil {
				return ended
			}
			return run()
		}
		return errors.Join(err, errors.New("usbpass bridge: missing started owner"))
	}
	defer func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		result = errors.Join(result, bridge.Close(shutdown))
	}()
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return run()
}
