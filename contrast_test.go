package main

import (
	"os"
	"path/filepath"
	"regexp"
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
	return renderSrcTheme(t, "200", src)
}

// renderSrcTheme is renderSrc with an explicit d2 theme, so a case can run under
// the light palette where a wrongly-inked label actually vanishes.
func renderSrcTheme(t *testing.T, theme, src string) string {
	t.Helper()
	t.Setenv("AEYE_D2_SKETCH", "0")
	t.Setenv("AEYE_D2_THEME", theme)
	p := filepath.Join(t.TempDir(), "d.d2")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
		t.Fatal(err)
	}
	svg, err := renderD2SVG(p)
	if err != nil {
		t.Fatalf("renderD2SVG: %v", err)
	}
	return string(svg)
}

// labelFill returns the fill of the <text> element whose body is exactly label,
// so a case can compare two labels' inks rather than merely spot an ink.
func labelFill(t *testing.T, svg, label string) string {
	t.Helper()
	re := regexp.MustCompile(`<text[^>]*fill="([^"]*)"[^>]*>` + regexp.QuoteMeta(label) + `</text>`)
	m := re.FindStringSubmatch(svg)
	if m == nil {
		t.Fatalf("no text label %q in svg", label)
	}
	return m[1]
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

// Named CSS colors and #rgb shorthand must contrast too — the hex-only gate used
// to skip them, so a light named fill (roster.d2's lightgreen/gold) sat
// light-on-light under the dark theme.
func TestContrastLabelsResolvesNamedAndShorthandFills(t *testing.T) {
	for _, c := range []struct {
		name, fill, wantInk string
	}{
		{"light named", "lightgreen", contrastDarkInk},
		{"light named gold", "gold", contrastDarkInk},
		{"dark named", "navy", contrastLightInk},
		{"light shorthand", `"#efe"`, contrastDarkInk},
		{"dark shorthand", `"#123"`, contrastLightInk},
	} {
		t.Run(c.name, func(t *testing.T) {
			svg := renderSrc(t, "a: A {style.fill: "+c.fill+"}\n")
			if !strings.Contains(svg, c.wantInk) {
				t.Fatalf("fill %s should ink %s; not found", c.fill, c.wantInk)
			}
		})
	}
}

// A fully transparent fill is no fill: the shape keeps the theme label color
// exactly like an unfilled one, under every theme.
func TestContrastLabelsTransparentFillKeepsThemeInk(t *testing.T) {
	for _, theme := range []string{"105", "200"} {
		t.Run("theme"+theme, func(t *testing.T) {
			src := "a: \"no fill\"\nb: \"transparent\" { style: { fill: transparent } }\n"
			svg := renderSrcTheme(t, theme, src)
			unfilled := labelFill(t, svg, "no fill")
			transparent := labelFill(t, svg, "transparent")
			if transparent != unfilled {
				t.Errorf("transparent fill label = %s, want the unfilled label's %s", transparent, unfilled)
			}
		})
	}
}

// Every full-transparency spelling csscolorparser accepts must be rejected as a
// fill, not just the keyword. d2's own grammar only takes "transparent" (it
// refuses rgba()/8-digit hex as a fill value), so the other spellings are pinned
// here at the gate; a partially transparent fill is still a fill.
func TestResolvableFillRejectsFullyTransparent(t *testing.T) {
	for _, fill := range []string{"transparent", "rgba(0,0,0,0)", "#00000000", "#0000", "hsla(0,0%,0%,0)"} {
		if resolvableFill(fill) {
			t.Errorf("resolvableFill(%q) = true, want false (fully transparent)", fill)
		}
	}
	for _, fill := range []string{"rgba(0,0,0,0.5)", "lightgreen", "#efe", "#1e1e2e"} {
		if !resolvableFill(fill) {
			t.Errorf("resolvableFill(%q) = false, want true", fill)
		}
	}
	for _, fill := range []string{"url(#grad)", "N1"} {
		if resolvableFill(fill) {
			t.Errorf("resolvableFill(%q) = true, want false (not a concrete color)", fill)
		}
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
