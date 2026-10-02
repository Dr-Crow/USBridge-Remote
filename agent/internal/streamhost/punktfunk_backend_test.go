package streamhost

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"
)

// fakePunktfunk stands in for punktfunk-host's management API, validating
// the exact wire shapes confirmed from api/openapi.json: bearer-token auth
// (not Basic), GET /clients, GET /pair + POST /pair/pin with the full
// ceremony identity, DELETE /clients/{fingerprint}, GET /status and
// GET /health. mu guards every field ServeHTTP reads: TestPunktfunkSubmitPINWaitsForTheCeremonyToAppear
// mutates `pending` from a second goroutine while SubmitPIN's poll loop is
// concurrently hitting GET /pair.
type fakePunktfunk struct {
	token string // "" means don't check auth

	mu      sync.Mutex
	clients []punktfunkPairedClient
	pending []punktfunkPendingCeremony
	posted  *struct {
		Pin         string `json:"pin"`
		UniqueID    string `json:"uniqueid"`
		Fingerprint string `json:"fingerprint"`
		PeerIP      string `json:"peer_ip"`
	}
	unpaired   string
	activeSess int
}

func (f *fakePunktfunk) setPending(p []punktfunkPendingCeremony) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pending = p
}

func (f *fakePunktfunk) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/v1/health" {
		if f.token != "" {
			got := r.Header.Get("Authorization")
			if got != "Bearer "+f.token {
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing or invalid bearer token"})
				return
			}
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.URL.Path == "/api/v1/health" && r.Method == http.MethodGet:
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "version": "test", "abi_version": 1})
	case r.URL.Path == "/api/v1/clients" && r.Method == http.MethodGet:
		_ = json.NewEncoder(w).Encode(f.clients)
	case r.URL.Path == "/api/v1/pair" && r.Method == http.MethodGet:
		_ = json.NewEncoder(w).Encode(punktfunkPairingStatus{PinPending: len(f.pending) > 0, Pending: f.pending})
	case r.URL.Path == "/api/v1/pair/pin" && r.Method == http.MethodPost:
		var body struct {
			Pin         string `json:"pin"`
			UniqueID    string `json:"uniqueid"`
			Fingerprint string `json:"fingerprint"`
			PeerIP      string `json:"peer_ip"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.posted = &body
		w.WriteHeader(http.StatusNoContent)
	case r.URL.Path == "/api/v1/status" && r.Method == http.MethodGet:
		_ = json.NewEncoder(w).Encode(punktfunkRuntimeStatus{ActiveSessions: f.activeSess})
	case len(r.URL.Path) > len("/api/v1/clients/") && r.URL.Path[:len("/api/v1/clients/")] == "/api/v1/clients/" && r.Method == http.MethodDelete:
		f.unpaired = r.URL.Path[len("/api/v1/clients/"):]
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func startFakePunktfunk(t *testing.T, f *fakePunktfunk) int {
	t.Helper()
	srv := httptest.NewTLSServer(f)
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func TestPunktfunkListClientsMapsFingerprintAsUniqueID(t *testing.T) {
	label := "Living Room TV"
	f := &fakePunktfunk{
		token: "tok",
		clients: []punktfunkPairedClient{
			{Fingerprint: "abc123", Label: &label},
			{Fingerprint: "def456"},
		},
	}
	port := startFakePunktfunk(t, f)
	b := &punktfunkBackend{token: "tok"}

	clients, err := b.ListClients(port)
	if err != nil {
		t.Fatalf("ListClients: %v", err)
	}
	if len(clients) != 2 {
		t.Fatalf("got %d clients, want 2", len(clients))
	}
	if clients[0].UniqueID != "abc123" || clients[0].Name != "Living Room TV" {
		t.Errorf("clients[0] = %+v, want fingerprint abc123 named by label", clients[0])
	}
	if clients[1].UniqueID != "def456" || clients[1].Name != "def456" {
		t.Errorf("clients[1] = %+v, want unlabeled client named by its own fingerprint", clients[1])
	}
}

func TestPunktfunkSubmitPINEchoesTheCeremonyIdentity(t *testing.T) {
	f := &fakePunktfunk{
		token: "tok",
		pending: []punktfunkPendingCeremony{
			{UniqueID: "0123456789ABCDEF", Fingerprint: "9f86d0", PeerIP: "192.168.1.42"},
		},
	}
	port := startFakePunktfunk(t, f)
	b := &punktfunkBackend{token: "tok"}

	if err := b.SubmitPIN(port, "1234"); err != nil {
		t.Fatalf("SubmitPIN: %v", err)
	}
	if f.posted == nil {
		t.Fatal("POST /pair/pin was never called")
	}
	if f.posted.Pin != "1234" || f.posted.UniqueID != "0123456789ABCDEF" ||
		f.posted.Fingerprint != "9f86d0" || f.posted.PeerIP != "192.168.1.42" {
		t.Errorf("posted %+v, want the PIN plus the full pending ceremony identity", f.posted)
	}
}

func TestPunktfunkSubmitPINWaitsForTheCeremonyToAppear(t *testing.T) {
	old := punktfunkPendingWait
	punktfunkPendingWait = 2 * time.Second
	t.Cleanup(func() { punktfunkPendingWait = old })

	f := &fakePunktfunk{token: "tok"}
	port := startFakePunktfunk(t, f)
	b := &punktfunkBackend{token: "tok"}

	// Flip the ceremony into existence shortly after SubmitPIN starts polling.
	go func() {
		time.Sleep(50 * time.Millisecond)
		f.setPending([]punktfunkPendingCeremony{{UniqueID: "u", Fingerprint: "fp", PeerIP: "1.2.3.4"}})
	}()

	if err := b.SubmitPIN(port, "0042"); err != nil {
		t.Fatalf("SubmitPIN: %v", err)
	}
	if f.posted == nil || f.posted.UniqueID != "u" {
		t.Fatalf("posted = %+v, want the ceremony that appeared mid-poll", f.posted)
	}
}

func TestPunktfunkSubmitPINFailsWhenNoCeremonyShowsUp(t *testing.T) {
	old := punktfunkPendingWait
	punktfunkPendingWait = 300 * time.Millisecond
	t.Cleanup(func() { punktfunkPendingWait = old })

	f := &fakePunktfunk{token: "tok"}
	port := startFakePunktfunk(t, f)
	b := &punktfunkBackend{token: "tok"}

	err := b.SubmitPIN(port, "1234")
	if err == nil {
		t.Fatal("SubmitPIN: want an error when no ceremony ever appears")
	}
}

func TestPunktfunkUnpairClientUsesFingerprintAsUniqueID(t *testing.T) {
	f := &fakePunktfunk{token: "tok"}
	port := startFakePunktfunk(t, f)
	b := &punktfunkBackend{token: "tok"}

	if err := b.UnpairClient(port, "abc123"); err != nil {
		t.Fatalf("UnpairClient: %v", err)
	}
	if f.unpaired != "abc123" {
		t.Errorf("unpaired = %q, want abc123", f.unpaired)
	}
}

func TestPunktfunkSessionActiveReadsRuntimeStatus(t *testing.T) {
	f := &fakePunktfunk{token: "tok", activeSess: 1}
	port := startFakePunktfunk(t, f)
	b := &punktfunkBackend{token: "tok", adminPort: port}

	if !b.SessionActive() {
		t.Error("SessionActive() = false with active_sessions:1, want true")
	}

	f.activeSess = 0
	if b.SessionActive() {
		t.Error("SessionActive() = true with active_sessions:0, want false")
	}
}

func TestPunktfunkWaitReadyPollsHealth(t *testing.T) {
	f := &fakePunktfunk{token: "tok"}
	port := startFakePunktfunk(t, f)
	b := &punktfunkBackend{}

	if !b.WaitReady(port, 2*time.Second) {
		t.Error("WaitReady() = false against a live /health endpoint, want true")
	}
}

func TestPunktfunkRejectsAWrongBearerToken(t *testing.T) {
	f := &fakePunktfunk{token: "correct-token"}
	port := startFakePunktfunk(t, f)
	b := &punktfunkBackend{token: "wrong-token"}

	if _, err := b.ListClients(port); err == nil {
		t.Error("ListClients with a wrong bearer token: want an error, got nil")
	}
}
