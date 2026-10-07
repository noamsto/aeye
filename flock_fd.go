//go:build !windows

package main

import (
	"errors"
	"fmt"
	"io"

	"golang.org/x/sys/unix"
)

// runFlockFD locks an fd inherited from the calling shell. flock(2) locks
// belong to the open file description, so the lock outlives this process and
// stays held by the shell's fd — the same contract as util-linux `flock 9`,
// which macOS lacks. Returns the exit code: 0 locked, 1 held elsewhere (with
// nonBlock), 2 any other runtime failure.
func runFlockFD(fd int, nonBlock bool, stderr io.Writer) int {
	how := unix.LOCK_EX
	if nonBlock {
		how |= unix.LOCK_NB
	}
	for {
		err := unix.Flock(fd, how)
		switch {
		case err == nil:
			return 0
		case errors.Is(err, unix.EINTR):
		case nonBlock && errors.Is(err, unix.EWOULDBLOCK):
			return 1
		default:
			_, _ = fmt.Fprintf(stderr, "aeye flock-fd: fd %d: %v\n", fd, err)
			return 2
		}
	}
}
