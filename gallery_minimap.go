package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/draw"
)

// minimapMinSide is the smallest viewport rectangle side in px: an outer black and
// an inner white ring per side leave a 2px hollow, so the rectangle stays visible.
const minimapMinSide = 6

// minimapFit is src scaled to fit box, preserving aspect. Each side is at least 1px.
func minimapFit(src, box image.Point) image.Point {
	if src.X <= 0 || src.Y <= 0 || box.X <= 0 || box.Y <= 0 {
		return image.Point{}
	}
	scale := math.Min(float64(box.X)/float64(src.X), float64(box.Y)/float64(src.Y))
	return image.Pt(
		max(1, int(math.Round(float64(src.X)*scale))),
		max(1, int(math.Round(float64(src.Y)*scale))),
	)
}

// minimapRect is the crop's pixel rectangle inside a fit-sized raster. An axis
// thinner than minimapMinSide is widened about the crop's centre and clamped
// inside the raster, so a deep zoom still leaves a visible rectangle.
func minimapRect(fit image.Point, c cropFrac) image.Rectangle {
	r := cropPixels(image.Rect(0, 0, fit.X, fit.Y), c)
	r.Min.X, r.Max.X = widenAxis(r.Min.X, r.Max.X, int(math.Round(c.cx()*float64(fit.X))), fit.X)
	r.Min.Y, r.Max.Y = widenAxis(r.Min.Y, r.Max.Y, int(math.Round(c.cy()*float64(fit.Y))), fit.Y)
	return r
}

func widenAxis(lo, hi, center, limit int) (int, int) {
	if hi-lo >= minimapMinSide {
		return lo, hi
	}
	size := min(minimapMinSide, limit)
	lo = clamp(center-size/2, 0, limit-size)
	return lo, lo + size
}

// drawViewport strokes r into dst as a 1px black ring with a 1px white ring just
// inside it, so the rectangle reads on both light and dark thumbnails. r is
// clipped to dst; Pix is written directly because this runs on every pan frame.
func drawViewport(dst *image.RGBA, r image.Rectangle) {
	strokeRing(dst, r, color.RGBA{0, 0, 0, 255})
	strokeRing(dst, r.Inset(1), color.RGBA{255, 255, 255, 255})
}

func strokeRing(dst *image.RGBA, r image.Rectangle, c color.RGBA) {
	if r.Empty() {
		return
	}
	fillRect(dst, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+1), c)
	fillRect(dst, image.Rect(r.Min.X, r.Max.Y-1, r.Max.X, r.Max.Y), c)
	fillRect(dst, image.Rect(r.Min.X, r.Min.Y+1, r.Min.X+1, r.Max.Y-1), c)
	fillRect(dst, image.Rect(r.Max.X-1, r.Min.Y+1, r.Max.X, r.Max.Y-1), c)
}

func fillRect(dst *image.RGBA, r image.Rectangle, c color.RGBA) {
	r = r.Intersect(dst.Bounds())
	if r.Empty() {
		return
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		i := dst.PixOffset(r.Min.X, y)
		for end := i + r.Dx()*4; i < end; i += 4 {
			dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = c.R, c.G, c.B, c.A
		}
	}
}

// minimapBox is the thumb's own box in px. Kitty fits the placement into the thumb's
// cell box, so a raster of exactly minimapFit(src, box) px lands 1:1 and kitty
// supplies the letterbox. The preview's aspect must never enter here.
func (m *galleryModel) minimapBox() image.Point {
	return image.Pt(m.l.stripW*m.cellWpx(), m.l.stripH*m.cellHpx())
}

// minimapRawPath and minimapPNGPath are per-pane scratch files, distinct from the
// zoom ones so the thumb and the preview never overwrite each other's raster.
func (m *galleryModel) minimapRawPath() string {
	return filepath.Join(os.TempDir(), "aeye-minimap-"+strings.TrimPrefix(m.pane, "%")+".raw")
}

func (m *galleryModel) minimapPNGPath() string {
	return filepath.Join(os.TempDir(), "aeye-minimap-"+strings.TrimPrefix(m.pane, "%")+".png")
}

// minimapStamp names what the selected filmstrip slot holds, or should hold. Two
// equal stamps mean the same pixels, so syncMinimap skips the re-store.
type minimapStamp struct {
	id      int
	path    string
	mtime   int64
	overlay bool            // false = the plain thumb
	fit     image.Point     // raster size; zero unless overlay
	rect    image.Rectangle // viewport rectangle in that raster; zero unless overlay
}

// minimapBaseKey identifies the scaled thumb an overlay is drawn on. Comparable,
// so the per-frame check allocates nothing.
type minimapBaseKey struct {
	path  string
	mtime int64
	fit   image.Point
}

