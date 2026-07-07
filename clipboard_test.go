package main

import (
	"strings"
	"testing"
)

func TestMimeForPath(t *testing.T) {
	for _, c := range []struct{ path, want string }{
		{"/x/diagram.png", "image/png"},
		{"/x/shot.PNG", "image/png"},
		{"/x/photo.jpg", "image/jpeg"},
		{"/x/photo.jpeg", "image/jpeg"},
		{"/x/anim.gif", "image/gif"},
		{"/x/pic.webp", "image/webp"},
		{"/x/vec.svg", "image/svg+xml"},
		{"/x/noext", "image/png"},
	} {
		if got := mimeForPath(c.path); got != c.want {
			t.Errorf("mimeForPath(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestMacClipboardCmd(t *testing.T) {
	t.Run("reads raster formats as pasteboard image data", func(t *testing.T) {
		name, args := macClipboardCmd("/x/diagram.png", "image/png")
		want := `set the clipboard to (read (POSIX file "/x/diagram.png") as «class PNGf»)`
		if name != "osascript" || len(args) != 2 || args[0] != "-e" || args[1] != want {
			t.Fatalf("got (%q, %v)", name, args)
		}
	})
	t.Run("falls back to a file reference for classless formats", func(t *testing.T) {
		_, args := macClipboardCmd("/x/vec.svg", "image/svg+xml")
		want := `set the clipboard to POSIX file "/x/vec.svg"`
		if args[1] != want {
			t.Fatalf("got %q, want %q", args[1], want)
		}
	})
	t.Run("escapes quotes and backslashes in the path", func(t *testing.T) {
		_, args := macClipboardCmd(`/x/a "b"\c.png`, "image/png")
		if !strings.Contains(args[1], `"/x/a \"b\"\\c.png"`) {
			t.Fatalf("path not escaped: %q", args[1])
		}
	})
}

func TestMacPasteboardClass(t *testing.T) {
	for _, c := range []struct {
		mime, want string
		ok         bool
	}{
		{"image/png", "«class PNGf»", true},
		{"image/jpeg", "«class JPEG»", true},
		{"image/gif", "«class GIFf»", true},
		{"image/tiff", "«class TIFF»", true},
		{"image/svg+xml", "", false},
		{"image/webp", "", false},
	} {
		got, ok := macPasteboardClass(c.mime)
		if got != c.want || ok != c.ok {
			t.Errorf("macPasteboardClass(%q) = (%q, %v), want (%q, %v)", c.mime, got, ok, c.want, c.ok)
		}
	}
}

func TestClipboardToolPrefersWaylandWhenDisplaySet(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	if _, ok := lookPath("wl-copy"); !ok {
		t.Skip("wl-copy not on PATH")
	}
	name, args := clipboardTool("image/png")
	if name != "wl-copy" {
		t.Fatalf("with WAYLAND_DISPLAY set and wl-copy present, want wl-copy, got %q", name)
	}
	if len(args) < 2 || args[len(args)-1] != "image/png" {
		t.Errorf("expected the mime to be passed through, got %v", args)
	}
}
