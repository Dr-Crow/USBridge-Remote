package entitlement

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"usbridge_agent/internal/netpolicy"

	"github.com/gorilla/websocket"
)

// SignalRelayRefused is DialSignalRelay's error type when the backend
// explicitly refused the connect (403/429) -- mirrors TurnCredentialsRefused
// exactly, same "not_pro" vs "rate_limited" distinction for the same reason
// (see that type's doc comment): the caller needs to tell "genuinely not
// entitled right now" apart from "still entitled, just back off and retry".
type SignalRelayRefused struct{ Reason string }

func (e *SignalRelayRefused) Error() string {
	return fmt.Sprintf("entitlement: signal relay connect refused: %s", e.Reason)
}

// DialSignalRelay opens this agent's persistent WebSocket to usbridge-
// entitlement's WebRTC signaling relay (see usbridge-entitlement-backend's
// webrtcSignalRelay.ts) -- pro/enterprise tier only, gated server-side the
// same way FetchTurnCredentials is. The caller
// (app.webrtcSignalRelayWatchdog) owns the connection's whole lifetime:
// read incoming offers, reply with answers, redial on drop.
func DialSignalRelay(ctx context.Context, hwID string) (*websocket.Conn, error) {
	if err := netpolicy.RequireRuntimeOnline("hosted signal relay"); err != nil {
		return nil, err
	}
	wsURL, err := signalRelayURL(hwID)
	if err != nil {
		return nil, err
	}
	conn, resp, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		if resp != nil && (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests) {
			defer resp.Body.Close()
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
			var refusal struct {
				Error string `json:"error"`
			}
			_ = json.Unmarshal(body, &refusal)
			if refusal.Error == "" {
				refusal.Error = fmt.Sprintf("http_%d", resp.StatusCode)
			}
			return nil, &SignalRelayRefused{Reason: refusal.Error}
		}
		return nil, fmt.Errorf("entitlement: dial signal relay: %w", err)
	}
	return conn, nil
}

// signalRelayURL builds the wss:// (or ws:// against a local httptest.Server
// in tests -- see TestSetBackendBaseURL) signal-relay connect URL from
// backendBaseURL: a plain scheme swap plus the fixed path, same host every
// other entitlement call already targets.
func signalRelayURL(hwID string) (string, error) {
	u, err := url.Parse(backendBaseURL)
	if err != nil {
		return "", fmt.Errorf("entitlement: parse backend base URL: %w", err)
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		return "", fmt.Errorf("entitlement: unexpected backend scheme %q", u.Scheme)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/v1/webrtc/signal-relay/connect"
	q := u.Query()
	q.Set("hw_id", hwID)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
