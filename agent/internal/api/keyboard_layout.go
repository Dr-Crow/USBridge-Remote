package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"usbridge_agent/internal/input"
)

// keyboardLayout is GET/POST /api/keyboard/layout: read or switch the
// host's active keyboard layout (KDE Wayland only, see
// input/layout_linux.go). POST {"lang":"ru"} or {"lang":"en"}. Returns
// 501 where the host can't do it, so the client can fall back.
func (s *Server) keyboardLayout(w http.ResponseWriter, r *http.Request) {
	var (
		layout string
		err    error
	)
	if r.Method == http.MethodPost {
		var req struct {
			Lang string `json:"lang"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.fail(w, http.StatusBadRequest, "invalid_json", err)
			return
		}
		layout, err = input.SetKeyboardLayout(req.Lang)
	} else {
		layout, err = input.CurrentKeyboardLayout()
	}
	switch {
	case errors.Is(err, input.ErrLayoutUnsupported):
		s.fail(w, http.StatusNotImplemented, "keyboard_layout_unsupported", err)
	case errors.Is(err, input.ErrLayoutNotConfigured):
		s.fail(w, http.StatusNotFound, "keyboard_layout_not_configured", err)
	case err != nil:
		s.fail(w, http.StatusInternalServerError, "keyboard_layout_failed", err)
	default:
		s.ok(w, "keyboard_layout", map[string]string{"layout": layout})
	}
}
