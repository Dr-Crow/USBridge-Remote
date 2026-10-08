package app

import (
	"fmt"
	"log"

	"usbridge_agent/internal/config"
	"usbridge_agent/internal/localruntime"
)

// ValidateServiceRuntime preserves the interactive-only service-install guard
// for saved preferences as well as the older flag/environment opt-in.
func ValidateServiceRuntime() error {
	cfg, err := config.Load(resolveConfigPath())
	if err != nil {
		return err
	}
	return validateServiceRuntime(cfg.LocalRuntimeEnabled)
}

func validateServiceRuntime(configured bool) error {
	if configured || localruntime.Enabled() {
		return fmt.Errorf("local runtime research mode cannot run or install a system service")
	}
	return nil
}

// SetLocalRuntimeEnabled saves explicit opt-in for the next engine start.
// Component consent, vendor credentials and the live engine mode are unchanged.
func (a *App) SetLocalRuntimeEnabled(enabled bool) error {
	if a.cfg.LocalRuntimeEnabled == enabled {
		return nil
	}
	next := a.cfg
	next.LocalRuntimeEnabled = enabled
	if err := a.SaveConfig(next); err != nil {
		return err
	}
	log.Printf("[setup] local runtime preference saved=%t active=%t; restart engine to apply", enabled, localruntime.Enabled())
	return nil
}
