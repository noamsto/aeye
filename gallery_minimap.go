package main

import (
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"strings"
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
