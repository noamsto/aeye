//go:build linux

package main

import (
	"errors"

	"golang.org/x/sys/unix"
)

// watchOwner watches pid with pidfd_open: the fd becomes readable when the
// process exits and can never refer to a recycled pid, so no start-time
// identity check is needed. Where pidfd is unavailable (an old kernel) it falls
// back to liveness polling.
func watchOwner(pid int) <-chan struct{} {
	gone := make(chan struct{})
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		if errors.Is(err, unix.ESRCH) {
			// Already gone: the owner is not coming back, so fire immediately.
			close(gone)
			return gone
		}
		return pollOwnerProcess(pid)
	}
	go func() {
		defer unix.Close(fd)                                        //nolint:errcheck // close on the watcher goroutine's exit
		pfds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}} //nolint:gosec // a pidfd is a small non-negative descriptor
		for {
			if _, err := unix.Poll(pfds, -1); err == unix.EINTR {
				continue
			}
			break
		}
		close(gone)
	}()
	return gone
}
