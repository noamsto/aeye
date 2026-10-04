//go:build windows

package main

// watchOwner has no process-watch primitive here. Windows is not a supported
// aeye launch host, so the channel never fires and the viewer behaves as if no
// owner pid were set; this exists so the package still compiles for GOOS=windows.
func watchOwner(int) <-chan struct{} { return make(chan struct{}) }
