package adminapi

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

type runtimeBackend struct {
	TokenBackend
	saved bool
	calls int
	err   error
}

func (b *runtimeBackend) SetLocalRuntimeEnabled(v bool) error {
	b.calls++
	if b.err != nil {
		return b.err
	}
	b.saved = v
	return nil
}
func TestLocalRuntimeAdminSetting(t *testing.T) {
	for _, tc := range []struct {
		body      string
		fail      bool
		wantCalls int
		wantCode  int
	}{
		{`{"value":true}`, false, 1, 200}, {`{"value":false}`, false, 1, 200},
		{`invalid`, false, 0, 500}, {`{"value":true}`, true, 1, 500},
	} {
		b := &runtimeBackend{}
		if tc.fail {
			b.err = errors.New("save failed")
		}
		s := &Server{token: b}
		w := httptest.NewRecorder()
		s.handleSetLocalRuntime(w, httptest.NewRequest("POST", "/token/local-runtime", strings.NewReader(tc.body)))
		if b.calls != tc.wantCalls || w.Code != tc.wantCode {
			t.Fatalf("%+v: calls=%d code=%d", tc, b.calls, w.Code)
		}
		if tc.fail && b.saved {
			t.Fatal("failed save retained preference")
		}
	}
}

type webClientBackend struct {
	TokenBackend
	value string
}

func (b *webClientBackend) SetLocalWebClientURL(v string) error { b.value = v; return nil }
func TestLocalWebClientAdminSetting(t *testing.T) {
	b := &webClientBackend{}
	s := &Server{token: b}
	w := httptest.NewRecorder()
	s.handleSetLocalWebClient(w, httptest.NewRequest("POST", "/token/local-web-client", strings.NewReader(`{"value":"https://192.168.1.20/"}`)))
	if w.Code != 200 || b.value != "https://192.168.1.20/" {
		t.Fatal("local web setting did not reach backend")
	}
}
