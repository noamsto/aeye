package main

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// region is one navigable diagram object, bbox in source fractions (0..1).
type region struct {
	path           string
	x0, y0, x1, y1 float64
}

func (r region) cx() float64 { return (r.x0 + r.x1) / 2 }
func (r region) cy() float64 { return (r.y0 + r.y1) / 2 }

var (
	// A connection's id is "(src -> dst)[n]"; nested in a container it's
	// "parent.(src -> dst)[n]". Match the "(...)[n]" token at the end so nested
	// connections are filtered too, not just top-level ones.
	connPathRe  = regexp.MustCompile(`\(.*\)\[\d+\]$`)
	diagramIDRe = regexp.MustCompile(`^d2-\d+`)
)

// viewBox is an SVG coordinate space: origin (x,y) plus width/height.
type viewBox struct{ x, y, w, h float64 }

func parseViewBox(s string) (viewBox, bool) {
	f := strings.Fields(s)
	if len(f) != 4 {
		return viewBox{}, false
	}
	var v [4]float64
	for i := range f {
		n, err := strconv.ParseFloat(f[i], 64)
		if err != nil {
			return viewBox{}, false
		}
		v[i] = n
	}
	return viewBox{v[0], v[1], v[2], v[3]}, true
}

// parseRegions extracts each container/shape group as a region with a normalized
// bbox. d2 emits every object as a flat <g class="<base64-id> [styles]"> whose
// geometry (<path>/<rect>/<ellipse>/<circle>/<text>) follows in document order
// until the next such group, so we attribute each geometry element to the most
// recently opened decodable group. Groups whose id isn't a decodable dotted
// object path, connection groups, and the diagram-id group are skipped. Returns
// nil if nothing parses.
func parseRegions(data []byte) []region {
	type group struct {
		path                   string
		minX, minY, maxX, maxY float64
		set                    bool
	}
	var groups []*group
	var cur *group
	add := func(x0, y0, x1, y1 float64) {
		if cur == nil {
			return
		}
		if !cur.set {
			cur.minX, cur.minY, cur.maxX, cur.maxY, cur.set = x0, y0, x1, y1, true
			return
		}
		cur.minX, cur.minY = math.Min(cur.minX, x0), math.Min(cur.minY, y0)
		cur.maxX, cur.maxY = math.Max(cur.maxX, x1), math.Max(cur.maxY, y1)
	}

	// D2 wraps diagram content in an inner <svg class="… d2-svg" viewBox="minX minY w h">
	// whose viewBox defines the coordinate space for all geometry. Prefer it; fall
	// back to the first viewBox seen (plain D2, no sketch overlay). A <marker>
	// viewBox is never first — it follows the root <svg> — so it can't be picked.
	var box, firstBox viewBox
	var haveBox, haveFirst bool

	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false // d2 SVGs carry namespaces and HTML-ish label markup.
	for {
		tok, err := dec.Token()
		if err != nil {
			break // EOF, or a malformed tail — emit whatever parsed cleanly so far.
		}
		el, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		at := func(name string) string {
			for _, a := range el.Attr {
				if a.Name.Local == name {
					return a.Value
				}
			}
			return ""
		}
		num := func(name string) (float64, bool) {
			v, err := strconv.ParseFloat(at(name), 64)
			return v, err == nil
		}
		tx, ty := translateOf(at("transform"))

		switch el.Name.Local {
		case "mask", "defs":
			// d2 closes the diagram with a <mask> wrapping a full-canvas <rect>;
			// without this, that rect attributes to the last object group and
			// balloons its bbox to the whole canvas. <defs> (filter/marker defs)
			// holds no object geometry either.
			cur = nil
		case "svg":
			if vb, ok := parseViewBox(at("viewBox")); ok {
				if !haveFirst {
					firstBox, haveFirst = vb, true
				}
				if strings.Contains(at("class"), "d2-svg") {
					box, haveBox = vb, true
				}
			}
		case "g":
			// Only a decodable id opens a new geometry segment. Style groups
			// (e.g. the inner <g class="shape"> that wraps each object's shapes)
			// must leave the current group intact, or their geometry is lost.
			fields := strings.Fields(at("class"))
			if len(fields) == 0 {
				continue
			}
			id, err := base64.StdEncoding.DecodeString(fields[0])
			if err != nil {
				continue
			}
			g := &group{path: string(id)}
			groups = append(groups, g)
			cur = g
		case "path":
			if x0, y0, x1, y1, ok := pathBBox(at("d")); ok {
				add(x0+tx, y0+ty, x1+tx, y1+ty)
			}
		case "rect":
			if x, ok := num("x"); ok {
				y, _ := num("y")
				w, _ := num("width")
				h, _ := num("height")
				add(x+tx, y+ty, x+w+tx, y+h+ty)
			}
		case "ellipse", "circle":
			if cx, ok := num("cx"); ok {
				cy, _ := num("cy")
				rx, _ := num("rx")
				if rx == 0 {
					rx, _ = num("r")
				}
				ry, _ := num("ry")
				if ry == 0 {
					ry = rx
				}
				add(cx-rx+tx, cy-ry+ty, cx+rx+tx, cy+ry+ty)
			}
		case "text":
			// d2 draws a container's title ABOVE its shape; framing to the shape
			// alone clips it. The baseline (x,y) plus the font ascent covers the glyph.
			if x, ok := num("x"); ok {
				y, _ := num("y")
				fs := 16.0
				if m := fontSizeRe.FindStringSubmatch(at("style")); m != nil {
					fs, _ = strconv.ParseFloat(m[1], 64)
				}
				add(x+tx, y-fs+ty, x+tx, y+ty)
			}
		}
	}

	if !haveBox {
		box = firstBox
	}
	if box.w <= 0 || box.h <= 0 {
		return nil
	}

	var out []region
	for _, g := range groups {
		if g.path == "" || !g.set || connPathRe.MatchString(g.path) || diagramIDRe.MatchString(g.path) || !isObjectPath(g.path) {
			continue
		}
		out = append(out, region{
			path: g.path,
			x0:   (g.minX - box.x) / box.w,
			y0:   (g.minY - box.y) / box.h,
			x1:   (g.maxX - box.x) / box.w,
			y1:   (g.maxY - box.y) / box.h,
		})
	}
	return out
}

