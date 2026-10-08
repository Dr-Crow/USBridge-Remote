// Package netpolicy owns the agent's startup-frozen strict-LAN policy.
// Vendor requests must check this boundary before constructing a network request.
package netpolicy

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"sync/atomic"
)

const Environment = "USBRIDGE_STRICT_LAN"

var configured atomic.Bool
var runtimeLocal atomic.Bool

type provisioningKey struct{}

// ConfigureRuntimeLocal separates runtime cloud access from explicit setup downloads.
func ConfigureRuntimeLocal(local bool) { runtimeLocal.Store(local) }
func RuntimeLocal() bool               { return Strict() || runtimeLocal.Load() }

var ErrRuntimeLocal = errors.New("public runtime service disabled by local-runtime network policy")

func RequireRuntimeOnline(operation string) error {
	if err := RequireOnline(operation); err != nil {
		return err
	}
	if RuntimeLocal() {
		return fmt.Errorf("%s: %w", operation, ErrRuntimeLocal)
	}
	return nil
}

// WithPublicProvisioning grants only request-scoped component setup access.
// It never disables runtime guards or strict offline mode.
func WithPublicProvisioning(ctx context.Context) context.Context {
	return context.WithValue(ctx, provisioningKey{}, true)
}
func RequireProvisioning(ctx context.Context, operation string) error {
	if err := RequireOnline(operation); err != nil {
		return err
	}
	if RuntimeLocal() && ctx.Value(provisioningKey{}) != true {
		return fmt.Errorf("%s: explicit provisioning context required: %w", operation, ErrRuntimeLocal)
	}
	return nil
}

var ErrStrictLAN = errors.New("operation disabled by strict-LAN policy")

// Configure applies only at engine/GUI startup, never from a live settings save.
func Configure(strict bool) { configured.Store(strict) }

func Strict() bool { return configured.Load() || os.Getenv(Environment) == "1" }

func RequireOnline(operation string) error {
	if Strict() {
		return fmt.Errorf("%s: %w", operation, ErrStrictLAN)
	}
	return nil
}

// LocalURL permits only explicit private/link-local/loopback IP addresses.
// It never resolves a hostname through a potentially public DNS server. Mirrors
// require HTTPS except loopback test/development servers; redirects are handled
// separately by their client and may never introduce public fallback.
func LocalURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.User != nil || u.Fragment != "" {
		return nil, errors.New("local URL must not contain credentials or a fragment")
	}
	addr, err := netip.ParseAddr(u.Hostname())
	if err != nil || !(addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast()) {
		return nil, errors.New("local URL requires an explicit private, link-local or loopback IP; DNS/public destinations are not allowed")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && addr.IsLoopback()) {
		return nil, errors.New("local URL requires HTTPS (HTTP allowed only on loopback)")
	}
	return u, nil
}
