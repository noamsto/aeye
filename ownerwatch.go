package main

import (
	"os"
	"strconv"
)

// ownerWatch returns a channel closed when the process named by AEYE_OWNER_PID
// exits, or nil when there is no owner to watch. The launcher sets the pid when
// it can resolve the agent that opened the carousel; a carousel opened manually
// gets no watch and behaves exactly as before. Over the remote bridge the viewer
// need not run on the agent's host, so a local pid watch is meaningless and the
// pid is neither passed nor honoured there.
func ownerWatch() <-chan struct{} {
	if bridged() {
		return nil
	}
	pid, ok := ownerPIDFromEnv()
	if !ok {
		return nil
	}
	return watchOwner(pid)
}

// ownerPIDFromEnv parses AEYE_OWNER_PID into a positive pid. Absent, empty, or
// malformed values yield ok=false, so no watcher starts.
func ownerPIDFromEnv() (int, bool) {
	raw := os.Getenv("AEYE_OWNER_PID")
	if raw == "" {
		return 0, false
	}
	pid, err := strconv.Atoi(raw)
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}
