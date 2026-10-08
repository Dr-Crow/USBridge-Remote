package streamhost

import (
	"path/filepath"
	"strings"
)

func rustshineICEArgs(strict bool, stateDir string) []string {
	if strict {
		// Explicit empty value overrides clap's public default. Actual parser and
		// network acceptance must be verified against each pinned runtime binary.
		return []string{"--webrtc-ice-servers="}
	}
	return []string{"--turn-credentials-file", filepath.Join(stateDir, "rustshine", "turn-credentials.json")}
}

func strictStreamerEnv(env []string) []string {
	out := make([]string, 0, len(env)+2)
	for _, v := range env {
		key, _, _ := strings.Cut(v, "=")
		key = strings.ToUpper(key)
		if strings.HasPrefix(key, "USBRIDGE_STREAMER_") && (strings.Contains(key, "ICE") || strings.Contains(key, "TURN")) {
			continue
		}
		out = append(out, v)
	}
	return append(out, "USBRIDGE_STREAMER_WEBRTC_ICE_SERVERS=", "USBRIDGE_STREAMER_TURN_CREDENTIALS_FILE=")
}
