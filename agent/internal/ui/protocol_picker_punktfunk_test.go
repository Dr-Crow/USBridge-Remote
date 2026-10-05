package ui

import (
	"testing"

	"usbridge_agent/internal/account"
	"usbridge_agent/internal/entitlement"
)

// Punktfunk has a picker key of its own although it reports the Sunshine
// protocol to clients; with the shared key its tile could never show as the
// active one, and picking Sunshine while it ran would be a no-op.
func TestProtocolKeyFromStatusPunktfunk(t *testing.T) {
	st := entitlement.Status{ActiveBackend: "punktfunk"}
	if got := protocolKeyFromStatus(st); got != protocolPunktfunk {
		t.Errorf("picker key = %q, want %q", got, protocolPunktfunk)
	}
	if got := st.Protocol(); got != protocolOpensource {
		t.Errorf("wire protocol = %q, want %q", got, protocolOpensource)
	}
	if got := protocolKeyFromStatus(entitlement.Status{ActiveBackend: "sunshine"}); got != protocolOpensource {
		t.Errorf("sunshine picker key = %q, want %q", got, protocolOpensource)
	}
	if protocolNeedsPurchase(protocolPunktfunk, st, account.Status{}) {
		t.Error("Punktfunk must not need a purchase")
	}
	if got := chromeForProtocol(protocolPunktfunk).Kind; got != protocolOpensource {
		t.Errorf("chrome kind = %q, want the open-source look", got)
	}
}
