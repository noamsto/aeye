package main

import "syscall"

// childMaxRSSKiB is the peak resident size of the largest child this process has
// waited for. It is only meaningful on Linux, where Maxrss is in KiB.
func childMaxRSSKiB() (int64, bool) {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_CHILDREN, &ru); err != nil {
		return 0, false
	}
	return ru.Maxrss, true
}
