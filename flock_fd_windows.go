//go:build windows

package main

import (
	"fmt"
	"io"
)

func runFlockFD(fd int, _ bool, stderr io.Writer) int {
	_, _ = fmt.Fprintf(stderr, "aeye flock-fd: fd %d: not supported on windows\n", fd)
	return 2
}
