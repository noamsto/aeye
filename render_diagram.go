package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"oss.terrastruct.com/d2/d2graph"
	"oss.terrastruct.com/d2/d2layouts/d2dagrelayout"
	"oss.terrastruct.com/d2/d2layouts/d2elklayout"
	"oss.terrastruct.com/d2/d2lib"
	"oss.terrastruct.com/d2/d2renderers/d2svg"
	"oss.terrastruct.com/d2/lib/log"
	"oss.terrastruct.com/d2/lib/textmeasure"
	"oss.terrastruct.com/util-go/go2"

	"github.com/noamsto/themestate"
)

// renderD2SVG compiles a .d2 file to SVG bytes via the embedded d2 library,
// matching the carousel's old CLI invocation (`d2 -t 200 --sketch`). The
// in-file `vars: { d2-config: ... }` still overrides these (e.g. the
// layout-engine: elk that keeps arrows from crossing node labels).
// d2ThemeID picks the d2 theme. AEYE_D2_THEME wins when set; otherwise the
// terminal's mode decides — dark gets the dark theme (200), light the light one
// (105) — so a diagram matches the surroundings it's rendered into without the
// skill having to ask. In-file d2-config still overrides the chosen theme.
func d2ThemeID() (int64, error) {
	if t := os.Getenv("AEYE_D2_THEME"); t != "" {
		n, err := strconv.ParseInt(t, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid AEYE_D2_THEME %q: %w", t, err)
		}
		return n, nil
	}
	if themestate.Detect() == "light" {
		return 105, nil
	}
	return 200, nil
}

func renderD2SVG(in string) ([]byte, error) {
	src, err := os.ReadFile(in) //nolint:gosec // path is the user's own image/manifest file
	if err != nil {
		return nil, err
	}
	return compileD2SVG(string(src), in)
}

// compileD2SVG is renderD2SVG on source already in memory. inputPath anchors
// the source's relative imports; "" resolves them against the working dir.
func compileD2SVG(src, inputPath string) ([]byte, error) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		return nil, err
	}
	layoutResolver := func(engine string) (d2graph.LayoutGraph, error) {
		if engine == "elk" {
			return d2elklayout.DefaultLayout, nil
		}
		return d2dagrelayout.DefaultLayout, nil
	}
	themeID, err := d2ThemeID()
	if err != nil {
		return nil, err
	}
	renderOpts := &d2svg.RenderOpts{
		Pad:     go2.Pointer(int64(d2svg.DEFAULT_PADDING)),
		ThemeID: &themeID,
		Sketch:  new(os.Getenv("AEYE_D2_SKETCH") != "0"), // on unless =0
	}
	// Leave Layout nil so d2 picks it up from the source's
	// `vars: { d2-config: { layout-engine: ... } }` (it only honors that when
	// Layout is unset), falling back to dagre. LayoutResolver routes the chosen
	// engine — including the in-file elk that keeps arrows off node labels.
	// Prepend the semantic role classes for this theme so a diagram can tag
	// shapes/edges `class: warn` etc. without naming a color or a mode. d2 merges
	// this with any classes block in the source.
	source := classesBlock(paletteMode(themeID)) + src
	ctx := log.WithDefault(context.Background())
	diagram, graph, err := d2lib.Compile(ctx, source, &d2lib.CompileOptions{
		Ruler:          ruler,
		LayoutResolver: layoutResolver,
		InputPath:      inputPath,
	}, renderOpts)
	if err != nil {
		return nil, err
	}
	contrastLabels(diagram, graph)
	return d2svg.Render(diagram, renderOpts)
}

var (
	fontFamilyRe = regexp.MustCompile(`d2-[0-9]+-font-[a-z]+(?:-[a-z]+)*`)
	boldRe       = regexp.MustCompile(`\.text-bold \{(?:font-weight:bold;)?`)
	italicRe     = regexp.MustCompile(`\.text-italic \{(?:font-style:italic;)?`)
)

// d2FontFamily is the installed family text is rendered in (AEYE_D2_FONT).
func d2FontFamily() string {
	if family := os.Getenv("AEYE_D2_FONT"); family != "" {
		return family
	}
	return "Noto Sans"
}

