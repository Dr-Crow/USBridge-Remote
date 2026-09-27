package streamhost

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSunshine stands in for Sunshine's HTTPS admin API (/api/pin only),
// validating POST /api/pin the way Sunshine 2026.927's confighttp.cpp
// savePin does. pairings(n) is what GET /api/pin lists on its n-th call
// (1-based); nil means the build has no GET /api/pin at all (older
// Sunshine). pairingName is the device name each listed request carries.
// rejectPIN makes nvhttp::pin's result false (a wrong PIN).
type fakeSunshine struct {
	pairings    func(call int) []string
	pairingName string
	rejectPIN   bool

	mu       sync.Mutex
	gets     int
	posted   []map[string]string
	postAuth bool
}

func (f *fakeSunshine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/pin" {
		http.NotFound(w, r) // includes /api/csrf-token: SubmitPIN tolerates that
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Method {
	case http.MethodGet:
		if f.pairings == nil {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		f.gets++
		var list []map[string]string
		for _, id := range f.pairings(f.gets) {
			list = append(list, map[string]string{"id": id, "name": f.pairingName, "address": "192.168.1.5"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"pairings": list})
	case http.MethodPost:
		user, _, ok := r.BasicAuth()
		f.postAuth = ok && user == sunshineAdminUser
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.posted = append(f.posted, body)
		if f.pairings != nil {
			if msg := savePinValidation(body); msg != "" {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": msg, "status": false})
				return
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": !f.rejectPIN})
	}
}

// savePinValidation is Sunshine 2026.927's savePin input check.
func savePinValidation(body map[string]string) string {
	id := body["pairing_id"]
	if len(id) != 32 || strings.Trim(id, "0123456789abcdefABCDEF") != "" {
		return "pairing_id must contain exactly 32 hexadecimal characters"
	}
	if pin := body["pin"]; len(pin) != 4 || strings.Trim(pin, "0123456789") != "" {
		return "PIN must contain exactly 4 numeric digits"
	}
	if name := body["name"]; name == "" || len(name) > 128 {
		return "Client name must contain between 1 and 128 bytes"
	}
	return ""
}

func startFakeSunshine(t *testing.T, f *fakeSunshine) int {
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

var (
	pairingA = strings.Repeat("a", 32)
	pairingB = strings.Repeat("b", 32)
)

func TestSubmitPINSendsNewestPendingPairingID(t *testing.T) {
	f := &fakeSunshine{pairings: func(int) []string { return []string{pairingA, pairingB} }, pairingName: "Living room PC"}
	port := startFakeSunshine(t, f)

	if err := (&sunshineBackend{}).SubmitPIN(port, "1234"); err != nil {
		t.Fatalf("SubmitPIN: %v", err)
	}
	if len(f.posted) != 1 {
		t.Fatalf("posted %d times, want 1", len(f.posted))
	}
	if got := f.posted[0]; got["pin"] != "1234" || got["pairing_id"] != pairingB || got["name"] != "Living room PC" {
		t.Fatalf("posted %v, want pin 1234, the newest pairing_id %s and the request's device name", got, pairingB)
	}
	if !f.postAuth {
		t.Fatal("POST /api/pin was sent without the admin basic auth")
	}
}

func TestSubmitPINWaitsForThePairingRequestToAppear(t *testing.T) {
	f := &fakeSunshine{pairings: func(call int) []string {
		if call < 3 {
			return nil // Moonlight's /pair hasn't reached Sunshine yet
		}
		return []string{pairingA}
	}}
	port := startFakeSunshine(t, f)

	if err := (&sunshineBackend{}).SubmitPIN(port, "0042"); err != nil {
		t.Fatalf("SubmitPIN: %v", err)
	}
	if f.gets < 3 {
		t.Fatalf("GET /api/pin called %d times, want it polled until the request appeared", f.gets)
	}
	if got := f.posted[0]["pairing_id"]; got != pairingA {
		t.Fatalf("pairing_id = %q, want %q", got, pairingA)
	}
}

func TestSubmitPINFailsWhenNoPairingRequestShowsUp(t *testing.T) {
	old := pendingPairingWait
	pendingPairingWait = 300 * time.Millisecond
	t.Cleanup(func() { pendingPairingWait = old })

	f := &fakeSunshine{pairings: func(int) []string { return nil }}
	port := startFakeSunshine(t, f)

	err := (&sunshineBackend{}).SubmitPIN(port, "1234")
	if err == nil || !strings.Contains(err.Error(), "no pending pairing request") {
		t.Fatalf("SubmitPIN error = %v, want the no-pending-request error", err)
	}
	if len(f.posted) != 0 {
		t.Fatalf("PIN was posted %d times without a pairing request to attach it to", len(f.posted))
	}
}

func TestSubmitPINOnOlderSunshineSendsPINAlone(t *testing.T) {
	f := &fakeSunshine{} // no GET /api/pin
	port := startFakeSunshine(t, f)

	if err := (&sunshineBackend{}).SubmitPIN(port, "1234"); err != nil {
		t.Fatalf("SubmitPIN: %v", err)
	}
	if got := f.posted[0]; got["pin"] != "1234" || got["pairing_id"] != "" {
		t.Fatalf("posted %v, want the PIN alone", got)
	}
}

func TestSubmitPINNamesTheClientWhenTheRequestHasNoDeviceName(t *testing.T) {
	f := &fakeSunshine{pairings: func(int) []string { return []string{pairingA} }} // devicename ""
	port := startFakeSunshine(t, f)

	if err := (&sunshineBackend{}).SubmitPIN(port, "1234"); err != nil {
		t.Fatalf("SubmitPIN: %v", err)
	}
	if got := f.posted[0]["name"]; got != defaultPairingClientName {
		t.Fatalf("name = %q, want %q", got, defaultPairingClientName)
	}
}

func TestSubmitPINReportsARejectedPIN(t *testing.T) {
	f := &fakeSunshine{pairings: func(int) []string { return []string{pairingA} }, pairingName: "pc", rejectPIN: true}
	port := startFakeSunshine(t, f)

	err := (&sunshineBackend{}).SubmitPIN(port, "1234")
	if err == nil || !strings.Contains(err.Error(), "rejected the PIN") {
		t.Fatalf("SubmitPIN error = %v, want the rejected-PIN error", err)
	}
}
