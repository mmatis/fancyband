//go:build !linux && !darwin && !freebsd

package automation

import "errors"

var errPeerVerificationUnsupported = errors.New("automation: this platform cannot report the connecting process's user")

// peerUID is unavailable here. The server refuses connections it cannot
// verify unless the policy explicitly accepts unverified peers.
func peerUID(uintptr) (int, error) {
	return -1, errPeerVerificationUnsupported
}

const peerVerificationSupported = false
