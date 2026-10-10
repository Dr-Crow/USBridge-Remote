package componentjson

import "testing"

func TestRejectsAmbiguousObjects(t *testing.T) {
	var value struct {
		A      int            `json:"a"`
		Nested map[string]int `json:"nested"`
	}
	for _, raw := range []string{`{"a":1,"a":2}`, `{"nested":{"x":1,"x":2}}`, `{"a":1} {}`, `[]`, `null`, `{"unknown":1}`, `{"a":1,}`, `{"nested":[1]}`} {
		if Decode([]byte(raw), &value) == nil {
			t.Fatalf("invalid JSON accepted: %s", raw)
		}
	}
	if err := Decode([]byte(`{"a":1,"nested":{"x":2}}`), &value); err != nil {
		t.Fatal(err)
	}
}

func TestRejectsCaseAndUnicodeFoldAliases(t *testing.T) {
	type nested struct {
		Consent bool `json:"consent"`
	}
	for _, raw := range []string{
		`{"capture_consent":false,"CAPTURE_CONSENT":true}`,
		`{"CAPTURE_CONSENT":true}`,
		`{"capture_conſent":true}`,
		`{"schema_verſion":1}`,
		`{"nested":{"CONSENT":true}}`,
		`{"items":[{"Consent":true}]}`,
		`{"by_name":{"allowed":{"conſent":true}}}`,
	} {
		var out struct {
			CaptureConsent bool              `json:"capture_consent"`
			SchemaVersion  int               `json:"schema_version"`
			Nested         *nested           `json:"nested"`
			Items          []nested          `json:"items"`
			ByName         map[string]nested `json:"by_name"`
		}
		if Decode([]byte(raw), &out) == nil {
			t.Fatalf("noncanonical typed field accepted: %s", raw)
		}
	}
}

func TestExactFieldsRetainTypedCollections(t *testing.T) {
	type child struct {
		Consent bool `json:"consent"`
	}
	var out struct {
		Nested *child           `json:"nested"`
		Items  []child          `json:"items"`
		ByName map[string]child `json:"by_name"`
	}
	if err := Decode([]byte(`{"nested":{"consent":true},"items":[{"consent":false}],"by_name":{"arbitrary-key":{"consent":true}}}`), &out); err != nil {
		t.Fatal(err)
	}
	if out.Nested == nil || !out.Nested.Consent || len(out.Items) != 1 || out.Items[0].Consent || !out.ByName["arbitrary-key"].Consent {
		t.Fatal("canonical typed fields changed semantics")
	}
}
