package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"

	"oss.terrastruct.com/d2/d2graph"
	"oss.terrastruct.com/d2/d2layouts/d2dagrelayout"
	"oss.terrastruct.com/d2/d2layouts/d2elklayout"
	"oss.terrastruct.com/d2/d2lib"
	"oss.terrastruct.com/d2/d2renderers/d2svg"
	"oss.terrastruct.com/d2/lib/log"
	"oss.terrastruct.com/d2/lib/textmeasure"
	"oss.terrastruct.com/util-go/go2"
)

// renderD2SVG compiles a .d2 file to SVG bytes via the embedded d2 library,
// matching the carousel's old CLI invocation (`d2 -t 200 --sketch`). The
// in-file `vars: { d2-config: ... }` still overrides these (e.g. the
// layout-engine: elk that keeps arrows from crossing node labels).
func renderD2SVG(in string) ([]byte, error) {
	src, err := os.ReadFile(in)
	if err != nil {
		return nil, err
	}
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
	// Theme + sketch stay env-configurable, matching the old shell pipeline
	// (AEYE_D2_THEME default 105 = light; 200 = dark). In-file d2-config still
	// overrides these.
	themeID := int64(105)
	if t := os.Getenv("AEYE_D2_THEME"); t != "" {
		n, perr := strconv.ParseInt(t, 10, 64)
		if perr != nil {
			return nil, fmt.Errorf("invalid AEYE_D2_THEME %q: %w", t, perr)
		}
		themeID = n
	}
	renderOpts := &d2svg.RenderOpts{
		Pad:     go2.Pointer(int64(d2svg.DEFAULT_PADDING)),
		ThemeID: &themeID,
		Sketch:  go2.Pointer(os.Getenv("AEYE_D2_SKETCH") != "0"), // on unless =0
	}
	// Leave Layout nil so d2 picks it up from the source's
	// `vars: { d2-config: { layout-engine: ... } }` (it only honors that when
	// Layout is unset), falling back to dagre. LayoutResolver routes the chosen
	// engine — including the in-file elk that keeps arrows off node labels.
	ctx := log.WithDefault(context.Background())
	diagram, _, err := d2lib.Compile(ctx, string(src), &d2lib.CompileOptions{
		Ruler:          ruler,
		LayoutResolver: layoutResolver,
		InputPath:      in,
	}, renderOpts)
	if err != nil {
		return nil, err
	}
	return d2svg.Render(diagram, renderOpts)
}

var (
	fontFamilyRe = regexp.MustCompile(`d2-[0-9]+-font-[a-z]+(?:-[a-z]+)*`)
	boldRe       = regexp.MustCompile(`\.text-bold \{(?:font-weight:bold;)?`)
	italicRe     = regexp.MustCompile(`\.text-italic \{(?:font-style:italic;)?`)
)

// fixFonts rewrites d2's embedded @font-face family names to an installed
// family and injects weight/style onto the bold/italic classes — resvg ignores
// @font-face, so without this every text label is dropped. Idempotent: the
// optional already-injected token lets a re-run match without doubling.
// (Port of the former d2-fix-fonts.sh.)
func fixFonts(svg []byte) []byte {
	family := os.Getenv("AEYE_D2_FONT")
	if family == "" {
		family = "Noto Sans"
	}
	svg = fontFamilyRe.ReplaceAllLiteral(svg, []byte(family))
	svg = boldRe.ReplaceAllLiteral(svg, []byte(".text-bold {font-weight:bold;"))
	svg = italicRe.ReplaceAllLiteral(svg, []byte(".text-italic {font-style:italic;"))
	return svg
}

// runRenderDiagram is the whole diagram pipeline in one call: compile the .d2
// via the embedded d2 library, rewrite fonts, contrast labels against their
// fills, write the processed SVG next to the PNG (the carousel's vector
// source), then rasterize to PNG with resvg.
func runRenderDiagram(in, out string) error {
	svg, err := renderD2SVG(in)
	if err != nil {
		return fmt.Errorf("compile %s: %w", in, err)
	}
	svg = contrastSVG(fixFonts(svg))

	svgPath := out[:len(out)-len(filepath.Ext(out))] + ".svg" // out has no ext -> out+".svg"
	if err := os.WriteFile(svgPath, svg, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", svgPath, err)
	}

	resvg := os.Getenv("AEYE_RESVG")
	if resvg == "" {
		resvg = "resvg"
	}
	var args []string
	if dir := os.Getenv("AEYE_D2_FONT_DIR"); dir != "" {
		args = append(args, "--skip-system-fonts", "--use-fonts-dir", dir)
	}
	args = append(args, svgPath, out)
	cmd := exec.Command(resvg, args...)
	if stderr, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("resvg: %w: %s", err, stderr)
	}
	return nil
}
