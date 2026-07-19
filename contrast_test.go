package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContrastInk(t *testing.T) {
	for _, c := range []struct{ fill, want string }{
		{"#dcfce7", contrastDarkInk},  // light pastel green
		{"#ffedd5", contrastDarkInk},  // light pastel orange
		{"#ffffff", contrastDarkInk},  // white
		{"#1e1e2e", contrastLightInk}, // theme-dark base
		{"#000000", contrastLightInk}, // black
		{"#2563eb", contrastLightInk}, // mid-dark blue (L≈0.38)
	} {
		if got := contrastInk(c.fill); got != c.want {
			t.Errorf("contrastInk(%s) = %s, want %s", c.fill, got, c.want)
		}
	}
}

// renderSrc compiles d2 source to SVG through the real pipeline — sketch off for
// determinism, dark theme so theme-default labels are light (never our inks).
func renderSrc(t *testing.T, src string) string {
	t.Helper()
	t.Setenv("AEYE_D2_SKETCH", "0")
	t.Setenv("AEYE_D2_THEME", "200")
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

func TestContrastLabelsUserLightFillGetsDarkInk(t *testing.T) {
	svg := renderSrc(t, `a: A {style.fill: "#fde8e8"}`+"\n")
	if !strings.Contains(svg, contrastDarkInk) {
		t.Fatalf("a light user fill should darken its label (%s); not found", contrastDarkInk)
	}
}

func TestContrastLabelsUserDarkFillGetsLightInk(t *testing.T) {
	svg := renderSrc(t, `a: A {style.fill: "#1e1e2e"}`+"\n")
	if !strings.Contains(svg, contrastLightInk) {
		t.Fatalf("a dark user fill should lighten its label (%s); not found", contrastLightInk)
	}
}

func TestContrastLabelsNamedLightFillGetsDarkInk(t *testing.T) {
	// A bright *named* fill — the roster-worker case — must ink its label too, not
	// just #RRGGBB fills. Before the csscolorparser resolve, named fills were left
	// to d2's theme, so a light name got a light theme label, light-on-light.
	svg := renderSrc(t, "a: A {style.fill: lightgreen}\n")
	if !strings.Contains(svg, contrastDarkInk) {
		t.Fatalf("a light named fill should darken its label (%s); not found", contrastDarkInk)
	}
}

func TestContrastLabelsLeavesThemeShapesAlone(t *testing.T) {
	// No custom fills: every label keeps its theme color, so our inks never appear.
	svg := renderSrc(t, "a -> b -> c\n")
	if strings.Contains(svg, contrastDarkInk) || strings.Contains(svg, contrastLightInk) {
		t.Fatalf("theme-default labels must not be recolored; an ink leaked:\n%.200s", svg)
	}
}

func TestContrastLabelsLeavesContainerLabelAlone(t *testing.T) {
	// A container's label is drawn above its fill, on the canvas — it must keep the
	// theme color (here: light, readable on the dark canvas), not be inked dark
	// against the fill. No edge labels, so nothing should darken.
	svg := renderSrc(t, "c: Outer {\n  style.fill: \"#fde8e8\"\n  a -> b\n}\n")
	if strings.Contains(svg, contrastDarkInk) {
		t.Fatalf("a container's outside label must not be inked against its fill:\n%.200s", svg)
	}
}

func TestContrastLabelsEdgeInsideFilledContainer(t *testing.T) {
	// An edge label drawn inside a light-filled container must darken too — the
	// connection carries no fill, so the old shape-only pass missed it.
	svg := renderSrc(t, "c: \"\" {\n  style.fill: \"#fde8e8\"\n  a -> b: hi\n}\n")
	if !strings.Contains(svg, contrastDarkInk) {
		t.Fatal("an edge label inside a light-filled container should darken")
	}
}

func TestContrastLabelsRespectsUserFontColor(t *testing.T) {
	// User chose both fill and font-color: the explicit font-color must win.
	svg := renderSrc(t, "a: A {\n  style.fill: \"#fde8e8\"\n  style.font-color: \"#00ff00\"\n}\n")
	if strings.Contains(svg, contrastDarkInk) {
		t.Fatal("an explicit font-color must not be overridden by the contrast pass")
	}
}
