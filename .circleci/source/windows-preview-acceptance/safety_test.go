package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestNoDesktopCaptureOrGlobalInputAPI(t *testing.T) {
	for _, name := range []string{"winapi_windows.go", "run_windows.go", "main.go", "window_startup.go", "window_startup_windows.go"} {
		raw, e := os.ReadFile(name)
		if e != nil {
			t.Fatal(e)
		}
		f, e := parser.ParseFile(token.NewFileSet(), name, raw, 0)
		if e != nil {
			t.Fatal(e)
		}
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if path == "os/exec" {
				t.Fatal("production process launch must use the suspended job launcher")
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "NewProc" || len(call.Args) != 1 {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok {
				t.Fatal("dynamic native API selection")
			}
			api, _ := strconv.Unquote(lit.Value)
			for _, forbidden := range []string{"GetDC", "GetWindowDC", "GetDesktopWindow", "BitBlt", "StretchBlt", "SendInput", "mouse_event", "keybd_event", "DwmGetDxSharedSurface"} {
				if api == forbidden {
					t.Fatal("forbidden desktop/global input API")
				}
			}
			return true
		})
	}
}
func TestNoGeneratedOrPrivateInputsCommitted(t *testing.T) {
	entries, e := os.ReadDir(".")
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range entries {
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext == ".exe" || ext == ".dll" || ext == ".h264" || ext == ".ogg" || name == "result.json" {
			t.Fatal("generated runtime artifact in source directory")
		}
	}
}
