package main

import (
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
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
	frac := boxAspectFrac(b.Dx(), b.Dy(), m.l.previewW*m.cellWpx(), m.l.previewH*m.cellHpx())
	w, h := 1.0, 1.0
	if frac <= 1 {
		w = frac
	} else {
		h = 1 / frac
	}
	return recenterScaled(m.crop.cx(), m.crop.cy(), w, h)
}

// scaleCropAbout scales the crop's size by s about its center, preserving aspect.
// s < 1 zooms in, s > 1 zooms out. The longer side is floored at 1/zoomMax: it's
// the box-binding axis, so it sets on-screen magnification (true zoomMax×), and
// flooring on it lets a thin letterboxed strip still magnify instead of stalling.
// Both axes scale by the same factor, so a non-square crop never distorts.
func scaleCropAbout(c cropFrac, s float64) cropFrac {
	const minSide = 1.0 / zoomMax
	if hi := max(c.w(), c.h()); hi*s < minSide {
		s = minSide / hi
	}
	return recenterScaled(c.cx(), c.cy(), c.w()*s, c.h()*s)
}

// cropFillsBox reports whether the crop already fills the preview box — its pixel
// aspect matches the box's. False for the letterboxed rest view of a non-square
// image; toggleFill uses this to decide whether to switch to fullCrop or baseFillCrop.
func (m *galleryModel) cropFillsBox() bool {
	if m.curImg == nil {
		return true
	}
	b := m.curImg.Bounds()
	want := boxAspectFrac(b.Dx(), b.Dy(), m.l.previewW*m.cellWpx(), m.l.previewH*m.cellHpx())
	return math.Abs(m.crop.w()/m.crop.h()-want) < want*1e-3
}

