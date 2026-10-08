package update

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

type rejectNetworkTransport struct{ t *testing.T }

func (r rejectNetworkTransport) RoundTrip(*http.Request) (*http.Response, error) {
	r.t.Fatal("manual update channel attempted a network request")
	return nil, nil
}

func TestManualChannelDoesNotFetchOrApply(t *testing.T) {
	oldChannel, oldTransport := Channel, http.DefaultTransport
	Channel = "manual"
	http.DefaultTransport = rejectNetworkTransport{t}
	t.Cleanup(func() { Channel = oldChannel; http.DefaultTransport = oldTransport })
	if got := Check(context.Background(), "0.0.0"); got != nil {
		t.Fatal("manual channel returned an update")
	}
	if err := DownloadAndApply(context.Background(), nil, nil); err == nil || !strings.Contains(err.Error(), "manual") {
		t.Fatalf("expected manual-update rejection, got %v", err)
	}
}
