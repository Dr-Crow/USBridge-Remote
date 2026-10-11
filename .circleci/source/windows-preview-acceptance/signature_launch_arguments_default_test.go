//go:build !vm330_signature_probe

package main

import (
	"reflect"
	"testing"
)

func TestDefaultSignatureLaunchPolicyUnchanged(t *testing.T) {
	want := []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-File", "owned-script.ps1"}
	if !reflect.DeepEqual(graphicsSignatureLaunchArguments("owned-script.ps1"), want) {
		t.Fatal("default signature policy changed")
	}
}
