//go:build windows

package previewchild

import "testing"

func TestWindowsExplicitEnvironmentDropsOnlyDrivePseudoVariables(t *testing.T) {
	env := []string{`=C:=C:\work`, `=D:=D:\old`, `SystemRoot=C:\Windows`, `TEST_VALUE=keep=equals`}
	got := explicitEnvironment(env)
	if len(got) != 2 || got[0] != env[2] || got[1] != env[3] {
		t.Fatal("explicit environment policy changed")
	}
	if len(env) != 4 {
		t.Fatal("caller environment was mutated")
	}
}
