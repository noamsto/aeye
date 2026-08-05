package main

import (
	"image"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func approx(a, b float64) bool { return a-b < 1e-9 && b-a < 1e-9 }

func TestFullCrop(t *testing.T) {
	c := fullCrop()
	if c.x0 != 0 || c.y0 != 0 || c.x1 != 1 || c.y1 != 1 {
		t.Errorf("fullCrop = %+v", c)
	}
	if !c.isFull() {
		t.Error("fullCrop must report isFull")
	}
}

func TestCropFracDims(t *testing.T) {
	c := cropFrac{0.2, 0.1, 0.8, 0.6}
	if !approx(c.w(), 0.6) || !approx(c.h(), 0.5) {
		t.Errorf("w/h = %v,%v", c.w(), c.h())
	}
	if !approx(c.cx(), 0.5) || !approx(c.cy(), 0.35) {
		t.Errorf("cx/cy = %v,%v", c.cx(), c.cy())
	}
	if c.isFull() {
		t.Error("sub-rect must not report isFull")
	}
}

func TestClampF(t *testing.T) {
	if clampF(5, 1, 8) != 5 || clampF(0, 1, 8) != 1 || clampF(9, 1, 8) != 8 {
		t.Error("clampF bounds wrong")
	}
}

func TestCropPixels(t *testing.T) {
	b := image.Rect(0, 0, 100, 80)
	r := cropPixels(b, cropFrac{0.25, 0.25, 0.75, 0.75})
	if r != image.Rect(25, 20, 75, 60) {
		t.Errorf("cropPixels = %v, want (25,20)-(75,60)", r)
	}
	if r := cropPixels(b, fullCrop()); r != b {
		t.Errorf("full cropPixels = %v, want %v", r, b)
	}
}

func TestCropPixelsOffsetBounds(t *testing.T) {
	b := image.Rect(10, 20, 110, 100) // 100x80 at origin (10,20)
	r := cropPixels(b, cropFrac{0, 0, 0.5, 0.5})
	if r != image.Rect(10, 20, 60, 60) {
		t.Errorf("offset cropPixels = %v, want (10,20)-(60,60)", r)
	}
}

func TestResetZoom(t *testing.T) {
	m := &galleryModel{crop: cropFrac{0.1, 0.1, 0.3, 0.3}}
	m.resetZoom()
	if !m.crop.isFull() {
		t.Errorf("resetZoom = %+v", m.crop)
	}
}

func TestZoomByShrinksCentered(t *testing.T) {
	m := &galleryModel{crop: fullCrop()}
	m.zoomBy(2) // zoom in 2x → crop side halves, centered
	if !approx(m.crop.w(), 0.5) || !approx(m.crop.cx(), 0.5) {
		t.Errorf("zoom-in crop = %+v", m.crop)
	}
}

func TestZoomByClampsAtMax(t *testing.T) {
	m := &galleryModel{crop: fullCrop()}
	for i := 0; i < 50; i++ {
		m.zoomBy(1.25)
	}
	if !approx(m.crop.w(), 1.0/zoomMax) || !approx(m.crop.h(), 1.0/zoomMax) {
		t.Errorf("zoom must clamp to max side 1/%v, got w=%v h=%v", zoomMax, m.crop.w(), m.crop.h())
	}
}

func TestZoomOutFloorsToFull(t *testing.T) {
	m := &galleryModel{crop: cropFrac{0.4, 0.4, 0.6, 0.6}}
	for i := 0; i < 50; i++ {
		m.zoomBy(1 / 1.25)
	}
	if !m.crop.isFull() {
		t.Errorf("zoom-out must floor at full, got %+v", m.crop)
	}
}

// wideModel builds a model whose decoded image is 7.2:1 and whose preview box is
// 1.6:1, so the box-aspect fill crop is a full-height, 2/9-width vertical slice.
func wideModel() *galleryModel {
	return &galleryModel{
		curImg: image.NewRGBA(image.Rect(0, 0, 720, 100)),
		l:      layout{previewW: 160, previewH: 50},
		crop:   fullCrop(),
	}
}

func TestZoomByFirstStepFillsBox(t *testing.T) {
	m := wideModel()
	m.zoomBy(1.25) // from letterboxed rest: adopt fill framing, then one magnify step
	fill := m.baseFillCrop()
	if !approx(m.crop.w(), fill.w()/1.25) || !approx(m.crop.h(), fill.h()/1.25) {
		t.Errorf("first zoom-in crop = %+v, want fill/1.25 (fill=%+v)", m.crop, fill)
	}
	if !approx(m.crop.w()/m.crop.h(), fill.w()/fill.h()) {
		t.Errorf("first zoom must be box-aspect, got %+v", m.crop)
	}
	if !approx(m.crop.cx(), 0.5) || !approx(m.crop.cy(), 0.5) {
		t.Errorf("first zoom must stay centered, got %+v", m.crop)
	}
}

func TestZoomDeeperPreservesAspect(t *testing.T) {
	m := wideModel()
	m.crop = m.baseFillCrop()
	want := m.crop.w() / m.crop.h()
	m.zoomBy(1.25)
	m.zoomBy(1.25)
	if got := m.crop.w() / m.crop.h(); !approx(got, want) {
		t.Errorf("deeper zoom changed aspect: %v -> %v", want, got)
	}
}

func TestZoomInFromFramedRegionMagnifiesView(t *testing.T) {
	m := wideModel()
	// A wide framed region (Tab onto a step group): aspect far from the box, so it
	// letterboxes. Zooming in must magnify the framed view in place — scale about
	// its center, aspect preserved — not jump to a full-image-span slice.
	m.crop = cropFrac{0.1, 0.45, 0.9, 0.55}
	wantAspect := m.crop.w() / m.crop.h()
	cx, cy := m.crop.cx(), m.crop.cy()
	m.zoomBy(1.25)
	if got := m.crop.w() / m.crop.h(); !approx(got, wantAspect) {
		t.Errorf("magnify must preserve the framed aspect: %v -> %v", wantAspect, got)
	}
	if !approx(m.crop.w(), 0.8/1.25) || !approx(m.crop.h(), 0.1/1.25) {
		t.Errorf("magnify must scale the crop by 1/1.25 about center, got %+v", m.crop)
	}
	if !approx(m.crop.cx(), cx) || !approx(m.crop.cy(), cy) {
		t.Errorf("magnify must keep the view centered, got %+v", m.crop)
	}
}

func TestZoomInClampsLongSideAtMax(t *testing.T) {
	// A near-floor framed strip: the longer side sits just above 1/zoomMax, so one
	// more zoom-in must clamp on that box-binding side with aspect preserved. The
	// old shorter-side floor would have inverted the scale here and grown the crop.
	m := &galleryModel{crop: cropFrac{0.43, 0.49, 0.57, 0.51}} // w=0.14, h=0.02
	wantAspect := m.crop.w() / m.crop.h()
	m.zoomBy(1.25)
	if got := max(m.crop.w(), m.crop.h()); !approx(got, 1.0/zoomMax) {
		t.Errorf("longer side must clamp to 1/%v, got %v", zoomMax, got)
	}
	if got := m.crop.w() / m.crop.h(); !approx(got, wantAspect) {
		t.Errorf("clamp must preserve aspect: %v -> %v", wantAspect, got)
	}
}

func TestZoomOutFromFillReachesFull(t *testing.T) {
	m := wideModel()
	m.crop = m.baseFillCrop() // full-height slice; h == 1
	m.zoomBy(1 / 1.25)
	if !m.crop.isFull() {
		t.Errorf("zoom-out from a full-height fill must reach full, got %+v", m.crop)
	}
}

func TestPanByClamps(t *testing.T) {
	m := &galleryModel{crop: cropFrac{0.25, 0.25, 0.75, 0.75}}
	m.panBy(-1, -1) // big step up/left
	if !approx(m.crop.x0, 0) || !approx(m.crop.y0, 0) {
		t.Errorf("pan must clamp to the top-left, got %+v", m.crop)
	}
	if !approx(m.crop.w(), 0.5) || !approx(m.crop.h(), 0.5) {
		t.Errorf("pan must preserve crop size, got %+v", m.crop)
	}
}

func TestEnsureDecodedPreservesOnSamePath(t *testing.T) {
	m := &galleryModel{
		images:     []imageEntry{{Path: "/nope/a.png"}},
		curImgPath: "/nope/a.png",
		crop:       cropFrac{0.2, 0.2, 0.5, 0.5},
	}
	m.ensureDecoded() // same path → must not reset the crop
	if m.crop.isFull() {
		t.Errorf("same-path decode reset the crop: %+v", m.crop)
	}
}

func TestEnsureDecodedCropResetOnNewPath(t *testing.T) {
	m := &galleryModel{
		images:     []imageEntry{{Path: "/nope/new.png"}},
		curImgPath: "/nope/old.png",
		crop:       cropFrac{0.2, 0.2, 0.5, 0.5},
	}
	m.ensureDecoded() // path changed → crop must reset even though decode fails
	if !m.crop.isFull() {
		t.Errorf("new path must reset crop, got %+v", m.crop)
	}
}

func TestToggleFillFromFull(t *testing.T) {
	m := wideModel()
	m.toggleFill()
	if !approx(m.crop.w(), 2.0/9.0) || !approx(m.crop.h(), 1) {
		t.Errorf("fill from full = %+v, want w=2/9 h=1", m.crop)
	}
	if !approx(m.crop.cx(), 0.5) {
		t.Errorf("fill crop must be centered, got %+v", m.crop)
	}
}

func TestToggleFillFromFill(t *testing.T) {
	m := wideModel()
	m.crop = m.baseFillCrop()
	m.toggleFill()
	if !m.crop.isFull() {
		t.Errorf("fill -> full = %+v", m.crop)
	}
}

func TestToggleFillFromZoomed(t *testing.T) {
	m := wideModel()
	m.zoomBy(1.25) // zoomed views are already box-aspect fill
	m.toggleFill()
	if !m.crop.isFull() {
		t.Errorf("toggle from zoomed-fill must go to full, got %+v", m.crop)
	}
}

func TestToggleFillFromRegionClearsFocus(t *testing.T) {
	rs := []region{{path: "a", x0: 0.1, y0: 0.45, x1: 0.9, y1: 0.55}}
	m := wideModel()
	m.ready = true
	m.regions = newRegionTree(rs)
	m.regionIdx = 0
	m.frameFocused()
	if m.regionIdx < 0 {
		t.Fatal("setup: need focused region")
	}
	if m.crop.isFull() {
		t.Fatal("setup: need non-full crop from framed region")
	}
	want := wideModel()
	want.ready = true
	want.regions = newRegionTree(rs)
	want.regionIdx = 0
	want.frameFocused()
	if want.regionIdx >= 0 {
		want.exitRegions()
	}
	want.toggleFill()

	out, _ := m.Update(tea.KeyPressMsg{Text: "f", Code: 'f'})
	got := out.(galleryModel)
	if got.regionIdx != -1 || got.regionPath != nil {
		t.Errorf("region focus not cleared: idx=%d path=%v", got.regionIdx, got.regionPath)
	}
	if !approx(got.crop.w(), want.crop.w()) || !approx(got.crop.h(), want.crop.h()) {
		t.Errorf("f key from region = %+v, want %+v", got.crop, want.crop)
	}
}

func TestToggleFillNoopWhenMatched(t *testing.T) {
	// preview box pixels = 160*10 × 50*20 = 1600×1000 (1.6:1); image 160×100 matches.
	m := &galleryModel{
		curImg: image.NewRGBA(image.Rect(0, 0, 160, 100)),
		l:      layout{previewW: 160, previewH: 50},
		crop:   fullCrop(),
	}
	m.toggleFill()
	if !m.crop.isFull() {
		t.Errorf("matched aspect must stay full, got %+v", m.crop)
	}
}

func TestToggleFillNilImage(t *testing.T) {
	m := &galleryModel{crop: fullCrop()}
	m.toggleFill() // must not panic
	if !m.crop.isFull() {
		t.Errorf("nil image toggle changed crop: %+v", m.crop)
	}
}

func TestCropGeometryUsesMeasuredCellSize(t *testing.T) {
	// Regression: the crop math used to be shaped against the hardcoded 1:2
	// cellPxW/cellPxH estimate while the real cell here is 10x22. Anything that maps
	// a crop to the preview box must read the measured size, or the fill is skewed.
	img := image.NewRGBA(image.Rect(0, 0, 1600, 900))
	fill := func(cw, ch int) cropFrac {
		m := &galleryModel{l: layout{previewW: 100, previewH: 30}, cellW: cw, cellH: ch, curImg: img}
		return m.baseFillCrop()
	}
	if a, b := fill(10, 20), fill(10, 22); a == b {
		t.Errorf("baseFillCrop ignored the cell size: %+v at 10x20 == %+v at 10x22", a, b)
	}

	m := &galleryModel{l: layout{previewW: 100, previewH: 30}, cellW: 10, cellH: 22, curImg: img}
	m.crop = m.baseFillCrop()
	if !m.cropFillsBox() {
		t.Errorf("crop %+v from baseFillCrop does not fill the box at a 10x22 cell", m.crop)
	}
}

// Bridged viewers pay a network round trip per stored frame instead of a local
// encode, so both the pacing and the frame format flip when AEYE_BRIDGED is set.
func TestBridgedFramePolicy(t *testing.T) {
	if panFrameGapFor(false) != 8*time.Millisecond {
		t.Fatal("local gap changed")
	}
	if panFrameGapFor(true) <= 8*time.Millisecond {
		t.Fatal("bridged gap must be wider than the local one")
	}
	if !preferEncodedFrame(true) {
		t.Fatal("bridged must prefer the encoded (PNG) frame")
	}
	if preferEncodedFrame(false) {
		t.Fatal("local must keep the raw RGBA fast path")
	}
}
