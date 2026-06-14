package main

import (
	"fmt"
	"image"
	"os"
	"path/filepath"

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

// zoomBy scales the crop about its center by 1/factor (factor > 1 zooms in),
// preserving the crop's current aspect. The crop side is clamped to
// [1/zoomMax, 1]; reaching full snaps to fullCrop.
func (m *galleryModel) zoomBy(factor float64) {
	const minSide = 1.0 / zoomMax
	w := clampF(m.crop.w()/factor, minSide, 1)
	h := clampF(m.crop.h()/factor, minSide, 1)
	c := recenterScaled(m.crop.cx(), m.crop.cy(), w, h)
	if c.isFull() {
		c = fullCrop()
	}
	m.crop = c
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
		return
	}
	p := m.images[m.cursor].Path
	if p == m.curImgPath {
		return
	}
	m.curImgPath = p
	m.resetZoom()
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

var zoomScratch = filepath.Join(os.TempDir(), "agent-carousel-zoom.png")

// renderZoom crops m.curImg to m.crop, downscales the crop to the cols×rows cell
// box, writes it to a fixed scratch PNG, and returns that path. Returns the raw
// selected path when nothing is decoded or the crop is full (so the unzoomed
// path is byte-for-byte the pre-zoom behavior).
func (m *galleryModel) renderZoom(cols, rows int) string {
	raw := m.images[m.cursor].Path
	if m.curImg == nil || m.crop.isFull() {
		return raw
	}
	r := cropPixels(m.curImg.Bounds(), m.crop)
	tw, th := cols*cellPxW, rows*cellPxH
	scale := min(float64(tw)/float64(r.Dx()), float64(th)/float64(r.Dy()))
	if scale > 1 {
		scale = 1 // never upscale past source resolution (bitmap layer; Layer 2 lifts this for d2)
	}
	dst := image.NewRGBA(image.Rect(0, 0, int(float64(r.Dx())*scale), int(float64(r.Dy())*scale)))
	draw.CatmullRom.Scale(dst, dst.Bounds(), m.curImg, r, draw.Src, nil)
	return writePNG(zoomScratch, dst, raw)
}

// deletePreview clears just the preview image (id=previewID) + its placements,
// leaving the filmstrip untouched — so zoom/pan keystrokes don't re-transmit the
// whole grid (which would flicker).
func deletePreview() string {
	return tmuxPassthrough(fmt.Sprintf("\x1b_Ga=d,d=i,i=%d,q=2\x1b\\", previewID))
}

// transmitPreviewOnly re-renders + re-transmits only the preview image at the
// current crop. The placeholder cells in View already reference previewID.
func (m *galleryModel) transmitPreviewOnly() {
	if m.backend != backendKitty || m.tty == nil || len(m.images) == 0 {
		return
	}
	fmt.Fprint(m.tty, deletePreview())
	fmt.Fprint(m.tty, transmitVirtual(previewID, m.renderZoom(m.l.previewW, m.l.previewH), m.l.previewW, m.l.previewH))
}
