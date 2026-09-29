package api

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// ErrHostLayoutUnsupported: the agent can't switch its keyboard layout
// (not KDE Wayland, or an agent without /api/keyboard/layout).
var ErrHostLayoutUnsupported = errors.New("host keyboard layout switching unsupported")

// SetHostKeyboardLayout makes the host type with the layout for lang ("ru",
// "en", ...) and returns the host's layout short code.
func (c *USBClient) SetHostKeyboardLayout(lang string) (string, error) {
	payload, _ := json.Marshal(map[string]string{"lang": lang})
	body, err := c.PostRawWithTimeout("/api/keyboard/layout", payload, 3*time.Second)
	if err != nil && (strings.Contains(err.Error(), "HTTP error 501") || strings.Contains(err.Error(), "HTTP error 404")) {
		return "", ErrHostLayoutUnsupported
	}
	var out struct {
		Layout string `json:"layout"`
	}
	if err := decodeAgentResponse(body, err, &out); err != nil {
		return "", err
	}
	return out.Layout, nil
}
