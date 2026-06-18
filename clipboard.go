package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// copyImageToClipboard puts the image file's bytes on the system clipboard so it
// can be pasted straight into a chat or doc. It prefers wl-copy on Wayland and
// falls back to xclip on X11 — the same Linux-only assumption openSelected makes
// with xdg-open. Both tools daemonize to hold the selection, so Run returns once
// the bytes are handed off.
func copyImageToClipboard(path string) error {
	name, args := clipboardTool(mimeForPath(path))
	if name == "" {
		return errors.New("no clipboard tool found (install wl-clipboard or xclip)")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	cmd := exec.Command(name, args...)
	cmd.Stdin = f
	return cmd.Run()
}

// clipboardTool picks the clipboard command and its args for the given image
// MIME type, or returns an empty name when none is on PATH.
func clipboardTool(mime string) (string, []string) {
	wayland := os.Getenv("WAYLAND_DISPLAY") != ""
	_, hasWlCopy := lookPath("wl-copy")
	if wayland && hasWlCopy {
		return "wl-copy", []string{"--type", mime}
	}
	if _, ok := lookPath("xclip"); ok {
		return "xclip", []string{"-selection", "clipboard", "-t", mime}
	}
	if hasWlCopy {
		return "wl-copy", []string{"--type", mime}
	}
	return "", nil
}

func lookPath(name string) (string, bool) {
	p, err := exec.LookPath(name)
	return p, err == nil
}

// mimeForPath maps an image file extension to its clipboard MIME type, defaulting
// to image/png (the format of every diagram and most screenshots aeye shows).
func mimeForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	default:
		return "image/png"
	}
}
