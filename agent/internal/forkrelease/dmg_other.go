//go:build !darwin

package forkrelease

import (
	"context"
	"fmt"
)

func extractDMGApp(context.Context, string, string) error {
	return fmt.Errorf("DMG images are macOS-only")
}
