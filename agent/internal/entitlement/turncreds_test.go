package entitlement

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func turnCredentialsServer(t *testing.T, status int, body any) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/webrtc/turn-credentials" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		var req struct {
			HwID string `json:"hw_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.HwID != "test-hw-id" {
			t.Errorf("hw_id = %q, want test-hw-id", req.HwID)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	prev := TestSetBackendBaseURL(srv.URL)
	t.Cleanup(func() { TestSetBackendBaseURL(prev) })
}

func TestFetchTurnCredentials_Success(t *testing.T) {
	turnCredentialsServer(t, http.StatusOK, map[string]any{
		"iceServers": map[string]any{
			"urls":       []string{"turn:turn.cloudflare.com:3478?transport=udp"},
			"username":   "u",
			"credential": "c",
		},
		"expires_in": 3600,
	})

	creds, err := FetchTurnCredentials(context.Background(), "test-hw-id")
	if err != nil {
		t.Fatalf("FetchTurnCredentials: %v", err)
	}
	if creds.Username != "u" || creds.Credential != "c" || creds.ExpiresIn != 3600 {
		t.Fatalf("unexpected creds: %+v", creds)
	}
	if len(creds.URLs) != 1 || creds.URLs[0] != "turn:turn.cloudflare.com:3478?transport=udp" {
		t.Fatalf("unexpected urls: %+v", creds.URLs)
	}
}

func TestFetchTurnCredentials_NotPro(t *testing.T) {
	turnCredentialsServer(t, http.StatusForbidden, map[string]any{"error": "not_pro"})

	_, err := FetchTurnCredentials(context.Background(), "test-hw-id")
	refused, ok := err.(*TurnCredentialsRefused)
	if !ok {
		t.Fatalf("err = %v (%T), want *TurnCredentialsRefused", err, err)
	}
	if refused.Reason != "not_pro" {
		t.Fatalf("refused.Reason = %q, want not_pro", refused.Reason)
	}
}

func TestFetchTurnCredentials_RateLimited(t *testing.T) {
	turnCredentialsServer(t, http.StatusTooManyRequests, map[string]any{"error": "rate_limited"})

	_, err := FetchTurnCredentials(context.Background(), "test-hw-id")
	refused, ok := err.(*TurnCredentialsRefused)
	if !ok {
		t.Fatalf("err = %v (%T), want *TurnCredentialsRefused", err, err)
	}
	if refused.Reason != "rate_limited" {
		t.Fatalf("refused.Reason = %q, want rate_limited", refused.Reason)
	}
}

func TestFetchTurnCredentials_UpstreamError(t *testing.T) {
	turnCredentialsServer(t, http.StatusBadGateway, map[string]any{"error": "upstream_error"})

	_, err := FetchTurnCredentials(context.Background(), "test-hw-id")
	if err == nil {
		t.Fatal("expected an error")
	}
	if _, ok := err.(*TurnCredentialsRefused); ok {
		t.Fatalf("502 should not be a *TurnCredentialsRefused (that type is only for 403/429): %v", err)
	}
}

func TestWriteTurnCredentialsFile_RoundTrips(t *testing.T) {
	stateDir := t.TempDir()
	creds := &TurnCredentials{URLs: []string{"turn:turn.cloudflare.com:3478?transport=udp"}, Username: "u", Credential: "c", ExpiresIn: 3600}
	fetchedAt := time.Unix(1_700_000_000, 0)

	if err := WriteTurnCredentialsFile(stateDir, creds, fetchedAt); err != nil {
		t.Fatalf("WriteTurnCredentialsFile: %v", err)
	}

	raw, err := os.ReadFile(TurnCredentialsFilePath(stateDir))
	if err != nil {
		t.Fatalf("reading written file: %v", err)
	}
	var got turnCredentialsFileBody
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got.URLs, creds.URLs) || got.Username != "u" || got.Credential != "c" || got.FetchedAt != 1_700_000_000 || got.ExpiresIn != 3600 {
		t.Fatalf("got %+v", got)
	}
}

func TestTurnCredentialsFilePath_MatchesRustshineDir(t *testing.T) {
	got := TurnCredentialsFilePath("/state")
	want := filepath.Join("/state", "rustshine", "turn-credentials.json")
	if got != want {
		t.Fatalf("TurnCredentialsFilePath = %q, want %q", got, want)
	}
}

func TestClearTurnCredentialsFile_RemovesExistingFile(t *testing.T) {
	stateDir := t.TempDir()
	creds := &TurnCredentials{URLs: []string{"turn:x"}, Username: "u", Credential: "c", ExpiresIn: 3600}
	if err := WriteTurnCredentialsFile(stateDir, creds, time.Now()); err != nil {
		t.Fatalf("WriteTurnCredentialsFile: %v", err)
	}

	if err := ClearTurnCredentialsFile(stateDir); err != nil {
		t.Fatalf("ClearTurnCredentialsFile: %v", err)
	}
	if _, err := os.Stat(TurnCredentialsFilePath(stateDir)); !os.IsNotExist(err) {
		t.Fatalf("expected file to be gone, stat err = %v", err)
	}
}

func TestClearTurnCredentialsFile_NoOpWhenAlreadyAbsent(t *testing.T) {
	stateDir := t.TempDir()
	if err := ClearTurnCredentialsFile(stateDir); err != nil {
		t.Fatalf("ClearTurnCredentialsFile on an absent file should not error: %v", err)
	}
}
