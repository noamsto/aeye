//go:build !linux && !darwin

package main

// watchOwner has no native process-watch primitive on this platform, so it uses
// the liveness-poll fallback. The repo ships linux/darwin builds; this exists so
// the package still compiles elsewhere.
func watchOwner(pid int) <-chan struct{} { return pollOwnerProcess(pid) }
