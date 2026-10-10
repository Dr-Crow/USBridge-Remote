//go:build linux && source_preview_acceptance

package sourcepreview

// This build-tagged observer wraps the production launchers; it cannot grant
// consent, supply keys, substitute children, shorten the lease, or fake frames.
import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"

	"usbridge_agent/internal/localcomponents"
	"usbridge_agent/internal/sourcestreamer"
)

type AcceptanceSnapshot struct {
	PrepareCalls  int  `json:"prepare_calls"`
	StreamStarts  int  `json:"stream_starts"`
	ViewerStarts  int  `json:"viewer_starts"`
	StreamsJoined int  `json:"streams_joined"`
	ViewersJoined int  `json:"viewers_joined"`
	FreshKeys     bool `json:"fresh_keys"`
	FreshIDs      bool `json:"fresh_ids"`
	FreshKeyIDs   bool `json:"fresh_key_ids"`
	CleanJoins    bool `json:"clean_joins"`
}

// NewAcceptanceObservedManager is absent from ordinary builds. Only booleans
// and counts leave this package. Key material and its hashes never reach disk.
func NewAcceptanceObservedManager() (*Manager, func() AcceptanceSnapshot) {
	m := New()
	var mu sync.Mutex
	s := AcceptanceSnapshot{FreshKeys: true, FreshIDs: true, FreshKeyIDs: true, CleanJoins: true}
	keys := map[[32]byte]bool{}
	ids := map[string]bool{}
	keyIDs := map[uint32]bool{}
	prepare, start, view := m.deps.prepareViewer, m.deps.startStream, m.deps.startViewer
	m.deps.prepareViewer = func(ctx context.Context, o localcomponents.Options) (string, error) {
		mu.Lock()
		s.PrepareCalls++
		mu.Unlock()
		return prepare(ctx, o)
	}
	m.deps.startStream = func(ctx context.Context, o localcomponents.Options, l sourcestreamer.Launch) (stream, error) {
		if l.Display != ":96" || l.FFmpeg != "/usr/bin/ffmpeg" || !l.CaptureConsent || l.InputConsent || l.Width != 128 || l.Height != 72 || l.PeerIP != "127.0.0.1" {
			return nil, errors.New("acceptance may capture only its approved generated display")
		}
		digest := sha256.Sum256([]byte(l.KeyB64))
		mu.Lock()
		s.FreshKeys = s.FreshKeys && !keys[digest]
		s.FreshIDs = s.FreshIDs && !ids[l.SessionID]
		s.FreshKeyIDs = s.FreshKeyIDs && !keyIDs[l.KeyID]
		keys[digest], ids[l.SessionID], keyIDs[l.KeyID] = true, true, true
		s.StreamStarts++
		mu.Unlock()
		child, err := start(ctx, o, l)
		if err == nil {
			go func() {
				e := child.Wait()
				mu.Lock()
				s.StreamsJoined++
				s.CleanJoins = s.CleanJoins && e == nil
				mu.Unlock()
			}()
		}
		return child, err
	}
	m.deps.startViewer = func(ctx context.Context, path string, d descriptor) (viewer, error) {
		mu.Lock()
		s.ViewerStarts++
		mu.Unlock()
		child, err := view(ctx, path, d)
		if err == nil {
			go func() {
				e := child.Wait()
				mu.Lock()
				s.ViewersJoined++
				s.CleanJoins = s.CleanJoins && e == nil
				mu.Unlock()
			}()
		}
		return child, err
	}
	return m, func() AcceptanceSnapshot { mu.Lock(); defer mu.Unlock(); return s }
}
