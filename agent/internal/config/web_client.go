package config

import (
	"errors"
	"strings"
	"usbridge_agent/internal/netpolicy"
)

// ValidateLocalWebClientURL never resolves DNS or accepts credentials/query data.
func ValidateLocalWebClientURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := netpolicy.LocalURL(raw)
	if err != nil {
		return "", err
	}
	if u.RawQuery != "" || u.ForceQuery {
		return "", errors.New("local web URL must not contain a query")
	}
	return u.String(), nil
}

func (c Config) WebClientURL(local bool) string {
	if c.LocalWebClientURL != "" {
		value, err := ValidateLocalWebClientURL(c.LocalWebClientURL)
		if err == nil {
			return value
		}
		return ""
	}
	if local {
		return ""
	}
	return "https://web.usbridge.io"
}
