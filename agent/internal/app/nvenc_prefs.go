package app

import (
	"log"

	"usbridge_agent/internal/config"
	"usbridge_agent/internal/streamhost"
)

// nvencPrefKeys are the NVIDIA encoder preferences as both streamers read
// them: Sunshine's own config keys and values, which RustShine takes too.
func (a *App) nvencPrefKeys() map[string]string {
	twoPass, maxPerf := "disabled", "disabled"
	if a.cfg.NvencTwoPassOK() {
		twoPass = "quarter_res"
	}
	if a.cfg.NvidiaMaxPerformanceOK() {
		maxPerf = "enabled"
	}
	return map[string]string{
		"nvenc_twopass":            twoPass,
		"nvenc_latency_over_power": maxPerf,
	}
}

// applyNvencPrefs writes the NVIDIA encoder preferences into b's config,
// reporting whether anything changed (a running streamer then needs a
// restart to pick it up).
func (a *App) applyNvencPrefs(b streamhost.Backend) (changed bool) {
	if b == nil {
		return false
	}
	for key, value := range a.nvencPrefKeys() {
		if b.ConfigKey(key) == value {
			continue
		}
		if err := b.SetConfigKey(key, value); err != nil {
			log.Printf("[app] writing %s = %s: %v", key, value, err)
			continue
		}
		changed = true
	}
	return changed
}

func (a *App) NvencTwoPassEnabled() bool { return a.cfg.NvencTwoPassOK() }

func (a *App) NvidiaMaxPerformanceEnabled() bool { return a.cfg.NvidiaMaxPerformanceOK() }

func (a *App) SetNvencTwoPass(enabled bool) error {
	next := a.cfg
	next.NvencTwoPass = &enabled
	return a.saveNvencPrefs(next)
}

func (a *App) SetNvidiaMaxPerformance(enabled bool) error {
	next := a.cfg
	next.NvidiaMaxPerformance = &enabled
	return a.saveNvencPrefs(next)
}

// saveNvencPrefs persists the preferences and restarts the running
// streamer when its config changed; the other streamer gets them on its
// next start (startSunshineNow).
func (a *App) saveNvencPrefs(next config.Config) error {
	if err := a.SaveConfig(next); err != nil {
		return err
	}
	a.streamMu.Lock()
	defer a.streamMu.Unlock()
	if a.applyNvencPrefs(a.stream) && a.stream.Running() {
		log.Printf("[app] NVIDIA encoder preferences changed, restarting %s", a.streamKind)
		return a.RestartSunshine()
	}
	return nil
}
