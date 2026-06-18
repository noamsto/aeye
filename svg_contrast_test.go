package main

import (
	"bytes"
	"strings"
	"testing"
)

// node renders a d2-shaped node group: a shape carrying fill, then its label.
func node(fill, label string) string {
	return `<g class="aWQ= role"><g class="shape"><path stroke="#16a34a" fill="` + fill +
		`" class="shape" style="stroke-width:2;"/></g>` +
		`<text x="1" y="2" fill="#CDD6F4" class="text-bold fill-N1" style="text-anchor:middle;font-size:16px">` +
		label + `</text></g>`
}

// themedNode renders a node whose fill comes from a fill-N/fill-B class (d2's
// theme default) rather than an inline hex — the shape carries no fill attribute.
func themedNode(label string) string {
	return `<g class="aWQ= role"><g class="shape"><path stroke="#16a34a"` +
		` class="shape stroke-B1 fill-B5" style="stroke-width:2;"/></g>` +
		`<text x="1" y="2" fill="#CDD6F4" class="text-bold fill-N1" style="text-anchor:middle;font-size:16px">` +
		label + `</text></g>`
}

// edge renders a connection group: a connection path (fill:none), then its label.
func edge(label string) string {
	return `<g class="edge"><path d="M0 0" fill="none" class="connection stroke-B1" style="stroke-width:2;"/>` +
		`<text x="3" y="4" fill="#BAC2DE" class="text-italic fill-N2" style="text-anchor:middle;font-size:16px">` +
		label + `</text></g>`
}

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

func TestContrastSVGLightFillGetsDarkInk(t *testing.T) {
	out := string(contrastSVG([]byte(node("#dcfce7", "value"))))
	if !strings.Contains(out, "fill:"+contrastDarkInk) {
		t.Fatalf("label on a light fill should get dark ink in its inline style; got:\n%s", out)
	}
}

func TestContrastSVGDarkFillGetsLightInk(t *testing.T) {
	out := string(contrastSVG([]byte(node("#1e1e2e", "value"))))
	if !strings.Contains(out, "fill:"+contrastLightInk) {
		t.Fatalf("label on a dark fill should get light ink; got:\n%s", out)
	}
}

func TestContrastSVGEdgeLabelUntouched(t *testing.T) {
	out := string(contrastSVG([]byte(node("#dcfce7", "n") + edge("conflict"))))
	// Only the one node label is recolored; the edge label keeps its style.
	if n := strings.Count(out, "fill:"+contrastDarkInk); n != 1 {
		t.Errorf("expected exactly one recolored label, got %d in:\n%s", n, out)
	}
	if !strings.Contains(out, `font-size:16px">conflict</text>`) {
		t.Errorf("edge label style should be unchanged; got:\n%s", out)
	}
}

func TestContrastSVGThemedChildKeepsThemeText(t *testing.T) {
	// A themed child box nested after a light-filled parent container: its label
	// must keep the theme's light text, not inherit the parent's fill and end up
	// dark-on-dark.
	in := node("#fde8e8", "PARENT") + themedNode("CHILD")
	out := string(contrastSVG([]byte(in)))
	if n := strings.Count(out, "fill:"+contrastDarkInk); n != 1 {
		t.Fatalf("only the light-filled parent label should darken, not the themed child; got %d in:\n%s", n, out)
	}
	if !strings.Contains(out, `font-size:16px">CHILD</text>`) {
		t.Errorf("themed child label style should be untouched; got:\n%s", out)
	}
}

func TestContrastSVGGradientFillSkipped(t *testing.T) {
	in := `<g class="shape"><path fill="url(#streaks)" class="shape" style="s"/></g>` +
		`<text fill="#CDD6F4" class="fill-N1" style="text-anchor:middle">x</text>`
	out := string(contrastSVG([]byte(in)))
	if strings.Contains(out, "fill:"+contrastDarkInk) || strings.Contains(out, "fill:"+contrastLightInk) {
		t.Errorf("a non-solid (gradient) fill must not drive recoloring; got:\n%s", out)
	}
}

func TestContrastSVGInjectsStyleWhenAbsent(t *testing.T) {
	in := `<g class="shape"><path fill="#dcfce7" class="shape"/></g>` +
		`<text x="1" fill="#CDD6F4" class="fill-N1">x</text>`
	out := string(contrastSVG([]byte(in)))
	if !strings.Contains(out, `style="fill:`+contrastDarkInk+`"`) {
		t.Fatalf("a label without a style attr should gain one; got:\n%s", out)
	}
}

func TestContrastSVGIdempotent(t *testing.T) {
	once := contrastSVG([]byte(node("#dcfce7", "v") + edge("e")))
	twice := contrastSVG(once)
	if !bytes.Equal(once, twice) {
		t.Errorf("second pass changed the output:\nonce:  %s\ntwice: %s", once, twice)
	}
}

func TestContrastSVGSkipsForeignObject(t *testing.T) {
	// A node fill is set, then a markdown block whose HTML contains a literal
	// "<text>"; it must not be recolored.
	in := `<g class="shape"><path fill="#dcfce7" class="shape"/></g>` +
		`<foreignObject><div class="md">sample <text>x</text></div></foreignObject>`
	out := string(contrastSVG([]byte(in)))
	if strings.Contains(out, "fill:"+contrastDarkInk) {
		t.Errorf("content inside <foreignObject> must not be recolored; got:\n%s", out)
	}
}

func TestSetStyleFillNoAttrs(t *testing.T) {
	if got := setStyleFill("<text>", contrastDarkInk); got != `<text style="fill:`+contrastDarkInk+`">` {
		t.Errorf("bare tag: got %q", got)
	}
	if got := setStyleFill("<text/>", contrastDarkInk); got != `<text style="fill:`+contrastDarkInk+`"/>` {
		t.Errorf("self-closing: got %q", got)
	}
}

func TestContrastSVGPreservesNonLabelBytes(t *testing.T) {
	in := `<!-- a comment --><g class="shape"><path fill="#dcfce7" class="shape"/></g>` +
		`<text class="fill-N1" style="a:b">v</text></g>`
	out := string(contrastSVG([]byte(in)))
	if !strings.Contains(out, "<!-- a comment -->") {
		t.Errorf("comments must survive; got:\n%s", out)
	}
}
