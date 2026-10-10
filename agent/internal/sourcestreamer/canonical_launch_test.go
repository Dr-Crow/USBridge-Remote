package sourcestreamer

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestLaunchRejectsNoncanonicalNamesAndKeyEncoding(t *testing.T) {
	raw, err := json.Marshal(validLaunch())
	if err != nil {
		t.Fatal(err)
	}
	for _, replacement := range []string{`"CAPTURE_CONSENT":true`, `"capture_consent":false,"CAPTURE_CONSENT":true`, `"capture_conſent":true`} {
		bad := bytes.Replace(raw, []byte(`"capture_consent":true`), []byte(replacement), 1)
		if _, err := DecodeLaunch(bytes.NewReader(bad)); err == nil {
			t.Fatal("ambiguous capture field accepted")
		}
	}
	for _, separator := range []string{"\r", "\n", "\r\n"} {
		request := validLaunch()
		request.KeyB64 = request.KeyB64[:4] + separator + request.KeyB64[4:]
		if err := request.Validate(); err == nil || strings.Contains(err.Error(), request.KeyB64) {
			t.Fatal("noncanonical key accepted or disclosed")
		}
	}
}
