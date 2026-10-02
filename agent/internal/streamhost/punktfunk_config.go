package streamhost

import (
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

// punktfunkConfigDir is where this agent pins punktfunk-host's config
// directory (via PUNKTFUNK_CONFIG_DIR, set in Start -- punktfunk_backend.go)
// instead of its generic per-OS default ($HOME/.config/punktfunk,
// %ProgramData%\punktfunk). Same rationale as sunshineDataDir: an
// independently installed standalone Punktfunk on the same machine must
// never collide with this agent's own managed instance.
func (b *punktfunkBackend) punktfunkConfigDir() string {
	if b.stateDir == "" {
		return ""
	}
	return filepath.Join(b.stateDir, "punktfunk")
}

// ConfigPath returns where punktfunk-host keeps host-settings.json (the
// settings store SetConfigKey/ConfigKey read/write) -- see
// punktfunk-host's own settings_cli doc comment in main.rs ("settings set
// <ID> <VALUE> writes the console's settings store without a running host").
func (b *punktfunkBackend) ConfigPath() string {
	dir := b.punktfunkConfigDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "host-settings.json")
}

// LogPath: punktfunk-host's own internal log file path is not confirmed yet
// (only this backend's stdout/stderr capture, b.logPath, is wired -- see
// Start). "" until confirmed, same as CapExecPath off Linux elsewhere in
// this package -- callers already treat an empty path as "not applicable".
func (b *punktfunkBackend) LogPath() string { return "" }

// SetConfigKey writes one punktfunk-host setting via its own `settings set`
// CLI (confirmed command, see main.rs settings_cli) -- the only way to write
// host-settings.json documented as safe without a running host, since
// hand-editing it would race the host's own debounced writer. Per that same
// CLI's own doc comment, most settings only take effect after punktfunk-host
// restarts.
func (b *punktfunkBackend) SetConfigKey(key, value string) error {
	bin := b.binaryPath()
	if bin == "" {
		return nil
	}
	cmd := exec.Command(bin, "settings", "set", key, value)
	cmd.Env = append(os.Environ(), punktfunkConfigDirEnv+"="+b.punktfunkConfigDir())
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("[punktfunk] settings set %s failed: %v: %s", key, err, out)
		return err
	}
	return nil
}

// ConfigKey reads one value back from host-settings.json. "" if unset or
// the file doesn't exist yet (fresh install, or punktfunk-host never
// started).
func (b *punktfunkBackend) ConfigKey(key string) string {
	path := b.ConfigPath()
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var store map[string]json.RawMessage
	if json.Unmarshal(data, &store) != nil {
		return ""
	}
	raw, ok := store[key]
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	// Not a JSON string (bool/number) -- return the raw JSON text rather
	// than silently losing the value.
	return string(raw)
}

// SetExternalIP, SetBindAddress, SetCaptureMode, SetAudioSink, and
// SetOutputName are not yet wired to real pf-host-config setting IDs --
// those weren't researched this pass (see
// agent/docs/PUNKTFUNK_BACKEND_TODO.md). No-ops rather than guessed IDs that
// could silently write the wrong setting: GameStream streaming still works
// with Punktfunk's own auto-detection (capture monitor, audio sink) in the
// meantime.
func (b *punktfunkBackend) SetExternalIP(ip string) error    { return nil }
func (b *punktfunkBackend) ExternalIP() string               { return "" }
func (b *punktfunkBackend) SetBindAddress(ip string) error   { return nil }
func (b *punktfunkBackend) BindAddress() string              { return "" }
func (b *punktfunkBackend) SetCaptureMode(mode string) error { return nil }
func (b *punktfunkBackend) CaptureMode() string              { return "" }
func (b *punktfunkBackend) SetAudioSink(sink string) error   { return nil }
func (b *punktfunkBackend) AudioSink() string                { return "" }
func (b *punktfunkBackend) SetOutputName(name string) error  { return nil }
func (b *punktfunkBackend) OutputName() string               { return "" }