// zoomBy moves the crop one zoom step (factor > 1 zooms in). From the letterboxed
// rest view, the first zoom-in adopts box-aspect fill framing (so the preview is
// packed) then magnifies one step; further steps scale that crop about its center.
// A Tab-framed region keeps its aspect — only full→zoom switches framing. Zooming
// out grows the crop until it spills past the image, then snaps back to rest.
func (m *galleryModel) zoomBy(factor float64) {
	if factor > 1 {
		base := m.crop
		if m.crop.isFull() {
			base = m.baseFillCrop()
		}
		m.crop = scaleCropAbout(base, 1/factor)
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

// toggleFill switches between letterboxed full-image framing and a box-aspect
// fill crop centered on the current view. Magnification is not preserved — this
// is a framing rewrite. When the image already matches the preview box,
// baseFillCrop equals fullCrop, so the call is a no-op.
func (m *galleryModel) toggleFill() {
	if m.curImg == nil {
		return
	}
	if m.cropFillsBox() && !m.crop.isFull() {
		m.crop = fullCrop()
		return
	}
	m.crop = m.baseFillCrop()
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
	// Round the origin and the SIZE separately. Truncating both edges independently
	// lets a constant-size crop yield rects that differ by a pixel as it pans, so the
	// downscale target changes every frame and the image visibly shimmers — measured
	// as the raster oscillating between 1179 and 1180 px wide during a drag.
	cw := clamp(int(math.Round(c.w()*w)), 1, b.Dx())
	ch := clamp(int(math.Round(c.h()*h)), 1, b.Dy())
	x0 := clamp(int(math.Round(c.x0*w)), 0, b.Dx()-cw)
	y0 := clamp(int(math.Round(c.y0*h)), 0, b.Dy()-ch)
	return image.Rect(b.Min.X+x0, b.Min.Y+y0, b.Min.X+x0+cw, b.Min.Y+y0+ch)
}

// zoomScratchPath is a per-pane scratch file so concurrently-zoomed panes don't
// overwrite each other's preview render.
func (m *galleryModel) zoomScratchPath() string {
	return filepath.Join(os.TempDir(), "aeye-zoom-"+strings.TrimPrefix(m.pane, "%")+".png")
}

// zoomRawPath is the raw-RGBA sibling of zoomScratchPath. A distinct extension so a
// stale file of one kind is never handed to kitty as the other.
func (m *galleryModel) zoomRawPath() string {
	return filepath.Join(os.TempDir(), "aeye-zoom-"+strings.TrimPrefix(m.pane, "%")+".raw")
}

// renderCropOf crops src to m.crop, downscales to the cols×rows cell box, writes
// the scratch PNG, and returns its path. raw is the fallback path on any miss.
func (m *galleryModel) renderCropOf(src image.Image, cols, rows int, raw string) string {
	dst := m.cropRaster(src, cols, rows)
	if dst == nil {
		return raw
	}
	return writePNGEnc(m.zoomScratchPath(), dst, raw, fastPNG.Encode)
}

// cropRaster crops src to m.crop and downscales it into the cols x rows cell box,
// returning nil when there is nothing to render (no source, or an unzoomed view —
// where the original file is served as-is). Shared by the PNG path the raster
// backend needs and the raw-RGBA path kitty prefers.
func (m *galleryModel) cropRaster(src image.Image, cols, rows int) *image.RGBA {
	if src == nil || m.crop.isFull() {
		return nil
	}
	r := cropPixels(src.Bounds(), m.crop)
	tw, th := cols*m.cellWpx(), rows*m.cellHpx()
	scale := min(float64(tw)/float64(r.Dx()), float64(th)/float64(r.Dy()))
	if scale > 1 {
		scale = 1 // never upscale past source resolution (bitmap layer; Layer 2 lifts this for d2)
	}
	dst := image.NewRGBA(image.Rect(0, 0, int(float64(r.Dx())*scale), int(float64(r.Dy())*scale)))
	// ApproxBiLinear: this runs on every pan/zoom keystroke, so speed matters more
	// than the last bit of quality.
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, r, draw.Src, nil)
	return dst
}

// renderZoom crops m.curImg to m.crop, downscales the crop to the cols×rows cell
// box, writes it to a fixed scratch PNG, and returns that path. Returns the raw
// selected path when nothing is decoded or the crop is full (so the unzoomed
// path is byte-for-byte the pre-zoom behavior).
func (m *galleryModel) renderZoom(cols, rows int) string {
	return m.renderCropOf(m.curImg, cols, rows, m.images[m.cursor].Path)
}

// storePreviewCrop emits the store for the preview at the current crop, skipping
// the PNG encode: kitty reads raw RGBA straight from the file, which measured 2.5x
// faster per frame than a fast PNG encode (6.7ms -> 2.6ms on a 1220x814 box) and is
// the dominant per-pan cost. Falls back to storing the original file when there is
// nothing to crop (unzoomed) or the raw write fails.
func (m *galleryModel) storePreviewCrop() string {
	orig := m.images[m.cursor].Path
	dst := m.cropRaster(m.curImg, m.l.previewW, m.l.previewH)
	if dst == nil {
		return transmitVirtual(m.previewID(), orig, m.l.previewW, m.l.previewH)
	}
	b := dst.Bounds()
	if !preferEncodedFrame(m.bridged) {
		if out := writeRaw(m.zoomRawPath(), dst); out != "" {
			return transmitVirtualRaw(m.previewID(), out, b.Dx(), b.Dy(), m.l.previewW, m.l.previewH)
		}
	}
	return transmitVirtual(m.previewID(),
		writePNGEnc(m.zoomScratchPath(), dst, orig, fastPNG.Encode), m.l.previewW, m.l.previewH)
}

// transmitPreviewOnly re-renders the preview at the current crop and re-places
// it under the same id (a=T) WITHOUT deleting first. Re-placing in place keeps
// the image visible (a data-only a=t update leaves the unicode placeholder
// blank); skipping the delete avoids the blank-frame flicker on zoom/pan.
func (m *galleryModel) transmitPreviewOnly() {
	if m.backend != backendKitty || m.tty == nil || len(m.images) == 0 {
		return
	}
	fmt.Fprint(m.tty, m.storePreviewCrop())
}

// panFrameGap is the minimum spacing between preview re-stores while dragging. A
// drag delivers motion faster than a frame costs, so without this the work queues
// up: the image trails the cursor and keeps moving after it stops. Throttling makes
// each frame render the CURRENT position instead of a stale backlogged one.
const panFrameGap = 8 * time.Millisecond

// bridgedPanFrameGap replaces it when the viewer renders onto a foreign host's
// terminal (lazytmux's remote bridge, AEYE_BRIDGED): there every frame is a file
// the bridge must fetch over ssh before the terminal can read it, so the cost per
// frame is a network round trip rather than a 2.6ms encode.
const bridgedPanFrameGap = 60 * time.Millisecond

// bridged reports whether this viewer's output is being relayed to another host's
// terminal. Set by lazytmux's bridge when it launches the carousel remotely.
func bridged() bool { return os.Getenv("AEYE_BRIDGED") != "" }

func panFrameGapFor(bridged bool) time.Duration {
	if bridged {
		return bridgedPanFrameGap
	}
	return panFrameGap
}

// preferEncodedFrame reports whether to spend a PNG encode to shrink the frame.
// Locally the raw RGBA write wins (2.5x faster per frame, and the terminal reads
// it straight off local disk); across a bridge the same frame is ~10-20x more
// bytes on the wire, which inverts the trade.
func preferEncodedFrame(bridged bool) bool { return bridged }

// panFlushMsg re-stores the final framing of a throttled drag burst. Generation
// gated, so only the newest arms a paint (mirroring vectorKickMsg).
type panFlushMsg struct{ gen uint64 }

// transmitPanFrame re-stores at most once per the pacing gap. When it throttles, it
// returns a trailing flush so the last position of a burst is never dropped.
func (m *galleryModel) transmitPanFrame() tea.Cmd {
	gap := panFrameGapFor(m.bridged)
	if time.Since(m.lastPanAt) >= gap {
		m.transmitPreviewOnly()
		m.lastPanAt = time.Now()
		return nil
	}
	m.panGen++
	g := m.panGen
	return tea.Tick(gap, func(time.Time) tea.Msg { return panFlushMsg{gen: g} })
}
