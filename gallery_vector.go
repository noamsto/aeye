package main

import (
	"bytes"
	"crypto/sha1" //nolint:gosec // sha1 only names cache files; not a security use
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/image/draw"
)

var (
	svgViewBoxRe = regexp.MustCompile(`viewBox="(-?[\d.]+) (-?[\d.]+) (-?[\d.]+) (-?[\d.]+)"`)
)

// closeTagIndex returns the index of the first '>' at or after from that lies
// outside a quoted attribute value, or -1. from must be outside any quote.
func closeTagIndex(b []byte, from int) int {
	var q byte
	for i := from; i < len(b); i++ {
		switch c := b[i]; {
		case q != 0:
			if c == q {
				q = 0
			}
		case c == '"' || c == '\'':
			q = c
		case c == '>':
			return i
		}
	}
	return -1
}

func isXMLSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

// sizeAttrLen returns the length of a leading width or height attribute
// (name, '=', quoted value) in b, or 0 if b does not start with one.
func sizeAttrLen(b []byte) int {
	var name int
	switch {
	case bytes.HasPrefix(b, []byte("width")):
		name = len("width")
	case bytes.HasPrefix(b, []byte("height")):
		name = len("height")
	default:
		return 0
	}
	i := name
	for i < len(b) && isXMLSpace(b[i]) {
		i++
	}
	if i >= len(b) || b[i] != '=' {
		return 0
	}
	i++
	for i < len(b) && isXMLSpace(b[i]) {
		i++
	}
	if i >= len(b) || (b[i] != '"' && b[i] != '\'') {
		return 0
	}
	j := bytes.IndexByte(b[i+1:], b[i])
	if j < 0 {
		return 0
	}
	return i + 1 + j + 1
}

// stripSizeAttrs removes width and height attributes from a start tag. It walks
// the tag outside quoted values, so attribute-like text inside a value (or a
// name such as data-width, or stroke-width) is left alone.
func stripSizeAttrs(tag []byte) []byte {
	out := make([]byte, 0, len(tag))
	var q byte
	for i := 0; i < len(tag); i++ {
		c := tag[i]
		switch {
		case q != 0:
			if c == q {
				q = 0
			}
		case c == '"' || c == '\'':
			q = c
		case isXMLSpace(c):
			if n := sizeAttrLen(tag[i+1:]); n > 0 {
				i += n // drop the whitespace and the attribute; the loop's i++ moves past it
				continue
			}
		}
		out = append(out, c)
	}
	return out
}

// svgRootNameLen returns the length of "<svg" or a namespace-prefixed "<p:svg" at
// the start of rest when followed by a tag delimiter, else 0.
func svgRootNameLen(rest []byte) int {
	n := 1 // past '<'
	for n < len(rest) && rest[n] != ':' && !isXMLSpace(rest[n]) && rest[n] != '>' && rest[n] != '/' {
		n++
	}
	if n < len(rest) && rest[n] == ':' {
		n++
	} else {
		n = 1 // no prefix: the name starts right after '<'
	}
	if !bytes.HasPrefix(rest[n:], []byte("svg")) || n+3 >= len(rest) {
		return 0
	}
	if c := rest[n+3]; isXMLSpace(c) || c == '>' || c == '/' {
		return n + 3
	}
	return 0
}

// rootTagEnd returns the index of the root <svg start tag and of the '>' closing
// it. The root is the first element, so the XML declaration, DOCTYPE and
// comments before it are skipped (a comment may mention "<svg"). ok is false
// when anything else comes first or the tag never closes.
func rootTagEnd(svg []byte) (start, gt int, ok bool) {
	for i := 0; i < len(svg); {
		j := bytes.IndexByte(svg[i:], '<')
		if j < 0 {
			return 0, 0, false
		}
		i += j
		rest := svg[i:]
		switch {
		case bytes.HasPrefix(rest, []byte("<!--")):
			k := bytes.Index(rest[4:], []byte("-->"))
			if k < 0 {
				return 0, 0, false
			}
			i += 4 + k + 3
		case bytes.HasPrefix(rest, []byte("<?")):
			k := bytes.Index(rest[2:], []byte("?>"))
			if k < 0 {
				return 0, 0, false
			}
			i += 2 + k + 2
		case bytes.HasPrefix(rest, []byte("<!")):
			// DOCTYPE: its '>' ends it unless an internal [subset] is open.
			depth, k := 0, 2
			for ; k < len(rest); k++ {
				if c := rest[k]; c == '[' {
					depth++
				} else if c == ']' {
					depth--
				} else if c == '>' && depth <= 0 {
					break
				}
			}
			if k >= len(rest) {
				return 0, 0, false
			}
			i += k + 1
		case svgRootNameLen(rest) > 0:
			gt = closeTagIndex(svg, i+svgRootNameLen(rest))
			return i, gt, gt >= 0
		default:
			return 0, 0, false
		}
	}
	return 0, 0, false
}

