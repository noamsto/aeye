package main

import (
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/draw"
)

// cropFrac is the visible sub-rectangle of the source image, in source
// fractions (0..1). Full image = {0,0,1,1}. Invariant kept by the methods that
// mutate it: 0 <= x0 < x1 <= 1 and 0 <= y0 < y1 <= 1.
type cropFrac struct{ x0, y0, x1, y1 float64 }

func fullCrop() cropFrac { return cropFrac{0, 0, 1, 1} }

func (c cropFrac) w() float64  { return c.x1 - c.x0 }
func (c cropFrac) h() float64  { return c.y1 - c.y0 }
func (c cropFrac) cx() float64 { return (c.x0 + c.x1) / 2 }
func (c cropFrac) cy() float64 { return (c.y0 + c.y1) / 2 }

// isFull reports whether the crop covers (essentially) the whole image, i.e.
// nothing is zoomed. The epsilon absorbs float drift from repeated zoom-out.
func (c cropFrac) isFull() bool { return c.w() >= 0.999 && c.h() >= 0.999 }

// clampF clamps a float64 to [lo, hi].
func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

const zoomMax = 8.0

func (m *galleryModel) resetZoom() { m.crop = fullCrop() }

// recenterScaled returns a crop of the given width/height centered at (cx,cy),
// shifted to stay inside [0,1] (size preserved). w/h are pre-clamped to (0,1].
func recenterScaled(cx, cy, w, h float64) cropFrac {
	x0 := clampF(cx-w/2, 0, 1-w)
	y0 := clampF(cy-h/2, 0, 1-h)
	return cropFrac{x0, y0, x0 + w, y0 + h}
}

// baseFillCrop is the largest crop — centered on the current view — whose pixel
// aspect matches the preview box, so it fills the box with no letterbox. For a
// wide image that's a full-height vertical slice; for a tall image, a full-width
// horizontal band. Returns fullCrop when nothing is decoded.
func (m *galleryModel) baseFillCrop() cropFrac {
	if m.curImg == nil {
		return fullCrop()
	}
	b := m.curImg.Bounds()
	frac := boxAspectFrac(b.Dx(), b.Dy(), m.l.previewW*cellPxW, m.l.previewH*cellPxH)
	w, h := 1.0, 1.0
	if frac <= 1 {
		w = frac
	} else {
		h = 1 / frac
	}
	return recenterScaled(m.crop.cx(), m.crop.cy(), w, h)
}

// scaleCropAbout scales the crop's size by s about its center, preserving aspect.
// s < 1 zooms in, s > 1 zooms out. The smaller side is floored at 1/zoomMax, and
// flooring scales both axes together so a non-square crop never distorts.
func scaleCropAbout(c cropFrac, s float64) cropFrac {
	const minSide = 1.0 / zoomMax
	if lo := min(c.w(), c.h()); lo*s < minSide {
		s = minSide / lo
	}
	return recenterScaled(c.cx(), c.cy(), c.w()*s, c.h()*s)
}

// cropFillsBox reports whether the crop already fills the preview box — its pixel
// aspect matches the box's. False for the rest view of a non-square image and for
// a region framed wider or taller than the box (Tab framing keeps the whole
// region, so a wide step group letterboxes).
func (m *galleryModel) cropFillsBox() bool {
	if m.curImg == nil {
		return true
	}
	b := m.curImg.Bounds()
	want := boxAspectFrac(b.Dx(), b.Dy(), m.l.previewW*cellPxW, m.l.previewH*cellPxH)
	return math.Abs(m.crop.w()/m.crop.h()-want) < want*1e-3
}

// zoomBy moves the crop one zoom step (factor > 1 zooms in). Zooming in while the
// crop is letterboxed — the rest view, or a region framed wider/taller than the
// box — snaps to the box-aspect fill crop centered on the current view, so a wide
// diagram (or a wide Tab-framed step group) grows to fill the box instead of
// staying a strip. Once it fills, deeper steps scale it uniformly. Zooming out
// grows the crop until it spills past the image, then snaps back to the rest view.
func (m *galleryModel) zoomBy(factor float64) {
	if factor > 1 {
		if !m.cropFillsBox() {
			m.crop = m.baseFillCrop()
			return
		}
		m.crop = scaleCropAbout(m.crop, 1/factor)
		return
	}
	if m.crop.isFull() {
		return
	}
	w, h := m.crop.w()/factor, m.crop.h()/factor
	if w >= 1 || h >= 1 {
		m.crop = fullCrop()
		return
	}
	m.crop = recenterScaled(m.crop.cx(), m.crop.cy(), w, h)
}

