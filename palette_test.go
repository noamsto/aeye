package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPaletteMode(t *testing.T) {
	for _, c := range []struct {
		id   int64
		want string
	}{
		{105, "light"}, {0, "light"}, {8, "light"},
		{200, "dark"}, {201, "dark"},
	} {
		if got := paletteMode(c.id); got != c.want {
			t.Errorf("paletteMode(%d) = %s, want %s", c.id, got, c.want)
		}
	}
}

func TestClassesBlockDarkDropsFill(t *testing.T) {
	b := classesBlock("dark")
	if !strings.Contains(b, `stroke: "#f38ba8"`) || !strings.Contains(b, `font-color: "#f38ba8"`) {
		t.Fatalf("dark warn should carry the mocha accent on stroke + font-color:\n%s", b)
	}
	if strings.Contains(b, "fill:") {
		t.Fatalf("dark roles must not set a fill (keep the theme surface):\n%s", b)
	}
}

func TestClassesBlockLightUsesPastelFill(t *testing.T) {
	b := classesBlock("light")
	for _, want := range []string{`warn: { style: { fill: "#fde8e8";`, `stroke: "#d20f39"`, `good: { style: { fill: "#e6f4ea";`} {
		if !strings.Contains(b, want) {
			t.Fatalf("light block missing %q:\n%s", want, b)
		}
	}
}

// renderThemed compiles src through the real pipeline at the given d2 theme.
func renderThemed(t *testing.T, src, theme string) string {
	t.Helper()
	t.Setenv("AEYE_D2_SKETCH", "0")
	t.Setenv("AEYE_D2_THEME", theme)
	p := filepath.Join(t.TempDir(), "d.d2")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	svg, err := renderD2SVG(p)
	if err != nil {
		t.Fatalf("renderD2SVG: %v", err)
	}
	return string(svg)
}

func TestInjectedClassAppliesPerTheme(t *testing.T) {
	src := "a: A { class: warn }\n"
	if dark := renderThemed(t, src, "200"); !strings.Contains(dark, "#f38ba8") {
		t.Error("dark: a `class: warn` shape should carry the mocha accent")
	}
	if light := renderThemed(t, src, "105"); !strings.Contains(light, "#fde8e8") {
		t.Error("light: a `class: warn` shape should carry the pastel fill")
	}
}
