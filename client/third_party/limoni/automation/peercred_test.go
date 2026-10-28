package automation

import (
	"errors"
	"strings"
	"testing"
)

func TestAdmitPeerFailsClosed(t *testing.T) {
	unknown := errors.New("cannot tell")
	for _, tc := range []struct {
		name            string
		peer, self      int
		peerErr         error
		allowUnverified bool
		admit           bool
	}{
		{"same user", 1000, 1000, nil, false, true},
		{"different user", 0, 1000, nil, false, false},
		{"different user is refused even when unverified peers are allowed", 1001, 1000, nil, true, false},
		{"unverifiable peer is refused by default", -1, 1000, unknown, false, false},
		{"unverifiable peer admitted only by explicit opt-in", -1, 1000, unknown, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := admitPeer(tc.peer, tc.peerErr, tc.self, tc.allowUnverified)
			if tc.admit && err != nil {
				t.Errorf("refused: %v", err)
			}
			if !tc.admit && err == nil {
				t.Error("admitted")
			}
		})
	}
}

// Where the kernel can report the peer, the real check must admit this
// process's own user without any opt-in. Where it cannot, the default policy
// must refuse the connection outright.
func TestPeerVerificationOnThisPlatform(t *testing.T) {
	server, err := Listen(shortSocketPath(t))
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer server.Close()
	server.Publish(Snapshot{Tree: sampleTree(), Width: 80, Height: 24})

	client, err := Dial(server.Addr())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	_, _, _, helloErr := client.Hello()
	if peerVerificationSupported {
		if helloErr != nil {
			t.Fatalf("own user was refused by the kernel check: %v", helloErr)
		}
		return
	}
	if helloErr == nil || !strings.Contains(helloErr.Error(), "cannot be verified") {
		t.Fatalf("unverifiable platform admitted a connection under the default policy: %v", helloErr)
	}
}
