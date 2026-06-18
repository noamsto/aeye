package main

import (
	"regexp"
	"strconv"

	"oss.terrastruct.com/d2/d2graph"
	"oss.terrastruct.com/d2/d2target"
)

// Soft ink endpoints — high contrast without the harshness of pure black/white,
// keeping the sketch register.
const (
	contrastDarkInk  = "#13111C"
	contrastLightInk = "#F5F5F5"
)

// hexFillRe matches a #RRGGBB fill. Named or gradient fills are left to d2.
var hexFillRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// contrastLabels recolors a shape's label to contrast its fill, but only for
// shapes the user explicitly filled without also picking a font color — d2's
// theme already chooses a readable label color for its own shapes, and
// overriding those would flatten the theme's text palette.
//
// It works on the compiled graph + diagram rather than the rendered SVG, so
// fills are resolved color values with none of the class-vs-hex ambiguity the
// old SVG-regex pass tripped over. d2's exporter builds diagram.Shapes 1:1 from
// graph.Objects with shape.ID == obj.AbsID(), so a shape is user-filled exactly
// when its object's Style.Fill is set.
func contrastLabels(diagram *d2target.Diagram, graph *d2graph.Graph) {
	userFilled := make(map[string]bool, len(graph.Objects))
	for _, obj := range graph.Objects {
		if obj.Style.Fill != nil && obj.Style.FontColor == nil {
			userFilled[obj.AbsID()] = true
		}
	}
	for i := range diagram.Shapes {
		s := &diagram.Shapes[i]
		if userFilled[s.ID] && hexFillRe.MatchString(s.Fill) {
			s.Color = contrastInk(s.Fill)
		}
	}
}

// contrastInk returns the ink — dark or light — that reads best on hexFill,
// chosen by relative luminance.
func contrastInk(hexFill string) string {
	r, _ := strconv.ParseInt(hexFill[1:3], 16, 0)
	g, _ := strconv.ParseInt(hexFill[3:5], 16, 0)
	b, _ := strconv.ParseInt(hexFill[5:7], 16, 0)
	l := (0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)) / 255
	if l > 0.5 {
		return contrastDarkInk
	}
	return contrastLightInk
}