func (s minimapStamp) baseKey() minimapBaseKey {
	return minimapBaseKey{path: s.path, mtime: s.mtime, fit: s.fit}
}

// minimapState is the selected thumb's viewport-overlay bookkeeping. base and
// frame are reused across pan frames so a steady-state overlay allocates no pixels.
type minimapState struct {
	stored    minimapStamp // what the selected slot holds now; zero = unknown
	baseKey   minimapBaseKey
	base      *image.RGBA    // selected thumb scaled to fit, no outline
	frame     *image.RGBA    // base plus the outline; same bounds as base
	failedKey minimapBaseKey // base that could not be built; not retried until the key changes
}

// minimapWant is the stamp the selected slot should hold now. It does no pixel work.
func (m *galleryModel) minimapWant() minimapStamp {
	if m.cursor >= len(m.images) {
		return minimapStamp{}
	}
	e := m.images[m.cursor]
	w := minimapStamp{
		id:    m.stripID(m.cursor - stripStart(m.cursor, m.l.stripCols, len(m.images))),
		path:  e.Path,
		mtime: e.Mtime,
	}
	if m.crop.isFull() || m.curSize == (image.Point{}) {
		return w
	}
	fit := minimapFit(m.curSize, m.minimapBox())
	if fit == (image.Point{}) {
		return w
	}
	o := w
	o.overlay, o.fit = true, fit
	if o.baseKey() == m.mini.failedKey {
		return w
	}
	o.rect = minimapRect(fit, m.crop)
	return o
}

// selectedThumbStore is the APC that puts w into the selected slot, and the stamp
// the slot then holds (plain when the overlay could not be produced).
func (m *galleryModel) selectedThumbStore(w minimapStamp) (string, minimapStamp) {
	if w.overlay {
		if apc := m.overlayStore(w); apc != "" {
			return apc, w
		}
		w.overlay, w.fit, w.rect = false, image.Point{}, image.Rectangle{}
	}
	return transmitVirtual(w.id, cachedPNG(w.path, m.l.stripW, m.l.stripH), m.l.stripW, m.l.stripH), w
}

// overlayStore renders w's viewport rectangle onto the scaled thumb and returns
// its store, or "" when no raster could be built or written.
func (m *galleryModel) overlayStore(w minimapStamp) string {
	if key := w.baseKey(); key != m.mini.baseKey {
		base := m.minimapBase(w)
		if base == nil {
			tracef("minimap base failed: %s", w.path)
			m.mini.failedKey = key
			return ""
		}
		m.mini.base, m.mini.baseKey = base, key
	}
	if m.mini.frame == nil || m.mini.frame.Rect != m.mini.base.Rect {
		m.mini.frame = image.NewRGBA(m.mini.base.Rect)
	}
	copy(m.mini.frame.Pix, m.mini.base.Pix)
	drawViewport(m.mini.frame, w.rect)
	if !preferEncodedFrame(m.bridged) {
		if out := writeRaw(m.minimapRawPath(), m.mini.frame); out != "" {
			return transmitVirtualRaw(w.id, out, w.fit.X, w.fit.Y, m.l.stripW, m.l.stripH)
		}
	}
	if out := writePNGEnc(m.minimapPNGPath(), m.mini.frame, "", fastPNG.Encode); out != "" {
		return transmitVirtual(w.id, out, m.l.stripW, m.l.stripH)
	}
	tracef("minimap write failed: %s", w.path)
	return ""
}

// minimapBase is the cached strip thumb scaled to w.fit, or nil when it can't be read.
func (m *galleryModel) minimapBase(w minimapStamp) *image.RGBA {
	f, err := os.Open(cachedPNG(w.path, m.l.stripW, m.l.stripH)) //nolint:gosec // cache file or the user's own image
	if err != nil {
		return nil
	}
	defer f.Close() //nolint:errcheck // read-only file; close error is irrelevant
	src, _, err := image.Decode(f)
	if err != nil {
		return nil
	}
	dst := image.NewRGBA(image.Rect(0, 0, w.fit.X, w.fit.Y))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
	return dst
}

// syncMinimap re-stores the selected thumb when its overlay no longer matches the
// crop. Re-storing in place (a=T, no delete) keeps the thumb visible, like the preview.
func (m *galleryModel) syncMinimap() {
	if m.backend != backendKitty || m.tty == nil || len(m.images) == 0 {
		return
	}
	w := m.minimapWant()
	if w == m.mini.stored {
		return
	}
	apc, got := m.selectedThumbStore(w)
	if _, err := fmt.Fprint(m.tty, apc); err == nil {
		m.mini.stored = got
	}
}