// isObjectPath rejects junk classes; a d2 object path is dot-separated non-empty
// segments. (Connections/diagram-id are filtered separately by the caller.)
func isObjectPath(p string) bool {
	for _, seg := range strings.Split(p, ".") {
		if seg == "" {
			return false
		}
	}
	return true
}

var (
	translateRe = regexp.MustCompile(`translate\(\s*(-?[\d.]+)(?:[ ,]+(-?[\d.]+))?`)
	fontSizeRe  = regexp.MustCompile(`font-size:\s*([\d.]+)`)
)

// translateOf reads translate(tx[,ty]) from a transform attribute value. d2
// positions most node shapes with local path coords + a translate; the cylinder
// uses absolute coords + no transform. Defaulting to (0,0) handles both.
func translateOf(transform string) (tx, ty float64) {
	m := translateRe.FindStringSubmatch(transform)
	if m == nil {
		return 0, 0
	}
	tx, _ = strconv.ParseFloat(m[1], 64)
	if m[2] != "" {
		ty, _ = strconv.ParseFloat(m[2], 64)
	}
	return
}

// regionTree indexes regions by their parent path so drilling is a lookup.
type regionTree struct {
	byParent map[string][]region // parent path ("" = root) → spatially ordered children
}

func newRegionTree(rs []region) *regionTree {
	t := &regionTree{byParent: map[string][]region{}}
	for _, r := range rs {
		parent := ""
		if i := strings.LastIndex(r.path, "."); i >= 0 {
			parent = r.path[:i]
		}
		t.byParent[parent] = append(t.byParent[parent], r)
	}
	for k := range t.byParent {
		sortSpatial(t.byParent[k])
	}
	return t
}

// childrenOf returns the regions directly under the given drill path (nil/empty
// = root level), in spatial reading order.
func (t *regionTree) childrenOf(path []string) []region {
	return t.byParent[strings.Join(path, ".")]
}

const framePadding = 1.1 // ~10% margin around the framed region

// boxAspectFrac returns the crop fraction aspect (cropW_frac / cropH_frac) whose
// *pixel* aspect equals the box's, i.e. (cropW·srcW)/(cropH·srcH) == boxW/boxH.
// A crop shaped to this ratio fills the box with no letterboxing.
func boxAspectFrac(srcW, srcH, boxW, boxH int) float64 {
	return (float64(boxW) * float64(srcH)) / (float64(boxH) * float64(srcW))
}

// frameRegion returns the crop (source fractions) that frames r to the preview
// box. It matches the crop's *pixel* aspect to the box (the crop is letterboxed
// into the box, so this fills it) by folding in the source aspect, then takes
// the smallest such rect containing r (with padding), centered on r, clamped to
// [0,1].
func frameRegion(r region, srcW, srcH, boxW, boxH int) cropFrac {
	rw, rh := (r.x1-r.x0)*framePadding, (r.y1-r.y0)*framePadding
	targetFrac := boxAspectFrac(srcW, srcH, boxW, boxH)
	cropW, cropH := rw, rh
	if rw/rh < targetFrac {
		cropW = rh * targetFrac
	} else {
		cropH = rw / targetFrac
	}
	// If matching the box aspect overflows the image, the region is too tall (or
	// wide) to fill the box without cropping it — keep that axis tight to the
	// region so it stays centered with symmetric letterbox, rather than padding
	// out to the full image and stranding the region against an empty margin.
	if cropW > 1 {
		cropW = math.Min(rw, 1)
	}
	if cropH > 1 {
		cropH = math.Min(rh, 1)
	}
	x0 := clampF(r.cx()-cropW/2, 0, 1-cropW)
	y0 := clampF(r.cy()-cropH/2, 0, 1-cropH)
	return cropFrac{x0, y0, x0 + cropW, y0 + cropH}
}