// cropViewBox rewrites the SVG's root viewBox to the crop sub-rectangle so
// resvg rasterizes only the visible window — keeping render cost flat at any
// zoom depth instead of scaling with 1/crop. The crop is normalized over the
// full canvas, the same space the bitmap layer samples, so the sharp render
// lands pixel-aligned over the instant preview. The root tag also loses its
// width and height, so resvg sizes the canvas from the crop's viewBox (its
// aspect) instead of fitting the crop inside the full-size canvas. Nested svg
// elements keep their sizes. Returns (svg, false) unchanged unless the root
// start tag itself carries the viewBox: a viewBox on a nested marker, symbol or
// svg would not crop the render, so the caller must not treat it as cropped.
func cropViewBox(svg []byte, c cropFrac) ([]byte, bool) {
	start, gt, ok := rootTagEnd(svg)
	if !ok {
		return svg, false
	}
	m := svgViewBoxRe.FindSubmatchIndex(svg[start:gt])
	if m == nil {
		return svg, false
	}
	loc := make([]int, len(m))
	for i, v := range m {
		loc[i] = start + v
	}
	num := func(pair int) float64 {
		v, _ := strconv.ParseFloat(string(svg[loc[2*pair]:loc[2*pair+1]]), 64)
		return v
	}
	minX, minY, vbW, vbH := num(1), num(2), num(3), num(4)
	repl := fmt.Sprintf(`viewBox="%.4f %.4f %.4f %.4f"`,
		minX+c.x0*vbW, minY+c.y0*vbH, c.w()*vbW, c.h()*vbH)

	tag := make([]byte, 0, gt-start+len(repl))
	tag = append(tag, svg[start:loc[0]]...)
	tag = append(tag, repl...)
	tag = append(tag, svg[loc[1]:gt]...)
	tag = stripSizeAttrs(tag)

	out := make([]byte, 0, len(svg)+len(repl))
	out = append(out, svg[:start]...)
	out = append(out, tag...)
	out = append(out, svg[gt:]...)
	return out, true
}

// maxVectorHeight bounds the sharp render's height: twice the producer cap, so a
// tall diagram at preview width still has headroom before it starts shrinking.
const maxVectorHeight = 2 * maxRasterSide

// renderVector rasterizes the crop window of the SVG to a scratch PNG at targetW
// pixels wide via resvg, caching on (svg, mtime, crop, targetW). Returns the PNG
// path, or "" if resvg is absent or the render fails (caller falls back to the
// bitmap crop). When zoomed, only the crop region is rendered (see cropViewBox).
func renderVector(vector string, crop cropFrac, targetW int) string {
	fi, err := os.Stat(vector)
	if err != nil {
		return ""
	}
	bin := resvgBin()
	// One cached PNG per (svg, mtime); crop + width are the filename suffix so we
	// can evict prior framings — /tmp then holds at most one vector scratch per
	// diagram, not one per pan/zoom step.
	stem := fmt.Sprintf("aeye-vec-%x", sha1.Sum([]byte(fmt.Sprintf("%s|%d", vector, fi.ModTime().UnixNano())))) //nolint:gosec // sha1 only names cache files; not a security use
	out := filepath.Join(os.TempDir(), fmt.Sprintf("%s-%.4f_%.4f_%.4f_%.4f-%d.png",
		stem, crop.x0, crop.y0, crop.x1, crop.y1, targetW))
	if _, err := os.Stat(out); err == nil {
		return out
	}
	if _, err := exec.LookPath(bin); err != nil {
		return ""
	}
	// A zoomed render needs the crop applied to the root viewBox; without it resvg
	// would render the whole diagram under a frame of the wrong region, so fall
	// back to the bitmap crop. Checked before evicting the cached framings.
	var cropped []byte
	if !crop.isFull() {
		data, err := os.ReadFile(vector) //nolint:gosec // path is the user's own image/manifest file
		if err != nil {
			return ""
		}
		var ok bool
		if cropped, ok = cropViewBox(data, crop); !ok {
			return ""
		}
	}
	// Evict stale framings for this svg before rendering the new one.
	olds, _ := filepath.Glob(filepath.Join(os.TempDir(), stem+"-*.png"))
	for _, o := range olds {
		os.Remove(o) //nolint:errcheck,gosec // best-effort temp cleanup
	}
	src := vector
	if cropped != nil {
		// Crop in the name too: two debounced renders of the same diagram at
		// different framings can overlap, and a shared name would race (one
		// overwriting the other's viewBox, or unlinking it before resvg opens it).
		tmp := filepath.Join(os.TempDir(), fmt.Sprintf("%s-%.4f_%.4f_%.4f_%.4f-crop.svg",
			stem, crop.x0, crop.y0, crop.x1, crop.y1))
		if err := os.WriteFile(tmp, cropped, 0o644); err != nil { //nolint:gosec // scratch SVG in tmp for the local rasterizer
			return ""
		}
		defer os.Remove(tmp) //nolint:errcheck // best-effort temp cleanup
		src = tmp
	}
	// The height bound only binds on a crop taller than maxVectorHeight at
	// targetW (e.g. a 100x8000 svg at 1600 wide would be ~0.8 GB); resvg then
	// fits within both. Anything shorter renders byte-identical to --width alone.
	args := append(resvgFontArgs(), "--width", strconv.Itoa(targetW), "--height", strconv.Itoa(maxVectorHeight), src, out)
	if err := exec.Command(bin, args...).Run(); err != nil { //nolint:gosec // args built by the tool, not shell-interpreted
		os.Remove(out) //nolint:errcheck,gosec // best-effort temp cleanup
		return ""
	}
	return out
}

