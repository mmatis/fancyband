//go:build darwin || freebsd

package automation

import "golang.org/x/sys/unix"

// peerUID asks the kernel which user owns the process on the other end of the
// socket. Unlike anything the client could send, this cannot be forged.
func peerUID(fd uintptr) (int, error) {
	cred, err := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	if err != nil {
		return -1, err
	}
	return int(cred.Uid), nil
}

const peerVerificationSupported = true
