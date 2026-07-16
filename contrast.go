package main

import (
	"oss.terrastruct.com/d2/d2graph"
	"oss.terrastruct.com/d2/d2target"
	"oss.terrastruct.com/d2/lib/color"
	"oss.terrastruct.com/d2/lib/label"
)

// Soft ink endpoints — high contrast without the harshness of pure black/white,
// keeping the sketch register.
const (
	contrastDarkInk  = "#13111C"
	contrastLightInk = "#F5F5F5"
)

// resolvableFill reports whether a user fill resolves to a concrete RGB color —
// true for hex (#rgb, #rrggbb), named CSS colors (lightgreen), and rgb()/hsl().
// Gradients (url(...)) and anything else csscolorparser can't parse stay false,
// so their labels keep the theme color, as before.
func resolvableFill(fill string) bool {
	_, err := color.Luminance(fill)
	return err == nil
}

// contrastLabels recolors a shape's label to contrast its fill, but only for
// shapes the user explicitly filled without also picking a font color — d2's
// theme already chooses a readable label color for its own shapes, and
// overriding those would flatten the theme's text palette.
//
// It works on the compiled graph + diagram rather than the rendered SVG, so
// fills are resolved color values with none of the class-vs-hex ambiguity the
// old SVG-regex pass tripped over. d2's exporter builds diagram.Shapes/Connections
// 1:1 from graph.Objects/Edges with matching AbsID, so a shape is user-filled
// exactly when its object's Style.Fill is set.
//
// Edge labels are handled too: an edge label is drawn inside the lowest container
// shared by both ends, so if that container (or a filled ancestor) is user-filled
// the label — which keeps its theme color — would otherwise sit light-on-light.
func contrastLabels(diagram *d2target.Diagram, graph *d2graph.Graph) {
	fill := make(map[string]string, len(graph.Objects))
	for _, obj := range graph.Objects {
		if obj.Style.Fill != nil && obj.Style.FontColor == nil && resolvableFill(obj.Style.Fill.Value) {
			fill[obj.AbsID()] = obj.Style.Fill.Value
		}
	}
	for i := range diagram.Shapes {
		s := &diagram.Shapes[i]
		// A container's label is drawn outside its body (above it, on the canvas),
		// not on the fill — so contrast it against the canvas, i.e. leave the theme
		// color. Only labels sitting on the fill (leaf shapes, centered) get inked.
		if f, ok := fill[s.ID]; ok && !label.FromString(s.LabelPosition).IsOutside() {
			s.Color = contrastInk(f)
		}
	}

	edges := make(map[string]*d2graph.Edge, len(graph.Edges))
	for _, e := range graph.Edges {
		edges[e.AbsID()] = e
	}
	for i := range diagram.Connections {
		e := edges[diagram.Connections[i].ID]
		if e == nil || e.Style.FontColor != nil {
			continue
		}
		if f, ok := enclosingFill(commonContainer(e.Src, e.Dst), fill); ok {
			diagram.Connections[i].Color = contrastInk(f)
		}
	}
}

// commonContainer returns the lowest object that contains both a and b.
func commonContainer(a, b *d2graph.Object) *d2graph.Object {
	for p := a; p != nil; p = p.Parent {
		if b == p || b.IsDescendantOf(p) {
			return p
		}
	}
	return nil
}

// enclosingFill walks up from obj (inclusive) and returns the fill of the nearest
// ancestor the user filled — the color behind a label drawn at obj's level.
func enclosingFill(obj *d2graph.Object, fill map[string]string) (string, bool) {
	for p := obj; p != nil; p = p.Parent {
		if f, ok := fill[p.AbsID()]; ok {
			return f, true
		}
	}
	return "", false
}

// contrastInk returns the ink — dark or light — that reads best on fill, chosen
// by relative luminance. fill is any color resolvableFill accepts; a fill that
// somehow fails to parse scores 0 and takes the light ink.
func contrastInk(fill string) string {
	l, _ := color.Luminance(fill) // in [0,1]
	if l > 0.5 {
		return contrastDarkInk
	}
	return contrastLightInk
}
