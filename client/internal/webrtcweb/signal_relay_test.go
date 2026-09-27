package webrtcweb

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestSignalRelayOfferURL(t *testing.T) {
	prev := TestSetSignalRelayBackendBaseURL("https://usbridge-entitlement.example.test")
	defer TestSetSignalRelayBackendBaseURL(prev)

	got := signalRelayOfferURL()
	want := "https://usbridge-entitlement.example.test/v1/webrtc/signal-relay/offer"
	if got != want {
		t.Errorf("signalRelayOfferURL() = %q, want %q", got, want)
	}
}

func TestSignalRelayOfferURL_TrimsTrailingSlash(t *testing.T) {
	prev := TestSetSignalRelayBackendBaseURL("https://usbridge-entitlement.example.test/")
	defer TestSetSignalRelayBackendBaseURL(prev)

	got := signalRelayOfferURL()
	if strings.Contains(got, "//v1") {
		t.Errorf("signalRelayOfferURL() = %q, has a double slash before the path", got)
	}
}

func TestBuildRelayOfferBody(t *testing.T) {
	body, err := buildRelayOfferBody("hw-123", "v=0 offer sdp", 4000, "h265")
	if err != nil {
		t.Fatalf("buildRelayOfferBody: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["hw_id"] != "hw-123" || got["sdp"] != "v=0 offer sdp" || got["codec"] != "h265" {
		t.Errorf("unexpected body: %v", got)
	}
	if bk, ok := got["bitrate_kbps"].(float64); !ok || bk != 4000 {
		t.Errorf("bitrate_kbps = %v, want 4000", got["bitrate_kbps"])
	}
}

func TestBuildRelayOfferBody_OmitsZeroBitrateAndEmptyCodec(t *testing.T) {
	// Mirrors postOffer's own "bitrate_kbps omitted entirely (not sent as
	// 0) when unset" rule -- a literal 0 asks rustshine to freeze the
	// picture rather than "use your ceiling", so it must never be sent
	// just because Go's zero value happens to be 0.
	body, err := buildRelayOfferBody("hw-123", "v=0", 0, "")
	if err != nil {
		t.Fatalf("buildRelayOfferBody: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, present := got["bitrate_kbps"]; present {
		t.Errorf("bitrate_kbps should be omitted when 0, got %v", got["bitrate_kbps"])
	}
	if _, present := got["codec"]; present {
		t.Errorf("codec should be omitted when empty, got %v", got["codec"])
	}
}

func TestShouldFallbackToRelay(t *testing.T) {
	networkErr := errors.New("connection refused")
	httpErr := &offerHTTPError{Status: 401, Body: "bad signature"}

	cases := []struct {
		name string
		err  error
		hwID string
		want bool
	}{
		{"network failure with a known hw_id falls back", networkErr, "hw-123", true},
		{"network failure with no hw_id known does not fall back (nowhere to go)", networkErr, "", false},
		{"a real HTTP rejection never falls back, even with hw_id known", httpErr, "hw-123", false},
		{"nil error never falls back", nil, "hw-123", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldFallbackToRelay(tc.err, tc.hwID)
			if got != tc.want {
				t.Errorf("shouldFallbackToRelay(%v, %q) = %v, want %v", tc.err, tc.hwID, got, tc.want)
			}
		})
	}
}

func TestParseOfferAnswer(t *testing.T) {
	sdp, err := parseOfferAnswer([]byte(`{"sdp":"v=0 answer"}`))
	if err != nil {
		t.Fatalf("parseOfferAnswer: %v", err)
	}
	if sdp != "v=0 answer" {
		t.Errorf("sdp = %q, want %q", sdp, "v=0 answer")
	}
}

func TestParseOfferAnswer_MissingSDP(t *testing.T) {
	_, err := parseOfferAnswer([]byte(`{}`))
	if err == nil {
		t.Fatal("expected an error for a response with no sdp field")
	}
}

func TestParseOfferAnswer_InvalidJSON(t *testing.T) {
	_, err := parseOfferAnswer([]byte(`not json`))
	if err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
}

func TestOfferHTTPError_ErrorString(t *testing.T) {
	err := &offerHTTPError{Status: 403, Body: `{"error":"not_pro"}`}
	if !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "not_pro") {
		t.Errorf("unexpected error string: %s", err.Error())
	}
}
