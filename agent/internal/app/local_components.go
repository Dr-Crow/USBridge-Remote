package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"usbridge_agent/internal/config"
	"usbridge_agent/internal/localcomponents"
	"usbridge_agent/internal/localruntime"
	"usbridge_agent/internal/netpolicy"
	"usbridge_agent/internal/streamhost"
)

func localOptions(c config.Config) localcomponents.Options {
	return localcomponents.Options{StateDir: c.StateDir, Directory: c.LocalComponentDirectory, Bundle: c.LocalComponentBundle, Mirror: c.LocalComponentMirror, ManifestSHA256: c.LocalComponentManifestSHA256, CAFile: c.LocalComponentCAFile}
}
func prepareLocalComponent(ctx context.Context, c config.Config, name string) error {
	if !netpolicy.Strict() {
		return nil
	}
	if name == "rustshine" && (!c.StreamerConsent || !localruntime.Enabled()) {
		return fmt.Errorf("local RustShine requires separate streamer and pinned-runtime consent")
	}
	if name == "broker" && (!c.USBBrokerConsentGiven() || !localruntime.Enabled()) {
		return fmt.Errorf("local USB broker requires separate USB and pinned-runtime consent")
	}
	r, err := localcomponents.Resolve(ctx, localOptions(c), name)
	if err != nil {
		return err
	}
	if name == "sunshine" {
		streamhost.SetSunshineStageBinary(r.Binary)
	}
	if name == "punktfunk" {
		streamhost.SetPunktfunkStageDir(filepath.Dir(r.Binary))
	}
	return nil
}
func prepareLocalStartup(ctx context.Context, c config.Config) error {
	if !netpolicy.Strict() {
		return nil
	}
	name := c.PreferredBackend
	if name == "" {
		name = "sunshine"
	}
	var problems []error
	if err := prepareLocalComponent(ctx, c, name); err != nil {
		problems = append(problems, err)
	}
	if c.USBBrokerConsentGiven() {
		if err := prepareLocalComponent(ctx, c, "broker"); err != nil {
			problems = append(problems, err)
		}
	}
	return errors.Join(problems...)

}
func (a *App) prepareLocalComponent(ctx context.Context, name string) error {
	return prepareLocalComponent(ctx, a.cfg, name)
}

func (a *App) punktfunkAvailable() bool {
	if netpolicy.Strict() {
		return localcomponents.PreparedPath(a.cfg.StateDir, "punktfunk") != ""
	}
	return streamhost.PunktfunkAvailable(a.exeDir)
}
