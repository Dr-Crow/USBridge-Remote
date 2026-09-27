package entitlement

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TurnCredentialsFilePath is where the current TURN credential is mirrored
// to disk for the RustShine process to pick up via its own
// --turn-credentials-file flag -- see rust-shine's crates/webrtc-video
// turn_credentials module. Same directory/naming convention as
// TokenFilePath, and duplicated (not imported) by
// agent/internal/streamhost/rustshine_backend.go for the identical reason
// that file's own doc comment gives.
func TurnCredentialsFilePath(stateDir string) string {
	return filepath.Join(stateDir, "rustshine", "turn-credentials.json")
}

// turnCredentialsFileBody is the on-disk JSON shape --  MUST stay in sync
// with rust-shine's TurnCredentialsFile struct (crates/webrtc-video/src/
// turn_credentials.rs).
type turnCredentialsFileBody struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username"`
	Credential string   `json:"credential"`
	FetchedAt  int64    `json:"fetched_at"`
	ExpiresIn  int      `json:"expires_in"`
}

// WriteTurnCredentialsFile atomically (over)writes the TURN credentials
// file -- same temp-file+rename discipline WriteTokenFile already relies
// on (writeAtomic, download.go), so rust-shine's own poller (which re-reads
// this file every couple minutes, see turn_credentials::spawn_watchdog)
// never observes a half-written file. 0600: same trust level as the
// entitlement token file -- a TURN credential lets its bearer relay
// arbitrary UDP traffic through Cloudflare's network at real cost, see
// usbridge-entitlement-backend's webrtcTurn.ts doc comment.
func WriteTurnCredentialsFile(stateDir string, creds *TurnCredentials, fetchedAt time.Time) error {
	dest := TurnCredentialsFilePath(stateDir)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	body := turnCredentialsFileBody{
		URLs:       creds.URLs,
		Username:   creds.Username,
		Credential: creds.Credential,
		FetchedAt:  fetchedAt.Unix(),
		ExpiresIn:  creds.ExpiresIn,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return writeAtomic(dest, strings.NewReader(string(raw)), 0o600)
}

// ClearTurnCredentialsFile removes any previously written TURN credentials
// file -- called once this hardware id is no longer pro/enterprise (a
// downgrade/refund/cancellation), so a stale-but-not-yet-expired credential
// doesn't keep getting offered to new sessions for up to its own remaining
// TTL after the tier that earned it is gone. Not an error if the file was
// already absent.
func ClearTurnCredentialsFile(stateDir string) error {
	err := os.Remove(TurnCredentialsFilePath(stateDir))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
