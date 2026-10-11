//go:build !windows

package previewchild

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestUnixSyntheticEOFHelper(t *testing.T) {
	if os.Getenv("PREVIEWCHILD_UNIX_HELPER") != "1" {
		return
	}
	fmt.Fprintln(os.Stdout, "ready")
	_, _ = io.Copy(io.Discard, os.Stdin)
	fmt.Fprintln(os.Stdout, "cooperative-eof")
	os.Exit(0)
}

func TestUnixSourceContextCancellationStillOffersEOF(t *testing.T) {
	unixCancellation(t, true)
}
func TestUnixViewerContextCancellationStillKillsRoot(t *testing.T) {
	unixCancellation(t, false)
}
func unixCancellation(t *testing.T, cooperative bool) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	child, err := Start(ctx, Spec{Path: exe, Args: []string{"-test.run=^TestUnixSyntheticEOFHelper$"}, Env: append(os.Environ(), "PREVIEWCHILD_UNIX_HELPER=1"), CloseStdinOnCancel: cooperative})
	if err != nil {
		t.Fatal(err)
	}
	defer child.Stdout.Close()
	defer child.Stdin.Close()
	reader := bufio.NewReader(child.Stdout)
	if line, err := reader.ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatal("helper did not become ready")
	}
	cancel()
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal("stdout drain failed")
	}
	_ = child.Wait() // CommandContext reports cancellation even after a clean EOF exit.
	if strings.Contains(string(output), "cooperative-eof") != cooperative {
		t.Fatal("Unix context cancellation policy changed")
	}
}
