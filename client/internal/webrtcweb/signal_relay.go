// Platform-independent half of postOffer's WebRTC signaling-relay fallback
// (client_wasm.go, //go:build js && wasm) -- deliberately kept free of
// syscall/js so it compiles and unit-tests on every platform, the same
// "extract the decision logic, keep the platform glue thin" split this
// session already applied to api's webrtcAPITransport and the agent's own
// signal-relay dialer.
package webrtcweb

import (
	"encoding/json"
	"fmt"
	"strings"
)

// signalRelayBackendBaseURL is usbridge-entitlement's deployed URL -- same
// hardcoded address every other client-side package that talks to this
// backend already uses (see internal/account/account.go's own
// backendBaseURL, and agent/internal/entitlement/pubkey.go's identical
// duplication of the same URL on the agent side); this codebase's
// established convention is a small per-package var, not a shared
// exported one. A var, not a const, purely so tests can point it at a
// local httptest.Server for the duration of one test.
var signalRelayBackendBaseURL = "https://usbridge-entitlement.fatkulinamir80.workers.dev"

// TestSetSignalRelayBackendBaseURL points every signal-relay call at url,
// returning the previous value to restore -- same pattern as
// entitlement.TestSetBackendBaseURL (agent) and
// account.TestSetBackendBaseURL (client). Not meant to be called from
// production code; nothing in this repo does.
func TestSetSignalRelayBackendBaseURL(url string) string {
	prev := signalRelayBackendBaseURL
	signalRelayBackendBaseURL = url
	return prev
}

func signalRelayOfferURL() string {
	return strings.TrimRight(signalRelayBackendBaseURL, "/") + "/v1/webrtc/signal-relay/offer"
}

// buildRelayOfferBody builds the JSON body for a POST to the signal
// relay's /v1/webrtc/signal-relay/offer -- the same sdp/bitrate_kbps/codec
// fields postOffer already sends directly to the agent (see that
// function's own "bitrate_kbps omitted when unset" comment for why 0/""
// are dropped rather than sent literally), plus hw_id to address the
// right agent's Durable Object instance (see usbridge-entitlement-
// backend's webrtcSignalRelay.ts).
func buildRelayOfferBody(hwID, offerSDP string, bitrateKbps int, videoCodec string) ([]byte, error) {
	fields := map[string]any{"hw_id": hwID, "sdp": offerSDP}
	if bitrateKbps > 0 {
		fields["bitrate_kbps"] = bitrateKbps
	}
	if videoCodec != "" {
		fields["codec"] = videoCodec
	}
	return json.Marshal(fields)
}

// offerHTTPError is postOffer's error type once a real HTTP response came
// back -- from either the agent directly or the relay -- just not a 2xx.
// See shouldFallbackToRelay's doc comment for why distinguishing this from
// a network-level failure matters.
type offerHTTPError struct {
	Status int
	Body   string
}

func (e *offerHTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.Status, e.Body)
}

// shouldFallbackToRelay reports whether postOffer should retry against the
// signaling relay after a direct fetch() failure. Only a genuine network-
// level failure (fetch() itself rejected -- connection refused, DNS
// failure, mixed content or Local Network Access blocking the request: the
// agent was never actually reached) is eligible. An *offerHTTPError (a real
// response came back, just not 2xx -- e.g. a 401 from a wrong master key)
// means the agent WAS reached and rejected the request on its own terms;
// retrying the identical request through a different transport can't
// change that outcome, it would just mask the real error behind a second,
// unrelated one. hwID == "" (no relay address known -- see
// SavedConnection.HwID's doc comment on when that happens: a manual entry,
// or a QR/deep-link "Connect now" that was never saved) also short-
// circuits to false: there is nowhere to retry to.
func shouldFallbackToRelay(err error, hwID string) bool {
	if hwID == "" || err == nil {
		return false
	}
	_, isHTTPError := err.(*offerHTTPError)
	return !isHTTPError
}

// parseOfferAnswer extracts the "sdp" field from a /webrtc/offer-shaped
// JSON response body -- shared by both the direct and relay paths, which
// return the exact same {"sdp": "..."} shape (usbridge-entitlement-
// backend's webrtcSignalRelay.ts mirrors rustshine's own OfferResponse
// verbatim, see that file's own doc comment).
func parseOfferAnswer(body []byte) (string, error) {
	var parsed struct {
		SDP string `json:"sdp"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("webrtc: decoding offer answer: %w", err)
	}
	if parsed.SDP == "" {
		return "", fmt.Errorf("webrtc: offer answer had no sdp")
	}
	return parsed.SDP, nil
}
