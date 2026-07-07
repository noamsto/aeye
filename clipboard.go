package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// copyImageToClipboard puts the image file's bytes on the system clipboard so it
// can be pasted straight into a chat or doc. macOS drives the pasteboard through
// osascript; elsewhere it prefers wl-copy on Wayland and falls back to xclip on
// X11 (both daemonize to hold the selection, so Run returns once the bytes are
// handed off).
func copyImageToClipboard(path string) error {
	if runtime.GOOS == "darwin" {
		name, args := macClipboardCmd(path, mimeForPath(path))
		return exec.Command(name, args...).Run()
	}
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

// macClipboardCmd builds the osascript invocation that copies the image at path
// onto the macOS pasteboard. Raster formats with a pasteboard class are read as
// image data (so they paste into chats and docs); formats without one (svg,
// webp) fall back to a file reference, which pastes as an attachment.
func macClipboardCmd(path, mime string) (string, []string) {
	var script string
	if class, ok := macPasteboardClass(mime); ok {
		script = fmt.Sprintf("set the clipboard to (read (POSIX file %s) as %s)", asQuote(path), class)
	} else {
		script = fmt.Sprintf("set the clipboard to POSIX file %s", asQuote(path))
	}
	return "osascript", []string{"-e", script}
}

// macPasteboardClass maps an image MIME type to its AppleScript four-char
// pasteboard class, or reports false when the pasteboard has no image type for
// it (svg, webp).
func macPasteboardClass(mime string) (string, bool) {
	switch mime {
	case "image/png":
		return "«class PNGf»", true
	case "image/jpeg":
		return "«class JPEG»", true
	case "image/gif":
		return "«class GIFf»", true
	case "image/tiff":
		return "«class TIFF»", true
	}
	return "", false
}

// asQuote wraps s in an AppleScript string literal, escaping backslashes and
// quotes. AppleScript literals are byte-preserving, so UTF-8 paths pass through
// unharmed (unlike Go's %q, which would emit \u escapes AppleScript can't read).
func asQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
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
