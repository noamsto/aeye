package main

import (
	"image"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
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
	oldPath := m.images[0].Path

	m = rewriteDiagram(t, dir, m, "roster", hashB, sizeA)
	if m.images[0].Path == oldPath {
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
	if got := m.images[m.cursor].Name; got != "a" {
		t.Fatalf("cursor on %q, want the edited diagram a", got)
	}
	if m.crop != zoomed {
		t.Errorf("crop = %+v, want %+v", m.crop, zoomed)
	}
}
