package main

import (
	"fmt"
	"os"
)

// version is stamped at build time via -ldflags "-X main.version=...".
// Plain `go build` / devShell runs leave it "dev".
var version = "dev"

// usage:
//
//	aeye <key>              open the carousel for a manifest key (tmux pane id
//	                        %N or a Claude Code session id)
//	aeye svg-contrast FILE  recolor a d2 SVG's labels to contrast their fills
//	aeye --version          print the build version
func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v" || os.Args[1] == "version") {
		fmt.Println(version)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "svg-contrast" {
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: aeye svg-contrast FILE")
			os.Exit(2)
		}
		if err := runSVGContrast(os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	key := ""
	if len(os.Args) > 1 {
		key = os.Args[1]
	}
	if err := runGallery(key); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
