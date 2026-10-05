package main

import (
	"fmt"
	"image"
	"math"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// onScreenMag reads the magnification straight off what is on screen: the scale
// the crop renders at in the current box, relative to the scale the whole image
// renders at (the rest view), derived in pixel space rather than from crop
// fractions.
func onScreenMag(m *galleryModel) float64 {
	boxW := float64(m.l.previewW * m.cellWpx())
	boxH := float64(m.l.previewH * m.cellHpx())
	srcW, srcH := float64(m.curSize.X), float64(m.curSize.Y)
	rest := min(boxW/srcW, boxH/srcH)
	shown := min(boxW/(m.crop.w()*srcW), boxH/(m.crop.h()*srcH))
	return shown / rest
}

func zoomedModel(t *testing.T, img, pane image.Point, presses int) galleryModel {
	t.Helper()
	m := mouseModel(pane.X, pane.Y, 0, 1)
	m.ready = true
	m.cellW, m.cellH = 10, 20
	m.curSize = img
	m.images = []imageEntry{{}}
	m.curImgPath = m.images[0].Path
	m.crop = fullCrop()
	for range presses {
		pressZoom(&m, 'z')
	}
	return m
}

func resizeTo(m *galleryModel, w, h int) {
	out, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	*m = out.(galleryModel)
}

// toggleSplit presses `s` and delivers the resize tmux's move-pane would cause.
// With TMUX unset the key itself is a no-op: the viewer only ever sees the split
// flip as a resize, so the WindowSizeMsg is the whole signal.
func toggleSplit(t *testing.T, m *galleryModel, w, h int) {
	t.Helper()
	t.Setenv("TMUX", "")
	pressZoom(m, 's')
	resizeTo(m, w, h)
}

func filled(m *galleryModel) bool {
	sx, sy := displayedSpan(m.crop, m.curSize, m.l.previewW*m.cellWpx(), m.l.previewH*m.cellHpx())
	return math.Abs(sx-1) < 1e-3 && math.Abs(sy-1) < 1e-3
}

// A packed crop keeps the old box's aspect through a resize. The next zoom-in
// must re-pack it for the new box instead of scaling the stale aspect.
func TestResizeMidZoomPackedCropRepacks(t *testing.T) {
	m := zoomedModel(t, image.Pt(1800, 600), image.Pt(100, 50), 10)
	if !filled(&m) {
		t.Fatalf("setup: view should fill the box before the resize, crop=%+v", m.crop)
	}
	resizeTo(&m, 60, 80)
	pressZoom(&m, 'z')
	if !filled(&m) {
		t.Fatalf("zoom-in after resize left the view letterboxed, crop=%+v", m.crop)
	}
}

// A fill crop that renders at a lower magnification in the new box must not
// jump up when zooming out.
func TestResizeMidZoomZoomOutNeverMagnifies(t *testing.T) {
	m := zoomedModel(t, image.Pt(300, 1800), image.Pt(100, 50), 40)
	resizeTo(&m, 70, 50)
	before := onScreenMag(&m)
	if before <= 1 {
		t.Fatalf("setup: want a magnified view after the resize, got %.3fx", before)
	}
	pressZoom(&m, 'Z')
	if got := onScreenMag(&m); math.Abs(before/got-zoomStep) > 1e-9 {
		t.Fatalf("first zoom-out went %.4fx -> %.4fx, want a divide by %.2f", before, got, zoomStep)
	}
	for i := range 60 {
		prev := onScreenMag(&m)
		pressZoom(&m, 'Z')
		if got := onScreenMag(&m); got > prev+1e-9 {
			t.Fatalf("zoom-out %d raised magnification %.4fx -> %.4fx", i+2, prev, got)
		}
	}
	if !m.crop.isFull() {
		t.Fatalf("zoom-out never reached the full image: %+v", m.crop)
	}
}

type namedPane struct {
	name string
	pane image.Point
}

type midZoomCase struct {
	name     string
	img      image.Point
	pane     image.Point
	presses  int
	newPanes []namedPane
}

func midZoomCases() []midZoomCase {
	newPanes := []namedPane{
		{"shrink", image.Pt(60, 30)},
		{"grow", image.Pt(160, 80)},
		{"axis-flip", image.Pt(50, 100)},
	}
	return []midZoomCase{
		{"packed", image.Pt(1800, 600), image.Pt(100, 50), 10, newPanes},
		{"fill", image.Pt(300, 1800), image.Pt(100, 50), 40, newPanes},
	}
}

// After any box change, zoom-in never lowers the on-screen magnification and
// ends on a filled view, and zoom-out always lowers it.
func assertZoomFollowsBox(t *testing.T, m galleryModel) {
	t.Helper()
	zin, zout := m, m

	before := onScreenMag(&zin)
	pressZoom(&zin, 'z')
	if got := onScreenMag(&zin); got < before-1e-9 {
		t.Errorf("zoom-in lowered magnification %.4fx -> %.4fx", before, got)
	}
	for range 60 {
		pressZoom(&zin, 'z')
	}
	if !filled(&zin) {
		t.Errorf("zoom-in never filled the box, crop=%+v", zin.crop)
	}

	before = onScreenMag(&zout)
	if before <= 1+1e-9 {
		t.Fatalf("setup: want a magnified view after the resize, got %.4fx", before)
	}
	for i := 0; !zout.crop.isFull(); i++ {
		if i > 80 {
			t.Fatalf("zoom-out never reached the full image: %+v", zout.crop)
		}
		prev := onScreenMag(&zout)
		pressZoom(&zout, 'Z')
		got := onScreenMag(&zout)
		// A step that lands on rest snaps to the full crop; otherwise it must
		// strictly lower the magnification.
		if got >= prev-1e-9 && !(zout.crop.isFull() && math.Abs(got-1) < 1e-9) {
			t.Fatalf("zoom-out %d did not lower magnification: %.4fx -> %.4fx", i+1, prev, got)
		}
	}
}

func TestResizeMidZoomFollowsBox(t *testing.T) {
	for _, c := range midZoomCases() {
		for _, np := range c.newPanes {
			t.Run(fmt.Sprintf("%s/%s", c.name, np.name), func(t *testing.T) {
				m := zoomedModel(t, c.img, c.pane, c.presses)
				resizeTo(&m, np.pane.X, np.pane.Y)
				assertZoomFollowsBox(t, m)
			})
		}
	}
}

// Exercises only the resize half of a split flip: `s` is a no-op without tmux.
func TestSplitToggleMidZoomFollowsBox(t *testing.T) {
	for _, c := range midZoomCases() {
		for _, np := range c.newPanes {
			t.Run(fmt.Sprintf("%s/%s", c.name, np.name), func(t *testing.T) {
				m := zoomedModel(t, c.img, c.pane, c.presses)
				toggleSplit(t, &m, np.pane.X, np.pane.Y)
				assertZoomFollowsBox(t, m)
			})
		}
	}
}
