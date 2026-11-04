package main

import (
	"bytes"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var offsetValueRe = regexp.MustCompile(`stroke-dashoffset:[^;]*;`)

func TestAnimFrameSVG_D2OneAnimatedEdge(t *testing.T) {
	for _, sketch := range []string{"0", "1"} {
		t.Run("sketch="+sketch, func(t *testing.T) {
			t.Setenv("AEYE_D2_THEME", "200")
			t.Setenv("AEYE_D2_SKETCH", sketch)
			svg, err := compileD2SVG("a -> b: {style.animated: true}", "")
			if err != nil {
				t.Fatal(err)
			}
			if !svgAnimates(svg) {
				t.Fatal("svgAnimates = false for an animated edge")
			}
			const n = 12
			want := offsetValueRe.ReplaceAll(svg, nil)
			frames := make([][]byte, n)
			for k := range frames {
				frames[k] = animFrameSVG(svg, k, n)
				if !bytes.Equal(offsetValueRe.ReplaceAll(frames[k], nil), want) {
					t.Fatalf("frame %d differs from input outside the offset value", k)
				}
			}
			for i := range frames {
				for j := i + 1; j < n; j++ {
					if bytes.Equal(frames[i], frames[j]) {
						t.Fatalf("frames %d and %d are identical", i, j)
					}
				}
			}
			if !bytes.Equal(animFrameSVG(svg, 12, n), frames[0]) {
				t.Error("frame 12 != frame 0")
			}
			if !bytes.Equal(animFrameSVG(svg, 13, n), frames[1]) {
				t.Error("frame 13 != frame 1")
			}
			if !bytes.Contains(frames[0], []byte("stroke-dashoffset:0.0000;")) {
				t.Error("frame 0 lacks stroke-dashoffset:0.0000;")
			}
			// One loop is one dash period, so each frame shifts by period/n.
			edge := func(svg []byte) []byte {
				for _, tag := range animPathTagRe.FindAll(svg, -1) {
					if animEdgeRe.Match(tag) {
						return tag
					}
				}
				return nil
			}
			dash := animDasharrayRe.FindSubmatch(edge(svg))
			off := animOffsetRe.FindSubmatch(edge(frames[1]))
			if dash == nil || off == nil {
				t.Fatal("no dasharray / dashoffset in the animated edge")
			}
			d, _ := strconv.ParseFloat(string(dash[1]), 64)
			g, _ := strconv.ParseFloat(string(dash[2]), 64)
			o, _ := strconv.ParseFloat(string(off[2]), 64)
			if got := math.Abs(o) * n; math.Abs(got-(d+g)) > 1e-3 {
				t.Errorf("|offset(1)|*n = %v, want the dash period %v", got, d+g)
			}
		})
	}
}

func TestAnimNum(t *testing.T) {
	negZero := 0.0
	negZero = -negZero
	for _, tc := range []struct {
		in   float64
		want string
	}{
		{0, "0.0000"},
		{negZero, "0.0000"},
		{-0.00001, "0.0000"},
		{-2.5, "-2.5000"},
		{2.5, "2.5000"},
	} {
		if got := animNum(tc.in); got != tc.want {
			t.Errorf("animNum(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestAnimFrameSVG_NoAnimatedEdge(t *testing.T) {
	t.Setenv("AEYE_D2_THEME", "200")
	svg, err := compileD2SVG("a -> b", "")
	if err != nil {
		t.Fatal(err)
	}
	if svgAnimates(svg) {
		t.Fatal("svgAnimates = true for a static diagram")
	}
	if got := animFrameSVG(svg, 5, 12); !bytes.Equal(got, svg) {
		t.Error("static svg changed")
	}
}

func TestAnimFrameSVG_Direction(t *testing.T) {
	tag := func(offset, extra string) []byte {
		return fmt.Appendf(nil, `<svg><path class="connection animated-connection" style="stroke-width:2;stroke-dasharray:6,4;stroke-dashoffset:%s;animation: dashdraw 1s linear infinite;%s"/></svg>`, offset, extra)
	}
	for _, tc := range []struct {
		name string
		svg  []byte
		want string
	}{
		{"negative", tag("-100", ""), "stroke-dashoffset:-2.5000;"},
		{"positive", tag("100", ""), "stroke-dashoffset:2.5000;"},
		{"reverse", tag("-100", "animation-direction: reverse;"), "stroke-dashoffset:2.5000;"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := animFrameSVG(tc.svg, 3, 12); !bytes.Contains(got, []byte(tc.want)) {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestAnimFrameSVG_ClassHooks(t *testing.T) {
	svg := []byte(`<svg viewBox="0 0 10 10"><g class="edge aeye-flow"><path d="M0 0L5 5"/></g><g class="aeye-pulse"/><g class="aeye-glow"/></svg>`)
	if !svgAnimates(svg) {
		t.Fatal("svgAnimates = false for class hooks")
	}
	const n = 12
	for k := range n {
		got := string(animFrameSVG(svg, k, n))
		if c := strings.Count(got, "<style>"); c != 1 {
			t.Fatalf("frame %d: %d <style> elements", k, c)
		}
		if !strings.HasSuffix(got, "</style></svg>") {
			t.Fatalf("frame %d: style not immediately before </svg>", k)
		}
		css := got[strings.Index(got, "<style>"):]
		for _, want := range []string{
			".aeye-flow path, path.aeye-flow",
			"stroke-dashoffset: " + animNum(-float64(k)),
			"opacity: ",
			"filter: brightness(",
		} {
			if !strings.Contains(css, want) {
				t.Errorf("frame %d: css lacks %q: %s", k, want, css)
			}
		}
	}
	if !bytes.Equal(animFrameSVG(svg, n, n), animFrameSVG(svg, 0, n)) {
		t.Error("frame n != frame 0")
	}
	f0 := string(animFrameSVG(svg, 0, n))
	if !strings.Contains(f0, "opacity: 1.0000") || !strings.Contains(f0, "brightness(1.0000)") {
		t.Errorf("frame 0 not neutral: %s", f0)
	}
	for _, tc := range []struct {
		k                   int
		opacity, brightness string
	}{{3, "0.8000", "1.1500"}, {6, "0.6000", "1.3000"}} {
		got := string(animFrameSVG(svg, tc.k, n))
		if !strings.Contains(got, "opacity: "+tc.opacity) || !strings.Contains(got, "brightness("+tc.brightness+")") {
			t.Errorf("frame %d: want opacity %s, brightness %s: %s", tc.k, tc.opacity, tc.brightness, got)
		}
	}
	if svgAnimates([]byte(`<svg><g class="aeye-flowing"/></svg>`)) {
		t.Error("aeye-flowing must not match")
	}
}
