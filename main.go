package main

import (
	"fmt"
	"os"
)

// usage: agent-carousel <key>
// <key> is a tmux pane id (%N) or a Claude Code session id — whatever the
// capture adapter used to name the manifest file.
func main() {
	key := ""
	if len(os.Args) > 1 {
		key = os.Args[1]
	}
	if err := runGallery(key); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
