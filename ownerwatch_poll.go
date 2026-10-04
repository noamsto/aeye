//go:build !windows

package main

import (
	"syscall"
	"time"
)

// ownerPollInterval is the liveness-poll cadence on the fallback path. The
// expected owner is an agent process that lives for minutes, so a second's lag
// on its exit is invisible and the poll costs one kill(2) per tick.
const ownerPollInterval = time.Second

// pollOwnerProcess watches pid by polling liveness. A start token recorded at
// startup guards against a recycled pid: if the live process at pid did not
// start when the recorded one did, it is not our owner.
func pollOwnerProcess(pid int) <-chan struct{} {
	gone := make(chan struct{})
	token, tokenOK := processStartToken(pid)
	go func() {
		defer close(gone)
		if !processAlive(pid, token, tokenOK) {
			return
		}
		ticker := time.NewTicker(ownerPollInterval)
		defer ticker.Stop()
		for range ticker.C {
			if !processAlive(pid, token, tokenOK) {
				return
			}
		}
	}()
	return gone
}

// processAlive reports whether pid is still the same process as the one whose
// token was recorded. kill(pid, 0) returning ESRCH means gone; EPERM still means
// alive (it exists, we just can't signal it). When a start token is known, a
// mismatch means the pid was recycled.
func processAlive(pid int, token uint64, tokenOK bool) bool {
	err := syscall.Kill(pid, 0)
	if err == syscall.ESRCH {
		return false
	}
	if err != nil && err != syscall.EPERM {
		return false
	}
	if tokenOK {
		cur, ok := processStartToken(pid)
		if !ok || cur != token {
			return false
		}
	}
	return true
}
