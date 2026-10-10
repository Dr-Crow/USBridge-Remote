//go:build windows && cgo

package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"testing"

	"golang.org/x/sys/windows"
)

func TestPrivateEventWriterIsolatesWindowsAndCRTOutput(t *testing.T) {
	if os.Getenv("PREVIEW_OUTPUT_TEST_CHILD") == "1" {
		oldOut, oldErr := os.Stdout, os.Stderr
		out, err := privateEventWriter()
		if err != nil {
			os.Exit(81)
		}
		// GetHandleInformation is intentionally inspected in the same process
		// as the duplicate, before any handle can be closed or reused.
		if !previewEventHandleIsPrivate(out) {
			os.Exit(82)
		}
		fmt.Fprintln(oldOut, "cached-go-stdout-must-not-escape")
		fmt.Fprintln(oldErr, "cached-go-stderr-must-not-escape")
		fmt.Fprintln(os.Stdout, "go-stdout-must-not-escape")
		fmt.Fprintln(os.Stderr, "go-stderr-must-not-escape")
		log.Print("standard-logger-must-not-escape")
		println("runtime-output-must-not-escape")
		for _, id := range []uint32{windows.STD_OUTPUT_HANDLE, windows.STD_ERROR_HANDLE} {
			h, err := windows.GetStdHandle(id)
			if err != nil {
				os.Exit(83)
			}
			var written uint32
			if windows.WriteFile(h, []byte("win32-output-must-not-escape\n"), &written, nil) != nil {
				os.Exit(84)
			}
		}
		previewCRTOutputProbe()
		if _, err := out.WriteString("event-channel-only\n"); err != nil {
			os.Exit(85)
		}
		_ = out.Close()
		os.Exit(0)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestPrivateEventWriterIsolatesWindowsAndCRTOutput$")
	cmd.Env = append(os.Environ(), "PREVIEW_OUTPUT_TEST_CHILD=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("isolation subprocess failed: %v", err)
	}
	if stdout.String() != "event-channel-only\n" || stderr.Len() != 0 {
		t.Fatal("non-event output escaped isolation")
	}
}

func TestClearPreviewEnvironmentScrubsActualCRT(t *testing.T) {
	keys := []string{"USBRIDGE_FRAME_DUMP_DIR", "USBRIDGE_SKIP_DECODE", "USBRIDGE_UNKNOWN_FUTURE", "usbridge_mixed_case"}
	for _, key := range keys {
		t.Setenv(key, "go-private-diagnostic")
		if err := previewCRTSetenv(key, "native-private-diagnostic"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = previewCRTSetenv(key, "") })
		if _, ok := previewCRTLookupEnv(key); !ok {
			t.Fatal("native test setup did not reach getenv")
		}
	}
	// A C-only value may be absent from Go's cached os.Environ snapshot.
	const cOnly = "USBRIDGE_CRT_ONLY"
	if err := previewCRTSetenv(cOnly, "native-only-private-diagnostic"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = previewCRTSetenv(cOnly, "") })
	t.Setenv("PREVIEW_KEEP_RUNTIME", "retained")
	if err := previewCRTSetenv("PREVIEW_KEEP_RUNTIME", "retained"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = previewCRTSetenv("PREVIEW_KEEP_RUNTIME", "") })
	if err := previewCRTSetWideProbe(); err != nil {
		t.Fatal(err)
	}
	if !previewCRTContainsWideProbe() {
		t.Fatal("wide CRT probe was not initialized")
	}
	if err := clearPreviewEnvironment(); err != nil {
		t.Fatal(err)
	}
	if previewCRTContainsWideProbe() {
		t.Fatal("wide CRT retained a diagnostic")
	}
	for _, key := range append(keys, cOnly, "USBRIDGE_WIDE_ONLY") {
		if _, ok := previewCRTLookupEnv(key); ok {
			t.Fatal("C getenv retained a diagnostic")
		}
		key16, err := windows.UTF16PtrFromString(key)
		if err != nil {
			t.Fatal(err)
		}
		var value [256]uint16
		if _, err := windows.GetEnvironmentVariable(key16, &value[0], uint32(len(value))); err != windows.ERROR_ENVVAR_NOT_FOUND {
			t.Fatal("Win32 retained a diagnostic")
		}
		if _, ok := os.LookupEnv(key); ok {
			t.Fatal("Go retained a diagnostic")
		}
	}
	if got, ok := previewCRTLookupEnv("PREVIEW_KEEP_RUNTIME"); !ok || got != "retained" {
		t.Fatal("ordinary CRT runtime variable changed")
	}
	if err := clearPreviewEnvironment(); err != nil {
		t.Fatal("native scrub is not idempotent")
	}
}
