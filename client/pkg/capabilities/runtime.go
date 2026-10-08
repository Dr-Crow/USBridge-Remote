// Package capabilities defines optional agent runtime metadata shared by source
// clients and agents. It does not issue licenses or attest physical hardware.
package capabilities

type RuntimeStatus struct {
	Mode             string `json:"mode"`
	Backend          string `json:"backend"`
	StreamerPrepared bool   `json:"streamer_prepared"`
	USBPrepared      bool   `json:"usb_prepared"`
}

// EffectiveProtocol gives the prepared research runtime its own label rather
// than pretending it changes the vendor tariff. Codec/device availability is
// still taken from the separate live backend capability responses.
func (r *RuntimeStatus) EffectiveProtocol(vendor string) string {
	if r != nil && r.Mode == "local-research" && r.Backend == "rustshine" && r.StreamerPrepared {
		return "local"
	}
	return vendor
}
