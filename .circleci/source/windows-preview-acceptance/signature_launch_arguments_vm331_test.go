//go:build vm331_signature_probe

package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestVM331SignaturePolicyIsFixedProcessOnly(t *testing.T) {
	want := []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "RemoteSigned", "-File", "owned-script.ps1"}
	if !reflect.DeepEqual(graphicsSignatureLaunchArguments("owned-script.ps1"), want) {
		t.Fatal("VM test policy must be exact")
	}
	if strings.Contains(strings.Join(graphicsSignatureArguments("owned-script.ps1"), " "), "ExecutionPolicy") {
		t.Fatal("default invocation changed")
	}
	if strings.Contains(graphicsSignatureScript, "Set-ExecutionPolicy") {
		t.Fatal("persistent policy change prohibited")
	}
}
