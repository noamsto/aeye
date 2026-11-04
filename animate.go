package main

import (
	"bytes"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
)

const (
	animFrameMax   = 12   // frames per loop (n is lowered by the RGBA budget, never raised)
	animFrameMin   = 8    // below this the budget says "stay static"
	animGapMS      = 100  // per-frame gap: 12 × 100 ms = 1.2 s loop
	animFlowGap    = 12.0 // aeye-flow dot spacing, SVG user units
	animPulseDepth = 0.4  // aeye-pulse opacity 1 → 0.6
	animGlowDepth  = 0.3  // aeye-glow brightness 1 → 1.3
)

var (
	animPathTagRe   = regexp.MustCompile(`<path\b[^>]*>`)
	animEdgeRe      = regexp.MustCompile(`class="(?:[^"]*\s)?animated-connection(?:\s|")`)
	animHookRe      = regexp.MustCompile(`class="(?:[^"]*\s)?aeye-(?:flow|pulse|glow)(?:\s|")`)
	animDasharrayRe = regexp.MustCompile(`stroke-dasharray:\s*([\d.]+)\s*,\s*([\d.]+)`)
	animOffsetRe    = regexp.MustCompile(`(stroke-dashoffset:\s*)(-?[\d.]+)`)
)

// svgAnimates reports whether svg carries a D2 animated edge or an aeye-*
// class hook, i.e. whether animFrameSVG can produce distinct frames.
func svgAnimates(svg []byte) bool {
	return slices.ContainsFunc(animPathTagRe.FindAll(svg, -1), animEdgeRe.Match) || animHookRe.Match(svg)
}

// animFrameSVG returns loop frame k of n. k is reduced mod n first so frame n
// is byte-identical to frame 0, which makes the loop seamless.
func animFrameSVG(svg []byte, k, n int) []byte {
	k %= n
	out := animPathTagRe.ReplaceAllFunc(svg, func(tag []byte) []byte {
		if !animEdgeRe.Match(tag) {
			return tag
		}
		return animShiftEdge(tag, k, n)
	})
	if !animHookRe.Match(out) {
		return out
	}
	i := bytes.LastIndex(out, []byte("</svg>"))
	if i < 0 {
		return out
	}
	theta := 2 * math.Pi * float64(k) / float64(n)
	phase := (1 - math.Cos(theta)) / 2
	css := fmt.Sprintf("<style>"+
		".aeye-flow path, path.aeye-flow { stroke-dasharray: 0.01 %s; stroke-linecap: round; stroke-dashoffset: %s }"+
		".aeye-pulse { opacity: %s }"+
		".aeye-glow { filter: brightness(%s) }"+
		"</style>",
		animNum(animFlowGap),
		animNum(-float64(k)*animFlowGap/float64(n)),
		animNum(1-animPulseDepth*phase),
		animNum(1+animGlowDepth*phase))
	res := make([]byte, 0, len(out)+len(css))
	res = append(res, out[:i]...)
	res = append(res, css...)
	return append(res, out[i:]...)
}

// animShiftEdge rewrites a D2 animated edge's dashoffset to its phase for
// frame k of n. D2 animates the offset from 0 to ±10 periods in a browser;
// one period is a full loop here, in the same direction D2 uses.
func animShiftEdge(tag []byte, k, n int) []byte {
	dash := animDasharrayRe.FindSubmatch(tag)
	off := animOffsetRe.FindSubmatchIndex(tag)
	if dash == nil || off == nil {
		return tag
	}
	d, _ := strconv.ParseFloat(string(dash[1]), 64)
	g, _ := strconv.ParseFloat(string(dash[2]), 64)
	o, _ := strconv.ParseFloat(string(tag[off[4]:off[5]]), 64)
	dir := 1.0
	if o < 0 {
		dir = -1
	}
	if bytes.Contains(tag, []byte("animation-direction: reverse;")) {
		dir = -dir
	}
	v := animNum(dir * float64(k) * (d + g) / float64(n))
	res := make([]byte, 0, len(tag)+len(v))
	res = append(res, tag[:off[4]]...)
	res = append(res, v...)
	return append(res, tag[off[5]:]...)
}

// animNum formats v with fixed precision; values that round to zero print
// without a sign so frame text never differs by "-0.0000".
func animNum(v float64) string {
	s := fmt.Sprintf("%.4f", v)
	if s == "-0.0000" {
		return "0.0000"
	}
	return s
}
