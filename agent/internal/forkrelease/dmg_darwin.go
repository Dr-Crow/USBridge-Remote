package forkrelease

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// extractDMGApp copies the one .app in a DMG to dest. hdiutil mounts it
// read-only and out of Finder's sight; ditto keeps the bundle's signature,
// symlinks and xattrs intact. The DMG came from our own download (SHA-256
// already checked against the signed manifest), so it carries no quarantine.
func extractDMGApp(ctx context.Context, dmg, dest string) error {
	mnt, err := os.MkdirTemp("", "usbridge-dmg-")
	if err != nil {
		return err
	}
	defer os.Remove(mnt)
	if out, err := exec.CommandContext(ctx, "hdiutil", "attach", "-nobrowse", "-readonly", "-noautoopen",
		"-mountpoint", mnt, dmg).CombinedOutput(); err != nil {
		return fmt.Errorf("mount %s: %v: %s", filepath.Base(dmg), err, out)
	}
	defer func() {
		// Not ctx: a cancelled download must still unmount.
		dctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if exec.CommandContext(dctx, "hdiutil", "detach", mnt, "-quiet").Run() != nil {
			_ = exec.CommandContext(dctx, "hdiutil", "detach", mnt, "-force", "-quiet").Run()
		}
	}()
	apps, _ := filepath.Glob(filepath.Join(mnt, "*.app"))
	if len(apps) != 1 {
		return fmt.Errorf("%s holds %d apps, want 1", filepath.Base(dmg), len(apps))
	}
	if out, err := exec.CommandContext(ctx, "ditto", apps[0], dest).CombinedOutput(); err != nil {
		return fmt.Errorf("copy %s: %v: %s", filepath.Base(apps[0]), err, out)
	}
	return nil
}
