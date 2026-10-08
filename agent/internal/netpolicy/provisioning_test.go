package netpolicy

import (
	"context"
	"errors"
	"testing"
)

func TestLocalRuntimeProvisioningIsRequestScoped(t *testing.T) {
	t.Setenv(Environment, "")
	Configure(false)
	ConfigureRuntimeLocal(true)
	t.Cleanup(func() { Configure(false); ConfigureRuntimeLocal(false) })
	if !RuntimeLocal() || Strict() {
		t.Fatal("runtime policy and strict offline policy were conflated")
	}
	ordinary := context.Background()
	setup := WithPublicProvisioning(ordinary)
	if !errors.Is(RequireRuntimeOnline("account"), ErrRuntimeLocal) {
		t.Fatal("account allowed")
	}
	if !errors.Is(RequireProvisioning(ordinary, "component"), ErrRuntimeLocal) {
		t.Fatal("implicit provisioning allowed")
	}
	if err := RequireProvisioning(setup, "component"); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(RequireProvisioning(ordinary, "component"), ErrRuntimeLocal) {
		t.Fatal("provisioning changed global policy")
	}
	if !errors.Is(RequireRuntimeOnline("TURN"), ErrRuntimeLocal) {
		t.Fatal("setup enabled runtime cloud traffic")
	}
	Configure(true)
	if !errors.Is(RequireProvisioning(setup, "component"), ErrStrictLAN) {
		t.Fatal("setup bypassed strict offline mode")
	}
}
