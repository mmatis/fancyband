package session

import "runtime"

// Windows does not map Unix permission bits onto its ACLs.
func runtimeSupportsModeBits() bool { return runtime.GOOS != "windows" }
