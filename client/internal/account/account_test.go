package account

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeleteAccount_Success(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/v1/account" {
			t.Errorf("expected /v1/account, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-tok" {
			t.Errorf("expected Bearer test-tok, got %s", r.Header.Get("Authorization"))
		}
		called = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	prev := TestSetBackendBaseURL(srv.URL)
	defer TestSetBackendBaseURL(prev)

	err := DeleteAccount(context.Background(), "test-tok")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("handler was not called")
	}
}

func TestDeleteAccount_ActiveLicensesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"active_licenses","message":"Please cancel your active subscriptions before deleting your account."}`))
	}))
	defer srv.Close()

	prev := TestSetBackendBaseURL(srv.URL)
	defer TestSetBackendBaseURL(prev)

	err := DeleteAccount(context.Background(), "test-tok")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "Please cancel your active subscriptions before deleting your account." {
		t.Fatalf("unexpected error message: %v", err)
	}
}
