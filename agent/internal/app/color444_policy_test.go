package app

import (
	"testing"

	"usbridge_agent/internal/config"
	"usbridge_agent/internal/entitlement"
	"usbridge_agent/internal/streamhost"
)

type color444StatusBackend struct {
	streamhost.Backend
	active, available bool
}

func (b *color444StatusBackend) Color444Status() (bool, bool) {
	return b.active, b.available
}

type color444ProbeBackend struct {
	color444StatusBackend
	supported    bool
	probes, port int
}

func (b *color444ProbeBackend) Color444Supported(port int) bool {
	b.probes++
	b.port = port
	return b.supported
}

func TestColor444StatusUsesCapabilityNotSubscription(t *testing.T) {
	for _, tier := range []string{"", "free", "pro", "enterprise"} {
		for _, supported := range []bool{false, true} {
			t.Run(tier+map[bool]string{false: "_unsupported", true: "_supported"}[supported], func(t *testing.T) {
				backend := &color444ProbeBackend{supported: supported}
				a := &App{stream: backend, cfg: config.Config{SunshinePort: 47990}, entStatus: entitlement.Status{Tier: tier}}
				active, available := a.Color444Status()
				if active || available != supported {
					t.Fatalf("got (%v, %v), want (false, %v)", active, available, supported)
				}
				if backend.probes != 1 || backend.port != 47990 {
					t.Fatalf("probe calls=%d port=%d", backend.probes, backend.port)
				}
			})
		}
	}
}

func TestColor444StatusPreservesBackendResult(t *testing.T) {
	backend := &color444ProbeBackend{color444StatusBackend: color444StatusBackend{active: true, available: true}}
	a := &App{stream: backend}
	if active, available := a.Color444Status(); !active || !available {
		t.Fatalf("backend result lost: (%v, %v)", active, available)
	}
	if backend.probes != 0 {
		t.Fatal("already available backend should not need an encoder probe")
	}
}

func TestColor444StatusDoesNotInventBackendSupport(t *testing.T) {
	for _, a := range []*App{{}, {stream: &color444StatusBackend{}}} {
		if active, available := a.Color444Status(); active || available {
			t.Fatalf("unsupported backend reported (%v, %v)", active, available)
		}
	}
}
