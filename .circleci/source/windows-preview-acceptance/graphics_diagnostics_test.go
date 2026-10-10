package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestGraphicsRejectionDiagnosticsRemainFatalAndBounded(t *testing.T) {
	for _, tc := range []struct{ path, category string }{
		{`C:\private-job\bin\extra.dll`, "app_local"},
		{`C:\Windows\System32\extra.dll`, "windows_system32"},
		{`C:\WINDOWS\WinSxS\component-identity\extra.dll`, "windows_side_by_side"},
		{`\\?\C:\Windows\WinSxS\component\extra.dll`, "windows_side_by_side"},
		{`C:\Windows\System32\..\Temp\extra.dll`, "windows_other"},
		{`C:\WindowsOther\WinSxS\extra.dll`, "other"},
		{`C:\Windows\WinSxS-other\extra.dll`, "windows_other"},
		{`C:\private-job\bin-other\extra.dll`, "other"},
	} {
		err := rejectedGraphicsModule("graphics_module_not_in_verified_closure", "extra.dll", tc.path, `C:\private-job\bin`, `C:\Windows`, false)
		if err == nil || err.Error() != "graphics_module_not_in_verified_closure" {
			t.Fatal("diagnostics must preserve the exact rejection")
		}
		r := graphicsRejection(errors.Join(errors.New("outer"), err))
		if r == nil || r.Name != "extra.dll" || r.Location != tc.category || r.Declared {
			t.Fatalf("incorrect bounded category: %#v", r)
		}
		raw, _ := json.Marshal(r)
		if strings.Contains(string(raw), "private-job") || strings.Contains(string(raw), "component") || strings.Contains(string(raw), `C:`) {
			t.Fatal("private path escaped into receipt")
		}
	}
	for _, name := range []string{`..\extra.dll`, "extra.dll\n", "private.exe", strings.Repeat("a", 105) + ".dll"} {
		r := graphicsRejection(rejectedGraphicsModule("graphics_module_not_app_local", name, `D:\unknown\file.dll`, `C:\bin`, `C:\Windows`, true))
		if r.Name != "" || r.Location != "other" || !r.Declared {
			t.Fatal("unsafe basename was emitted")
		}
	}
	if graphicsRejection(errors.New("graphics_module_not_in_verified_closure")) != nil || graphicsRejection(nil) != nil {
		t.Fatal("untyped error became a module diagnosis")
	}
}
