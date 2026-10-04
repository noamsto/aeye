//go:build darwin

package main

import "golang.org/x/sys/unix"

// watchOwner watches pid with kqueue's EVFILT_PROC/NOTE_EXIT. Registering on a
// pid that has already exited fails (ESRCH); the poll fallback then fires at
// once.
func watchOwner(pid int) <-chan struct{} {
	kq, err := unix.Kqueue()
	if err != nil {
		return pollOwnerProcess(pid)
	}
	ev := unix.Kevent_t{
		Ident:  uint64(pid),
		Filter: unix.EVFILT_PROC,
		Flags:  unix.EV_ADD | unix.EV_ENABLE | unix.EV_ONESHOT,
		Fflags: unix.NOTE_EXIT,
	}
	if _, err := unix.Kevent(kq, []unix.Kevent_t{ev}, nil, nil); err != nil {
		unix.Close(kq) //nolint:errcheck // registration failed; nothing to watch
		return pollOwnerProcess(pid)
	}
	gone := make(chan struct{})
	go func() {
		defer unix.Close(kq) //nolint:errcheck // close on the watcher goroutine's exit
		buf := make([]unix.Kevent_t, 1)
		for {
			if _, err := unix.Kevent(kq, nil, buf, nil); err == unix.EINTR {
				continue
			}
			break
		}
		close(gone)
	}()
	return gone
}
