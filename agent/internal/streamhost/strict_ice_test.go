package streamhost

import (
	"strings"
	"testing"
)

func TestStrictICEExcludesCachedTURN(t *testing.T) {
	args := strings.Join(rustshineICEArgs(true, "/state"), " ")
	if args != "--webrtc-ice-servers=" || strings.Contains(args, "turn-credentials") {
		t.Fatal(args)
	}
	normal := strings.Join(rustshineICEArgs(false, "/state"), " ")
	if !strings.Contains(normal, "/state/rustshine/turn-credentials.json") {
		t.Fatal(normal)
	}
	env := strings.Join(strictStreamerEnv([]string{"PATH=/bin", "USBRIDGE_STREAMER_WEBRTC_ICE_SERVERS=stun:public", "USBRIDGE_STREAMER_TURN_CREDENTIALS_FILE=/cached"}), "\n")
	if strings.Contains(env, "stun:public") || strings.Contains(env, "/cached") || !strings.Contains(env, "PATH=/bin") {
		t.Fatal("unsafe environment")
	}
}
