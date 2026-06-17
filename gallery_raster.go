package main

import (
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
)

// parseSixelDA reports whether a Primary Device Attributes reply advertises
// sixel support (attribute 4). Reply form: ESC [ ? p1 ; p2 ; ... c
func parseSixelDA(resp string) bool {
	i := strings.IndexByte(resp, '?')
	j := strings.LastIndexByte(resp, 'c')
	if i < 0 || j < 0 || j < i {
		return false
	}
	for _, p := range strings.Split(resp[i+1:j], ";") {
		if p == "4" {
			return true
		}
	}
	return false
}

// probeSixel asks the terminal whether it can render sixel by writing a Primary
// Device Attributes query (ESC [ c) to /dev/tty and checking the reply for the
// sixel attribute (4). It runs in raw mode with a short deadline and fully drains
// the reply to the 'c' terminator, so a late response can't leak onto bubbletea's
// stdin and be misparsed as keystrokes. Any failure (no tty, timeout, no reply)
// returns false, so we never emit sixel bytes to an unconfirmed terminal.
//
// Inside tmux, tmux answers this query reflecting its own sixel capability
// (present when tmux is built --enable-sixel over a sixel-capable outer terminal).
func probeSixel() bool {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer tty.Close()
	old, err := term.MakeRaw(tty.Fd())
	if err != nil {
		return false
	}
	defer term.Restore(tty.Fd(), old)

	if _, err := tty.WriteString("\x1b[c"); err != nil {
		return false
	}

	ch := make(chan string, 1)
	go func() {
		var buf []byte
		b := make([]byte, 1)
		for {
			n, err := tty.Read(b)
			if n > 0 {
				buf = append(buf, b[0])
				if b[0] == 'c' {
					break
				}
			}
			if err != nil {
				break
			}
		}
		ch <- string(buf)
	}()

	select {
	case s := <-ch:
		return parseSixelDA(s)
	case <-time.After(150 * time.Millisecond):
		// Deferred tty.Close() unblocks the goroutine's Read, so it can't leak.
		return false
	}
}
