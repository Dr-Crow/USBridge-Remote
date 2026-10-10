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
