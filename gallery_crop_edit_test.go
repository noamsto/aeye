package main

import (
	"image"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// rewriteDiagram simulates the hook re-rendering name's .d2: a new content hash
// at size replaces the entry in place.
func rewriteDiagram(t *testing.T, dir string, m galleryModel, name, hash string, size image.Point) galleryModel {
	t.Helper()
	writeCropManifest(t, m.pane, diagramPairNamed(t, dir, name, hash, size))
	return reloaded(m)
}

func validCrop(c cropFrac) bool {
	for _, v := range []float64{c.x0, c.y0, c.x1, c.y1} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1 {
			return false
		}
	}
	return c.w() > 0 && c.h() > 0 && max(c.w(), c.h()) >= 1/zoomMax-1e-9
}

func TestSelectedDiagramCropSurvivesRewrite(t *testing.T) {
	dir := cropDir(t)
	m := newManifestCropModel(t, "dark", diagramPairNamed(t, dir, "roster", hashA, sizeA))
	m = sendAll(t, m, keyZoom, keyZoom, keyJ)
	zoomed := m.crop
	if zoomed.isFull() {
		t.Fatal("setup: diagram is not zoomed")
	}
	if len(m.images) == 0 {
		t.Fatal("setup: no images")
	}
	oldPath := m.images[0].Path

	m = rewriteDiagram(t, dir, m, "roster", hashB, sizeA)
	if len(m.images) == 0 || m.images[0].Path == oldPath {
		t.Fatal("setup: rewrite kept the render path")
	}
	if m.crop != zoomed {
		t.Errorf("crop after rewrite = %+v, want %+v", m.crop, zoomed)
	}
	m = land(t, m)
	if m.crop != zoomed {
		t.Errorf("crop after the new render landed = %+v, want %+v", m.crop, zoomed)
	}
}

func TestSelectedDiagramCropValidWhenRewriteChangesSize(t *testing.T) {
	for _, size := range []image.Point{sizeB, sizeC, {X: 40, Y: 4000}, {X: 4000, Y: 40}} {
		dir := cropDir(t)
		m := newManifestCropModel(t, "dark", diagramPairNamed(t, dir, "roster", hashA, sizeA))
		m = sendAll(t, m, keyZoom, keyZoom, keyJ)
		m = land(t, rewriteDiagram(t, dir, m, "roster", hashB, size))
		if m.crop.isFull() || !validCrop(m.crop) {
			t.Errorf("size %v: crop %+v, want a kept, valid zoom", size, m.crop)
		}
	}
}

// Regression (#355): a zoomed diagram re-rendered at a different aspect kept its
// crop fractions, but those were shaped for the old pixels — against the new
// image they no longer fill the box, leaving empty bars on the binding axis
// (left/right in a tall narrow box, top/bottom in a wide one) until the next zoom
// step re-packed the crop.
func TestRewriteThatChangesAspectStillFillsBox(t *testing.T) {
	for _, tc := range []struct {
		name     string
		win      image.Point
		from, to image.Point
	}{
		{"tall narrow box, landscape turns square", image.Pt(50, 70), image.Pt(1020, 394), image.Pt(300, 300)},
		{"tall narrow box, portrait turns square", image.Pt(40, 60), image.Pt(400, 300), image.Pt(300, 300)},
		{"wide box, square turns landscape", image.Pt(100, 20), image.Pt(400, 300), image.Pt(1020, 394)},
		{"wide box, portrait turns landscape", image.Pt(120, 24), image.Pt(300, 900), image.Pt(1600, 500)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := cropDir(t)
			m := newManifestCropModel(t, "dark", diagramPairNamed(t, dir, "roster", hashA, tc.from))
			m, _ = send(t, m, tea.WindowSizeMsg{Width: tc.win.X, Height: tc.win.Y})
			m = sendAll(t, land(t, m), keyZoom, keyZoom, keyZoom, keyZoom, keyZoom, keyZoom)
			before := m.crop
			m = land(t, rewriteDiagram(t, dir, m, "roster", hashB, tc.to))
			if m.crop.isFull() {
				t.Fatalf("rewrite dropped the zoom: crop %+v", m.crop)
			}
			boxW, boxH := m.l.previewW*m.cellWpx(), m.l.previewH*m.cellHpx()
			sx, sy := displayedSpan(m.crop, tc.to, boxW, boxH)
			if boxAspectFrac(tc.to.X, tc.to.Y, boxW, boxH) <= 1 {
				sy = 1 // width binds; the shortfall on the other axis is the rest letterbox
			} else {
				sx = 1
			}
			if math.Abs(sx-1) > 1e-2 || math.Abs(sy-1) > 1e-2 {
				t.Errorf("rewritten diagram covers %.3f x %.3f of the box, crop %+v", sx, sy, m.crop)
			}
			if math.Abs(m.crop.cx()-before.cx()) > 1e-9 || math.Abs(m.crop.cy()-before.cy()) > 1e-9 {
				t.Errorf("re-pack moved the view center: %+v -> %+v", before, m.crop)
			}
		})
	}
}

