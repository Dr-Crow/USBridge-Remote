// Package previeweligibility observes process eligibility for a future local
// Windows preview. It never grants capture consent or changes a token/session.
package previeweligibility

// Snapshot contains only bounded non-identifying facts. A visible window
// station is an interactive-session prerequisite, not proof of visible pixels.
type Snapshot struct {
	TokenQueried         bool `json:"token_queried"`
	NotElevated          bool `json:"not_elevated"`
	InteractiveSession   bool `json:"interactive_session"`
	TokenClosed          bool `json:"token_closed"`
	WindowStationQueried bool `json:"window_station_queried"`
	WindowStationVisible bool `json:"window_station_visible"`
}

func (s Snapshot) Eligible() bool {
	return s.TokenQueried && s.NotElevated && s.InteractiveSession && s.TokenClosed && s.WindowStationQueried && s.WindowStationVisible
}