// fixFonts rewrites d2's embedded @font-face family names to an installed
// family and injects weight/style onto the bold/italic classes — resvg ignores
// @font-face, so without this every text label is dropped. Idempotent: the
// optional already-injected token lets a re-run match without doubling.
// (Port of the former d2-fix-fonts.sh.)
func fixFonts(svg []byte) []byte {
	family := d2FontFamily()
	svg = fontFamilyRe.ReplaceAllLiteral(svg, []byte(family))
	svg = boldRe.ReplaceAllLiteral(svg, []byte(".text-bold {font-weight:bold;"))
	svg = italicRe.ReplaceAllLiteral(svg, []byte(".text-italic {font-style:italic;"))
	return svg
}

// runRenderDiagram is the whole diagram pipeline in one call: compile the .d2
// via the embedded d2 library (labels contrasted against their fills during
// compile), rewrite fonts, write the processed SVG next to the PNG (the
// carousel's vector source), then rasterize to PNG with resvg. A .svg input
// skips compile and fixFonts: its bytes are used verbatim.
func runRenderDiagram(in, out string) error {
	var svg []byte
	var err error
	var sizeArgs []string
	if strings.EqualFold(filepath.Ext(in), ".svg") {
		svg, err = os.ReadFile(in) //nolint:gosec // path is the user's own diagram file
		if err != nil {
			return fmt.Errorf("read %s: %w", in, err)
		}
		w, h, err := svgSize(svg)
		if err != nil {
			return fmt.Errorf("svg %s: cannot determine its size (%w)", in, err)
		}
		sizeArgs = rasterCapArgs(w, h)
	} else {
		svg, err = renderD2SVG(in)
		if err != nil {
			return fmt.Errorf("compile %s: %w", in, err)
		}
		svg = fixFonts(svg)
	}

	svgPath := out[:len(out)-len(filepath.Ext(out))] + ".svg" // out has no ext -> out+".svg"
	if err := os.WriteFile(svgPath, svg, 0o644); err != nil { //nolint:gosec // rendered diagram is a user-readable output file
		return fmt.Errorf("write %s: %w", svgPath, err)
	}

	args := append(append(resvgFontArgs(), sizeArgs...), svgPath, out)
	cmd := exec.Command(resvgBin(), args...) //nolint:gosec // args built by the tool, not shell-interpreted
	if stderr, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("resvg: %w: %s", err, stderr)
	}
	return nil
}

// maxRasterSide caps the long side of a rasterized .svg input: resvg renders at
// the declared size, so a few bytes claiming 20000x20000 cost gigabytes.
const maxRasterSide = 8192

// rasterCapArgs bounds resvg's output to the w×h canvas, scaled down so its long
// side fits maxRasterSide. Both bounds always go out, and resvg fits within
// both keeping its own aspect (scaling up or down): if svgSize ever reads the
// file differently from resvg, the output may be resized but never past the cap.
func rasterCapArgs(w, h float64) []string {
	s := min(1, maxRasterSide/max(w, h))
	side := func(v float64) string {
		return strconv.Itoa(int(max(1, min(maxRasterSide, math.Ceil(v*s)))))
	}
	return []string{"--width", side(w), "--height", side(h)}
}

var errNoSVGSize = errors.New("no width/height or viewBox")

// svgUnitPx converts the absolute CSS units to px at 96 dpi, as resvg does.
var svgUnitPx = map[string]float64{
	"": 1, "px": 1, "pt": 4.0 / 3, "pc": 16, "in": 96, "cm": 96 / 2.54, "mm": 96 / 25.4,
}

