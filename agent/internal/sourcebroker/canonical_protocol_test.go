package sourcebroker

import (
	"testing"

	"usbridge_agent/internal/componentjson"
)

func TestBrokerRejectsFoldedConsentAndEventFields(t *testing.T) {
	for _, raw := range []string{`{"consent":false,"CONSENT":true}`, `{"conſent":true}`} {
		var launch Launch
		if err := componentjson.Decode([]byte(raw), &launch); err == nil {
			t.Fatal("noncanonical consent field accepted")
		}
	}
	for _, raw := range []string{`{"event":"ready","os_attached":true,"OS_ATTACHED":false}`, `{"device":{"BUS_ID":"1-2"}}`} {
		var event Event
		if err := componentjson.Decode([]byte(raw), &event); err == nil {
			t.Fatal("noncanonical broker event field accepted")
		}
	}
}

func TestBrokerRequiresCanonicalTransferData(t *testing.T) {
	for _, data := range []string{"YQ==", "Y\rQ==", "Y\nQ==", "Y\r\nQ==", "YR=="} {
		command := Command{Op: "transfer", ID: "fixture", Length: 1, Data: data}
		event := Event{Event: "complete", ID: "fixture", ActualLength: 1, Data: data}
		wantValid := data == "YQ=="
		if (command.Validate() == nil) != wantValid {
			t.Fatal("unexpected command base64 acceptance")
		}
		if (event.validateStatus() == nil) != wantValid {
			t.Fatal("unexpected completion base64 acceptance")
		}
	}
}