// panBy shifts the crop by a fraction of its own size, so a keypress feels like
// a constant on-screen distance regardless of zoom. The shift is clamped so the
// crop stays inside [0,1] without resizing.
func (m *galleryModel) panBy(dx, dy float64) {
	w, h := m.crop.w(), m.crop.h()
	x0 := clampF(m.crop.x0+dx*w, 0, 1-w)
	y0 := clampF(m.crop.y0+dy*h, 0, 1-h)
	m.crop = cropFrac{x0, y0, x0 + w, y0 + h}
}

// ensureDecoded decodes the currently-selected image into m.curImg, but only
// when the selected path changed since the last decode. A changed selection
// resets the crop to fit; an unchanged selection (e.g. an auto-refresh tick that
// appended a different image elsewhere) preserves the crop and the decode.
func (m *galleryModel) ensureDecoded() {
	if len(m.images) == 0 {
		m.curImg, m.curImgPath = nil, ""
		m.regions, m.regionPath, m.regionIdx = nil, nil, -1
		return
	}
	p := m.images[m.cursor].Path
	if p == m.curImgPath {
		return
	}
	m.resetZoom()
	m.regions, m.regionPath, m.regionIdx = nil, nil, -1
	f, err := os.Open(p)
	if err != nil {
		m.curImg = nil
		return
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		m.curImg = nil
		return
	}
	m.curImg = img
	m.curImgPath = p
}

// cropPixels maps a normalized crop to a pixel rectangle inside b, offset by
// b.Min so callers can sample the source directly.
func cropPixels(b image.Rectangle, c cropFrac) image.Rectangle {
	w, h := float64(b.Dx()), float64(b.Dy())
	return image.Rect(
		b.Min.X+int(c.x0*w), b.Min.Y+int(c.y0*h),
		b.Min.X+int(c.x1*w), b.Min.Y+int(c.y1*h),
	)
}

// zoomScratchPath is a per-pane scratch file so concurrently-zoomed panes don't
// overwrite each other's preview render.
func (m *galleryModel) zoomScratchPath() string {
	return filepath.Join(os.TempDir(), "aeye-zoom-"+strings.TrimPrefix(m.pane, "%")+".png")
}

// renderCropOf crops src to m.crop, downscales to the cols×rows cell box, writes
// the scratch PNG, and returns its path. raw is the fallback path on any miss.
func (m *galleryModel) renderCropOf(src image.Image, cols, rows int, raw string) string {
	if src == nil || m.crop.isFull() {
		return raw
	}
	r := cropPixels(src.Bounds(), m.crop)
	tw, th := cols*cellPxW, rows*cellPxH
	scale := min(float64(tw)/float64(r.Dx()), float64(th)/float64(r.Dy()))
	if scale > 1 {
		scale = 1 // never upscale past source resolution (bitmap layer; Layer 2 lifts this for d2)
	}
	dst := image.NewRGBA(image.Rect(0, 0, int(float64(r.Dx())*scale), int(float64(r.Dy())*scale)))
	// ApproxBiLinear + fast PNG: this runs on every pan/zoom keystroke, so encode
	// speed matters more than the last bit of quality or file size.
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, r, draw.Src, nil)
	return writePNGEnc(m.zoomScratchPath(), dst, raw, fastPNG.Encode)
}

// renderZoom crops m.curImg to m.crop, downscales the crop to the cols×rows cell
// box, writes it to a fixed scratch PNG, and returns that path. Returns the raw
// selected path when nothing is decoded or the crop is full (so the unzoomed
// path is byte-for-byte the pre-zoom behavior).
func (m *galleryModel) renderZoom(cols, rows int) string {
	return m.renderCropOf(m.curImg, cols, rows, m.images[m.cursor].Path)
}

// transmitPreviewOnly re-renders the preview at the current crop and re-places
// it under the same id (a=T) WITHOUT deleting first. Re-placing in place keeps
// the image visible (a data-only a=t update leaves the unicode placeholder
// blank); skipping the delete avoids the blank-frame flicker on zoom/pan.
func (m *galleryModel) transmitPreviewOnly() {
	if m.backend != backendKitty || m.tty == nil || len(m.images) == 0 {
		return
	}
	fmt.Fprint(m.tty, transmitVirtual(previewID, m.renderZoom(m.l.previewW, m.l.previewH), m.l.previewW, m.l.previewH))
}
