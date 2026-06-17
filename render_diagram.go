package main

import (
	"context"
	"os"

	"oss.terrastruct.com/d2/d2graph"
	"oss.terrastruct.com/d2/d2layouts/d2dagrelayout"
	"oss.terrastruct.com/d2/d2layouts/d2elklayout"
	"oss.terrastruct.com/d2/d2lib"
	"oss.terrastruct.com/d2/d2renderers/d2svg"
	"oss.terrastruct.com/d2/d2themes/d2themescatalog"
	"oss.terrastruct.com/d2/lib/log"
	"oss.terrastruct.com/d2/lib/textmeasure"
	"oss.terrastruct.com/util-go/go2"
)

// renderD2SVG compiles a .d2 file to SVG bytes via the embedded d2 library,
// matching the carousel's old CLI invocation (`d2 -t 200 --sketch`). The
// in-file `vars: { d2-config: ... }` still overrides these (e.g. the
// layout-engine: elk that fixes arrows crossing node labels).
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
	themeID := d2themescatalog.DarkMauve.ID
	renderOpts := &d2svg.RenderOpts{
		Pad:     go2.Pointer(int64(d2svg.DEFAULT_PADDING)),
		ThemeID: &themeID,
		Sketch:  go2.Pointer(true),
	}
	ctx := log.WithDefault(context.Background())
	diagram, _, err := d2lib.Compile(ctx, string(src), &d2lib.CompileOptions{
		Ruler:          ruler,
		Layout:         go2.Pointer("dagre"),
		LayoutResolver: layoutResolver,
		InputPath:      in,
	}, renderOpts)
	if err != nil {
		return nil, err
	}
	return d2svg.Render(diagram, renderOpts)
}

func runRenderSVG(in, out string) error {
	svg, err := renderD2SVG(in)
	if err != nil {
		return err
	}
	return os.WriteFile(out, svg, 0o644)
}