func TestSanitizeCrop(t *testing.T) {
	nan := math.NaN()
	for _, tc := range []struct {
		name string
		in   cropFrac
		full bool
	}{
		{"nan", cropFrac{nan, 0, 1, 1}, true},
		{"inf", cropFrac{0, 0, math.Inf(1), 1}, true},
		{"zero width", cropFrac{0.5, 0, 0.5, 1}, true},
		{"inverted", cropFrac{0.8, 0.8, 0.2, 0.2}, true},
		{"out of bounds", cropFrac{-0.5, 0.9, 0.4, 1.7}, false},
		{"tinier than the floor", cropFrac{0.5, 0.5, 0.51, 0.51}, false},
	} {
		got := sanitizeCrop(tc.in)
		if !validCrop(got) {
			t.Errorf("%s: sanitizeCrop(%+v) = %+v, invalid", tc.name, tc.in, got)
		}
		if got.isFull() != tc.full {
			t.Errorf("%s: sanitizeCrop(%+v) = %+v, full = %v want %v", tc.name, tc.in, got, got.isFull(), tc.full)
		}
	}
}

func TestEditedDiagramCropKeptOnSwitchBack(t *testing.T) {
	dir := cropDir(t)
	m := newManifestCropModel(t, "dark",
		diagramPairNamed(t, dir, "a", hashA, sizeA), diagramPairNamed(t, dir, "b", hashB, sizeB))
	m = sendAll(t, m, keyZoom, keyZoom, keyJ)
	zoomed := m.crop
	m = sendAll(t, m, key2)

	const hashC = "00112233445566ff"
	writeCropManifest(t, m.pane, diagramPairNamed(t, dir, "a", hashC, sizeC), diagramPairNamed(t, dir, "b", hashB, sizeB))
	m = reloaded(m)
	m = sendAll(t, m, key1)
	if m.crop.isFull() || !validCrop(m.crop) {
		t.Errorf("edited diagram reopened at %+v, want a valid zoom like %+v", m.crop, zoomed)
	}
}

func TestEditedDiagramResetStillGoesToFit(t *testing.T) {
	dir := cropDir(t)
	m := newManifestCropModel(t, "dark", diagramPairNamed(t, dir, "roster", hashA, sizeA))
	m = sendAll(t, m, keyZoom, keyZoom)
	m = rewriteDiagram(t, dir, m, "roster", hashB, sizeA)
	m = sendAll(t, land(t, m), keyUnzoom)
	if !m.crop.isFull() {
		t.Fatalf("0 left crop %+v, want fit", m.crop)
	}
	m = land(t, rewriteDiagram(t, dir, m, "roster", hashA, sizeA))
	if !m.crop.isFull() {
		t.Errorf("rewrite after reset opened at %+v, want fit", m.crop)
	}
}

func TestRemovedDiagramCropPruned(t *testing.T) {
	dir := cropDir(t)
	m := newManifestCropModel(t, "dark",
		diagramPairNamed(t, dir, "a", hashA, sizeA), diagramPairNamed(t, dir, "b", hashB, sizeB))
	m = sendAll(t, m, keyZoom, keyZoom, keyJ, key2)
	if len(m.crops) != 1 {
		t.Fatalf("setup: crops = %v, want a's", m.crops)
	}
	writeCropManifest(t, m.pane, diagramPairNamed(t, dir, "b", hashB, sizeB))
	m = reloaded(m)
	if len(m.crops) != 0 {
		t.Errorf("crops = %v, want the removed diagram's pruned", m.crops)
	}
}

func TestRasterCropStillDroppedWhenStampChanges(t *testing.T) {
	dir := cropDir(t)
	p := filepath.Join(dir, "shot.png")
	writeTestImage(t, p, sizeA.X, sizeA.Y)
	m := newDecodeModel(t, sizeA)
	writeCropManifest(t, m.pane, `{"type":"image","path":"`+p+`","mtime":1}`)
	m = land(t, reloaded(m))
	m = sendAll(t, m, keyZoom, keyZoom, keyJ)
	m.rememberCrop()
	writeTestImage(t, p, sizeC.X, sizeC.Y)
	if err := os.Chtimes(p, testFuture(), testFuture()); err != nil {
		t.Fatal(err)
	}
	m.curImgPath = ""
	m.ensureDecoded()
	if !m.crop.isFull() {
		t.Errorf("changed raster opened at %+v, want fit", m.crop)
	}
}

func testFuture() time.Time { return time.Now().Add(time.Hour) }

func TestReloadFollowsEditedDiagramToNewSlot(t *testing.T) {
	dir := cropDir(t)
	m := newManifestCropModel(t, "dark",
		diagramPairNamed(t, dir, "a", hashA, sizeA), diagramPairNamed(t, dir, "b", hashB, sizeB))
	m = sendAll(t, m, keyZoom, keyZoom, keyJ)
	zoomed := m.crop

	// The hook drops the old entry and appends the new one at the end.
	const hashC = "00112233445566ff"
	writeCropManifest(t, m.pane, diagramPairNamed(t, dir, "b", hashB, sizeB), diagramPairNamed(t, dir, "a", hashC, sizeA))
	m = land(t, reloaded(m))
	if len(m.images) == 0 {
		t.Fatal("no images after reload")
	}
	if got := m.images[m.cursor].Name; got != "a" {
		t.Fatalf("cursor on %q, want the edited diagram a", got)
	}
	if m.crop != zoomed {
		t.Errorf("crop = %+v, want %+v", m.crop, zoomed)
	}
}
