//go:build !linux

package main

// processStartToken has no portable source on this platform; the poll fallback
// degrades to liveness alone. pidfd/kqueue cover the platforms this ships on.
func processStartToken(int) (uint64, bool) { return 0, false }