// sortSpatial orders regions so Tab advances in reading order along the group's
// dominant flow: left-to-right for a wider-than-tall layout, top-to-bottom
// otherwise. Picking the primary axis by spread keeps a horizontal flow (where a
// node can sit lower than its neighbours) from sorting by row first. The 0.05
// band tolerates minor misalignment on the secondary axis.
func sortSpatial(rs []region) {
	var minX, minY = 1.0, 1.0
	var maxX, maxY = 0.0, 0.0
	for _, r := range rs {
		minX, maxX = math.Min(minX, r.cx()), math.Max(maxX, r.cx())
		minY, maxY = math.Min(minY, r.cy()), math.Max(maxY, r.cy())
	}
	horizontal := (maxX - minX) > (maxY - minY)
	sort.SliceStable(rs, func(i, j int) bool {
		a, b := rs[i], rs[j]
		if horizontal {
			if math.Abs(a.cx()-b.cx()) > 0.05 {
				return a.cx() < b.cx()
			}
			return a.cy() < b.cy()
		}
		if math.Abs(a.cy()-b.cy()) > 0.05 {
			return a.cy() < b.cy()
		}
		return a.cx() < b.cx()
	})
}

// focusedRegion returns the region currently focused at the drill level.
func (m *galleryModel) focusedRegion() (region, bool) {
	if m.regions == nil || m.regionIdx < 0 {
		return region{}, false
	}
	sibs := m.regions.childrenOf(m.regionPath)
	if m.regionIdx >= len(sibs) {
		return region{}, false
	}
	return sibs[m.regionIdx], true
}

// cycleRegion focuses the next (+1) sibling at the current level, wrapping. The
// first call (regionIdx<0) enters region mode at index 0. Stepping back (-1) off
// the first sibling returns to the whole diagram. Frames the focus.
func (m *galleryModel) cycleRegion(dir int) {
	sibs := m.regions.childrenOf(m.regionPath)
	if len(sibs) == 0 {
		return
	}
	if dir < 0 && m.regionIdx == 0 {
		m.exitRegions()
		return
	}
	if m.regionIdx < 0 {
		m.regionIdx = 0
	} else {
		m.regionIdx = (m.regionIdx + dir + len(sibs)) % len(sibs)
	}
	m.frameFocused()
}

// drillIn descends into the focused container so its children become the cycle
// set; no-op when the focus is a leaf.
func (m *galleryModel) drillIn() {
	r, ok := m.focusedRegion()
	if !ok {
		return
	}
	child := strings.Split(r.path, ".")
	if len(m.regions.childrenOf(child)) == 0 {
		return // leaf
	}
	m.regionPath = child
	m.regionIdx = 0
	m.frameFocused()
}

// drillOut ascends to the parent level, re-focusing the container we came from.
func (m *galleryModel) drillOut() {
	if len(m.regionPath) == 0 {
		return
	}
	came := strings.Join(m.regionPath, ".")
	m.regionPath = m.regionPath[:len(m.regionPath)-1]
	sibs := m.regions.childrenOf(m.regionPath)
	m.regionIdx = 0
	for i, r := range sibs {
		if r.path == came {
			m.regionIdx = i
			break
		}
	}
	m.frameFocused()
}

// exitRegions leaves region mode and resets to fit-all.
func (m *galleryModel) exitRegions() {
	m.regionPath, m.regionIdx = nil, -1
	m.resetZoom()
}

// frameFocused sets the crop to frame the focused region, using the decoded
// image's pixel dims as the source size. No-op when nothing is decoded.
func (m *galleryModel) frameFocused() {
	r, ok := m.focusedRegion()
	if !ok || m.curImg == nil {
		return
	}
	b := m.curImg.Bounds()
	m.crop = frameRegion(r, b.Dx(), b.Dy(), m.l.previewW*cellPxW, m.l.previewH*cellPxH)
}

// ensureRegions parses the current d2 entry's SVG into m.regions on first use.
// No vector / not kitty / nothing parses → m.regions stays nil (keys no-op).
func (m *galleryModel) ensureRegions() {
	if m.regions != nil || m.backend != backendKitty {
		return
	}
	v := m.curVector()
	if v == "" {
		return
	}
	data, err := os.ReadFile(v)
	if err != nil {
		return
	}
	rs := parseRegions(data)
	if len(rs) == 0 {
		return
	}
	m.regions = newRegionTree(rs)
}
