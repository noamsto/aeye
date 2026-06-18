package main

import "testing"

func TestWithTheme(t *testing.T) {
	for _, c := range []struct{ in, mode, want string }{
		{"/d/abc-dark.png", "light", "/d/abc-light.png"},
		{"/d/abc-light.svg", "dark", "/d/abc-dark.svg"},
		{"/d/abc-dark.png", "dark", "/d/abc-dark.png"},      // already the target
		{"/d/screenshot.png", "light", "/d/screenshot.png"}, // no theme suffix
		{"/d/abc.png", "dark", "/d/abc.png"},                // pre-#81 cache
		{"", "light", ""},
	} {
		if got := withTheme(c.in, c.mode); got != c.want {
			t.Errorf("withTheme(%q,%q) = %q, want %q", c.in, c.mode, got, c.want)
		}
	}
}

func TestResolveThemeVariantsOnlyD2(t *testing.T) {
	in := []imageEntry{
		{Path: "/d/a-dark.png", Vector: "/d/a-dark.svg", Source: "d2"},
		{Path: "/d/shot-dark.png", Source: "screenshot"}, // non-d2: untouched even if suffix-like
	}
	out := resolveThemeVariants(in, "light")
	if out[0].Path != "/d/a-light.png" || out[0].Vector != "/d/a-light.svg" {
		t.Errorf("d2 entry not resolved: %+v", out[0])
	}
	if out[1].Path != "/d/shot-dark.png" {
		t.Errorf("non-d2 entry must be untouched, got %q", out[1].Path)
	}
}