// vectorReadyMsg carries a finished crop raster back to Update. vector/crop
// identify which request it answers, so a stale render (the selection or framing
// moved on while resvg ran) is ignored.
type vectorReadyMsg struct {
	vector   string
	crop     cropFrac
	raster   image.Image
	animates bool // the SVG has animated edges or aeye-* hooks to play
}

// renderVectorCmd rasterizes off the event loop (resvg subprocess) and decodes
// the result, so the TUI never blocks on a render. Returns nil on any failure.
func renderVectorCmd(vector string, crop cropFrac, targetW int) tea.Cmd {
	return func() tea.Msg {
		out := renderVector(vector, crop, targetW)
		if out == "" {
			return nil
		}
		f, err := os.Open(out) //nolint:gosec // path is the user's own image/manifest file
		if err != nil {
			return nil
		}
		defer f.Close() //nolint:errcheck // read-only file or already-failing path; close error is not actionable
		img, _, err := image.Decode(f)
		if err != nil {
			return nil
		}
		return vectorReadyMsg{vector: vector, crop: crop, raster: img, animates: vectorAnimates(vector)}
	}
}

// vectorKickMsg fires after the debounce window; only the latest generation's
// message survives to actually kick a render.
type vectorKickMsg struct{ gen uint64 }

// vectorDebounce coalesces a pan/zoom keystroke burst into a single sharp render
// of the final framing — below the threshold where waiting feels laggy, while
// the instant bitmap crop covers the gap.
const vectorDebounce = 70 * time.Millisecond

// curVector returns the selected entry's vector source path, or "" if it has none.
func (m *galleryModel) curVector() string {
	if len(m.images) == 0 {
		return ""
	}
	return m.images[m.cursor].Vector
}

// scheduleVector arms a debounced sharp re-render: it bumps the generation and
// fires a tick carrying it. A later keystroke bumps again, so an earlier tick
// arrives stale and is dropped — only the final framing reaches resvg. Returns
// nil when there's nothing to sharpen (no vector / not kitty).
func (m *galleryModel) scheduleVector() tea.Cmd {
	if m.curVector() == "" || m.backend != backendKitty {
		return nil
	}
	m.vecGen++
	g := m.vecGen
	return tea.Tick(vectorDebounce, func(time.Time) tea.Msg { return vectorKickMsg{gen: g} })
}

// kickVector returns the async render cmd for the current d2 selection at the
// current crop, or nil when there is nothing to sharpen (no vector / not kitty).
func (m *galleryModel) kickVector() tea.Cmd {
	v := m.curVector()
	if v == "" || m.backend != backendKitty {
		return nil
	}
	return renderVectorCmd(v, m.crop, m.l.previewW*m.cellWpx())
}

// fitToBox scales src to fit tw×th preserving aspect, upscaling if smaller — the
// rest-state path for small diagrams (vector has no upscale ceiling).
func fitToBox(src image.Image, tw, th int) image.Image {
	b := src.Bounds()
	scale := min(float64(tw)/float64(b.Dx()), float64(th)/float64(b.Dy()))
	dst := image.NewRGBA(image.Rect(0, 0, int(float64(b.Dx())*scale), int(float64(b.Dy())*scale)))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	return dst
}
