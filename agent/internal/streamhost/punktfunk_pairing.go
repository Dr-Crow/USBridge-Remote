package streamhost

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// punktfunkAdminHTTPClient is shared across every admin-API call instead of
// a fresh &http.Client{} per call -- see sunshineAdminHTTPClient's doc
// comment (sunshine_pairing.go) for why a throwaway Client/Transport per
// call leaks a persistent connection instead of reusing one. Needs its own
// InsecureSkipVerify TLS config: punktfunk-host's management API is HTTPS on
// a self-signed identity certificate, same trust model as Sunshine's admin
// API.
var punktfunkAdminHTTPClient = &http.Client{
	Timeout: 5 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
	},
}

// punktfunkAdminURL builds an https://127.0.0.1:<adminPort>/api/v1<path> URL.
func punktfunkAdminURL(adminPort int, path string) string {
	return fmt.Sprintf("https://%s:%d/api/v1%s", adminHost(), adminPort, path)
}

// punktfunkRequest sends an authenticated request against punktfunk-host's
// management API and returns the raw response -- callers decode the body.
// Confirmed from api/openapi.json: every admin route (everything except
// GET /health) wants "Authorization: Bearer <token>", not HTTP Basic.
func punktfunkRequest(method string, adminPort int, path, token string, body []byte) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, punktfunkAdminURL(adminPort, path), r)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return punktfunkAdminHTTPClient.Do(req)
}

// punktfunkReadErrBody mirrors readErrBody (sunshine_pairing.go): reads and
// truncates a failed admin-API response body so error messages carry
// punktfunk-host's actual ApiError.error message instead of a bare status
// code.
func punktfunkReadErrBody(resp *http.Response) string {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	var apiErr struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(b, &apiErr) == nil && apiErr.Error != "" {
		return apiErr.Error
	}
	msg := string(b)
	if msg == "" {
		msg = resp.Status
	}
	return msg
}

// punktfunkHealthy reports whether punktfunk-host's management API answers
// GET /api/v1/health -- confirmed unauthenticated in api/openapi.json
// ("security": [{}], and mgmt/clients.rs's "Unauthenticated: require_auth
// exempts it" doc comment). Used by WaitReady (punktfunk_backend.go).
func punktfunkHealthy(adminPort int) bool {
	resp, err := punktfunkRequest(http.MethodGet, adminPort, "/health", "", nil)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// punktfunkPairedClient mirrors the fields of punktfunk's PairedClient
// schema (mgmt/clients.rs) this backend actually uses. fingerprint is the
// only stable per-device identifier punktfunk exposes once pairing
// completes (there is no persisted uniqueid) -- see ListClients's doc
// comment for how that maps onto streamhost.Client.
type punktfunkPairedClient struct {
	Fingerprint string  `json:"fingerprint"`
	Subject     *string `json:"subject"`
	Label       *string `json:"label"`
}

// ListClients lists Punktfunk's GameStream-plane paired devices (GET
// /clients). streamhost.Client.UniqueID carries the certificate fingerprint
// here -- Punktfunk's PairedClient has no persisted uniqueid field (only a
// pairing ceremony in flight does, see SubmitPIN), so the fingerprint is the
// only identifier stable enough to use for UnpairClient. Name prefers the
// operator-assigned label, then the certificate subject, then the bare
// fingerprint -- mirrors mgmt/clients.rs's own PairedClient doc comment
// ("every moonlight-common-c client self-signs the same subject; label is
// the field to show").
func (b *punktfunkBackend) ListClients(adminPort int) ([]Client, error) {
	resp, err := punktfunkRequest(http.MethodGet, adminPort, "/clients", b.AdminPass(), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("punktfunk: list clients: %s", punktfunkReadErrBody(resp))
	}
	var raw []punktfunkPairedClient
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	clients := make([]Client, 0, len(raw))
	for _, c := range raw {
		name := c.Fingerprint
		if c.Label != nil && *c.Label != "" {
			name = *c.Label
		} else if c.Subject != nil && *c.Subject != "" {
			name = *c.Subject
		}
		clients = append(clients, Client{Name: name, UniqueID: c.Fingerprint})
	}
	return clients, nil
}

// UnpairClient removes a paired device (DELETE /clients/{fingerprint}).
// uniqueID is actually the certificate fingerprint here -- see ListClients's
// doc comment for why Punktfunk has no separate persisted uniqueid to use
// instead.
func (b *punktfunkBackend) UnpairClient(adminPort int, uniqueID string) error {
	resp, err := punktfunkRequest(http.MethodDelete, adminPort, "/clients/"+uniqueID, b.AdminPass(), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("punktfunk: unpair client: %s", punktfunkReadErrBody(resp))
	}
	return nil
}

type punktfunkPendingCeremony struct {
	UniqueID    string `json:"uniqueid"`
	Fingerprint string `json:"fingerprint"`
	PeerIP      string `json:"peer_ip"`
}

type punktfunkPairingStatus struct {
	PinPending bool                       `json:"pin_pending"`
	Pending    []punktfunkPendingCeremony `json:"pending"`
}

// punktfunkPendingWait mirrors rustshine/sunshine's pendingPairingWait: how
// long SubmitPIN polls GET /pair for a ceremony to appear before giving up,
// for the same reason -- Moonlight's /pair request can arrive at
// punktfunk-host a moment after the operator is already being prompted for
// a PIN by this agent's own UI.
var punktfunkPendingWait = 5 * time.Second

// SubmitPIN completes Punktfunk's GameStream pairing ceremony. Unlike
// Sunshine's "POST the PIN alone" (or rustshine's), punktfunk's POST
// /pair/pin requires the exact ceremony identity (uniqueid, fingerprint,
// peer_ip) from GET /pair's pending[] -- confirmed directly from
// mgmt/clients.rs's submit_pairing_pin. This polls GET /pair for the newest
// pending ceremony (same "poll until it appears, pick the newest" shape as
// sunshineBackend.SubmitPIN against GET /api/pin) and echoes its identity
// back in the submit.
func (b *punktfunkBackend) SubmitPIN(adminPort int, pin string) error {
	token := b.AdminPass()
	deadline := time.Now().Add(punktfunkPendingWait)
	var newest *punktfunkPendingCeremony
	for {
		resp, err := punktfunkRequest(http.MethodGet, adminPort, "/pair", token, nil)
		if err != nil {
			return err
		}
		var status punktfunkPairingStatus
		decErr := json.NewDecoder(resp.Body).Decode(&status)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK && decErr == nil && len(status.Pending) > 0 {
			newest = &status.Pending[len(status.Pending)-1]
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("punktfunk: no pending pairing request")
		}
		time.Sleep(250 * time.Millisecond)
	}

	body, err := json.Marshal(struct {
		Pin         string `json:"pin"`
		UniqueID    string `json:"uniqueid"`
		Fingerprint string `json:"fingerprint"`
		PeerIP      string `json:"peer_ip"`
	}{
		Pin:         pin,
		UniqueID:    newest.UniqueID,
		Fingerprint: newest.Fingerprint,
		PeerIP:      newest.PeerIP,
	})
	if err != nil {
		return err
	}
	resp, err := punktfunkRequest(http.MethodPost, adminPort, "/pair/pin", token, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("punktfunk: submit PIN: %s", punktfunkReadErrBody(resp))
	}
	return nil
}
