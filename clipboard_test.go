package main

import "testing"

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
