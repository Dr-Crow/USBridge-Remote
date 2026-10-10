package sourcestreamer

import "testing"

func TestTypedFrameFailureIsBoundedAndFailedOnly(t *testing.T) {
	for _, tt := range []struct {
		reason, code string
		valid        bool
	}{{"completed", "", true}, {"failed", "", true}, {"failed", "frame_too_large", true}, {"completed", "frame_too_large", false}, {"failed", "arbitrary backend details", false}, {"failed", "FRAME_TOO_LARGE", false}} {
		s := Stopped{SchemaVersion: 1, Event: "stopped", SessionID: "test", Reason: tt.reason, FailureCode: tt.code, Stats: &Stats{}}
		if (s.validate("test") == nil) != tt.valid {
			t.Fatalf("reason=%q code=%q", tt.reason, tt.code)
		}
	}
}