// svgSize is the pixel size resvg rasterizes svg at, read from the root
// element's width, height and viewBox. A percentage resolves against the
// viewBox; a missing side follows the viewBox's aspect. em/ex are rejected: they
// hang off a font size a stylesheet can set, which the root tag doesn't show.
func svgSize(svg []byte) (w, h float64, err error) {
	attrs, err := svgRootAttrs(svg)
	if err != nil {
		return 0, 0, err
	}
	vb, hasVB := parseViewBox(attrs["viewBox"])
	// resvg ignores a viewBox without a positive, finite area.
	hasVB = hasVB && vb.w > 0 && vb.h > 0 && !math.IsInf(vb.w+vb.h, 0)
	w, wSet, err := svgLength("width", attrs["width"], vb.w, hasVB)
	if err != nil {
		return 0, 0, err
	}
	h, hSet, err := svgLength("height", attrs["height"], vb.h, hasVB)
	if err != nil {
		return 0, 0, err
	}
	switch {
	case wSet && hSet:
	case !hasVB:
		return 0, 0, errNoSVGSize
	case wSet:
		h = w * vb.h / vb.w
	case hSet:
		w = h * vb.w / vb.h
	default:
		w, h = vb.w, vb.h
	}
	if w <= 0 || h <= 0 || math.IsInf(w+h, 0) {
		return 0, 0, fmt.Errorf("invalid size %vx%v", w, h)
	}
	return w, h, nil
}

// svgNS is the SVG namespace: resvg reads a root attribute prefixed into it
// (s:width with xmlns:s set to it) the same as an unprefixed one.
const svgNS = "http://www.w3.org/2000/svg"

// svgRootAttrs returns the attributes resvg reads off the document's root
// element, which must be <svg>: the un-namespaced ones and those in svgNS.
func svgRootAttrs(svg []byte) (map[string]string, error) {
	dec := xml.NewDecoder(bytes.NewReader(svg))
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("read root element: %w", err)
		}
		el, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if el.Name.Local != "svg" {
			return nil, fmt.Errorf("root element is <%s>, not <svg>", el.Name.Local)
		}
		attrs := map[string]string{}
		for _, a := range el.Attr {
			if a.Name.Space != "" && a.Name.Space != svgNS {
				continue
			}
			if _, dup := attrs[a.Name.Local]; dup {
				return nil, fmt.Errorf("root <svg> sets %s twice", a.Name.Local)
			}
			attrs[a.Name.Local] = a.Value
		}
		return attrs, nil
	}
}

// svgLength resolves a root width/height to px. set is false when it is missing,
// or a percentage with no viewBox to resolve against. Like resvg it allows
// leading whitespace but not trailing.
func svgLength(name, v string, vbSide float64, hasVB bool) (px float64, set bool, err error) {
	v = strings.TrimLeft(v, " \t\r\n")
	if v == "" {
		return 0, false, nil
	}
	var num, unit string
	if pct, ok := strings.CutSuffix(v, "%"); ok {
		num, unit = pct, "%"
	} else {
		i := strings.LastIndexFunc(v, func(r rune) bool { return r < 'a' || r > 'z' })
		num, unit = v[:i+1], v[i+1:]
	}
	n, ok := parseSVGNumber(num)
	if !ok {
		return 0, false, fmt.Errorf("unreadable %s %q", name, v)
	}
	if unit == "%" {
		return vbSide * n / 100, hasVB, nil
	}
	scale, ok := svgUnitPx[unit]
	if !ok {
		return 0, false, fmt.Errorf("unsupported %s unit %q", name, v)
	}
	return n * scale, true, nil
}

// resvgBin is the resvg executable: AEYE_RESVG when set, else resvg on PATH.
func resvgBin() string {
	if bin := os.Getenv("AEYE_RESVG"); bin != "" {
		return bin
	}
	return "resvg"
}

// resvgFontArgs makes resvg's default and generic serif/sans-serif families the
// one fixFonts uses: an SVG that names no family (or a generic one) would
// otherwise fall back to a font that may not exist and drop its text. With
// AEYE_D2_FONT_DIR set it also pins resvg to that bundled font directory, since
// the family (e.g. Source Sans 3) isn't a system font on macOS. Every resvg call
// must use these — the live sharp render as much as this hook — or the sharp
// render drops text the cached raster kept.
func resvgFontArgs() []string {
	f := d2FontFamily()
	args := []string{"--font-family", f, "--serif-family", f, "--sans-serif-family", f}
	if dir := os.Getenv("AEYE_D2_FONT_DIR"); dir != "" {
		args = append(args, "--skip-system-fonts", "--use-fonts-dir", dir)
	}
	return args
}
