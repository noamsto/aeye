package main

import (
	"os"
	"strconv"
)

// ownerWatch returns a channel closed when the process named by AEYE_OWNER_PID
// exits, or nil when there is no owner to watch: the pid is absent (a manual
// launch), invalid, or the session is remote-bridged, where the viewer may run
// on a different host from the agent and a local pid is meaningless.
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
