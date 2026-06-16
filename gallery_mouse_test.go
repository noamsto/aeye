package main

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// A 120x40 pane: computeLayout gives a known layout we can hit-test against.
func mouseModel(width, height, cursor, nimages int) galleryModel {
	m := galleryModel{
		width:  width,
		height: height,
		cursor: cursor,
		images: make([]imageEntry, nimages),
	}
	m.l = computeLayout(width, height)
	return m
}

func TestRectContains(t *testing.T) {
	r := rect{x: 5, y: 10, w: 4, h: 3}
	cases := []struct {
		x, y int
		want bool
	}{
		{5, 10, true}, {8, 12, true}, {9, 10, false}, {5, 13, false}, {4, 10, false},
	}
	for _, c := range cases {
		if got := r.contains(c.x, c.y); got != c.want {
			t.Errorf("contains(%d,%d) = %v, want %v", c.x, c.y, got, c.want)
		}
	}
}

func TestFilmstripCellRectsFullWindow(t *testing.T) {
	m := mouseModel(120, 40, 0, 50) // many images → full window of stripCols cells
	rects := m.filmstripCellRects()
	if len(rects) != m.l.stripCols {
		t.Fatalf("got %d cells, want stripCols=%d", len(rects), m.l.stripCols)
	}
	cellW := m.l.stripW + 2
	for i := 1; i < len(rects); i++ {
		gap := rects[i].x - (rects[i-1].x + cellW)
		if gap != stripGutter {
			t.Errorf("cell %d gap = %d, want %d", i, gap, stripGutter)
		}
		if rects[i].w != cellW {
			t.Errorf("cell %d width = %d, want %d", i, rects[i].w, cellW)
		}
	}
	// Row sits in the filmstrip band: top = height - stripH - 4.
	if rects[0].y != m.height-m.l.stripH-4 {
		t.Errorf("cell y = %d, want %d", rects[0].y, m.height-m.l.stripH-4)
	}
	if rects[0].h != m.l.stripH+2 {
		t.Errorf("cell height = %d, want %d", rects[0].h, m.l.stripH+2)
	}
}

func TestFilmstripCellRectsPartialWindow(t *testing.T) {
	m := mouseModel(120, 40, 0, 3) // fewer images than stripCols → 3 cells
	if got := len(m.filmstripCellRects()); got != 3 {
		t.Errorf("partial window cells = %d, want 3", got)
	}
}

func TestFilmstripHit(t *testing.T) {
	m := mouseModel(120, 40, 0, 50)
	rects := m.filmstripCellRects()
	mid := rects[2]
	idx, ok := m.filmstripHit(mid.x+1, mid.y+1)
	if !ok || idx != 2 { // cursor 0 → stripStart 0 → cell 2 is image 2
		t.Errorf("hit cell 2 = (%d,%v), want (2,true)", idx, ok)
	}
	cellW := m.l.stripW + 2
	if idx, ok := m.filmstripHit(rects[0].x+1, rects[0].y+1); !ok || idx != 0 {
		t.Errorf("hit cell 0 = (%d,%v), want (0,true)", idx, ok)
	}
	last := len(rects) - 1
	if idx, ok := m.filmstripHit(rects[last].x+1, rects[last].y+1); !ok || idx != last {
		t.Errorf("hit last cell = (%d,%v), want (%d,true)", idx, ok, last)
	}
	if _, ok := m.filmstripHit(rects[0].x+cellW, rects[0].y+1); ok {
		t.Error("click in gutter between cells must not hit any cell")
	}
	if _, ok := m.filmstripHit(mid.x+1, m.height-1); ok {
		t.Error("click in legend must not hit a cell")
	}
}

func TestPreviewRectInsideBand(t *testing.T) {
	m := mouseModel(120, 40, 0, 5)
	pr := m.previewRect()
	if pr.w != m.l.previewW || pr.h != m.l.previewH {
		t.Errorf("preview inner = %dx%d, want %dx%d", pr.w, pr.h, m.l.previewW, m.l.previewH)
	}
	if pr.y < 2 || pr.y+pr.h > m.height-m.l.stripH-4 {
		t.Errorf("preview rect rows %d..%d escape the preview band", pr.y, pr.y+pr.h)
	}
}

func TestOverFilmstripBand(t *testing.T) {
	m := mouseModel(120, 40, 0, 50)
	top := m.height - m.l.stripH - 4
	if !m.overFilmstripBand(top) || !m.overFilmstripBand(top+m.l.stripH+1) {
		t.Error("filmstrip band rows should report true")
	}
	if m.overFilmstripBand(2) || m.overFilmstripBand(m.height-1) {
		t.Error("preview/legend rows should report false")
	}
	if m.overFilmstripBand(top + m.l.stripH + 2) {
		t.Error("row just below the band should be false")
	}
}

func TestPreviewRectDegeneratePane(t *testing.T) {
	m := mouseModel(30, 12, 0, 5) // tiny pane → box can't fit the band
	pr := m.previewRect()
	if pr != (rect{}) {
		t.Errorf("degenerate pane previewRect = %+v, want zero rect", pr)
	}
	if pr.contains(15, 6) {
		t.Error("zero rect must not contain any point")
	}
}

func TestZoomAtKeepsPointStationary(t *testing.T) {
	m := mouseModel(120, 40, 0, 5)
	m.crop = cropFrac{0, 0, 1, 1}
	pr := m.previewRect()
	// Pointer near the top-left quarter of the preview.
	sx, sy := pr.x+pr.w/4, pr.y+pr.h/4
	fx := (float64(sx-pr.x) + 0.5) / float64(pr.w)
	fy := (float64(sy-pr.y) + 0.5) / float64(pr.h)
	wantX := m.crop.x0 + fx*m.crop.w()
	wantY := m.crop.y0 + fy*m.crop.h()
	m.zoomAt(sx, sy, 1.25)
	// The image point under the pointer must map back to the same screen frac.
	gotX := m.crop.x0 + fx*m.crop.w()
	gotY := m.crop.y0 + fy*m.crop.h()
	if !approx(gotX, wantX) || !approx(gotY, wantY) {
		t.Errorf("anchor drifted: got (%v,%v) want (%v,%v)", gotX, gotY, wantX, wantY)
	}
	if m.crop.isFull() {
		t.Error("zoom-in must shrink the crop")
	}
}

func TestZoomAtZoomOutToFull(t *testing.T) {
	m := mouseModel(120, 40, 0, 5)
	m.crop = cropFrac{0.25, 0.25, 0.75, 0.75} // zoomed-in starting state
	pr := m.previewRect()
	sx, sy := pr.x+pr.w/2, pr.y+pr.h/2
	for i := 0; i < 11; i++ {
		m.zoomAt(sx, sy, 1/1.25)
	}
	if !m.crop.isFull() {
		t.Errorf("repeated zoom-out must reach fullCrop, got %+v", m.crop)
	}
}

func TestDragPansWhenZoomed(t *testing.T) {
	m := mouseModel(120, 40, 0, 1)
	m.ready = true
	m.crop = cropFrac{0.25, 0.25, 0.75, 0.75} // zoomed in so panBy is live
	pr := m.previewRect()
	cx, cy := pr.x+pr.w/2, pr.y+pr.h/2
	// Click in the preview starts a drag.
	m2, _ := m.handleMouse(tea.MouseClickMsg{X: cx, Y: cy, Button: tea.MouseLeft})
	if !m2.dragging {
		t.Fatal("click in preview should start dragging")
	}
	before := m2.crop
	// Drag right+down by a few cells → crop must move.
	m3, _ := m2.handleMouse(tea.MouseMotionMsg{X: cx + 5, Y: cy + 3, Button: tea.MouseLeft})
	if m3.crop == before {
		t.Error("drag motion should pan the crop")
	}
	// Release ends the drag.
	m4, _ := m3.handleMouse(tea.MouseReleaseMsg{X: cx + 5, Y: cy + 3, Button: tea.MouseLeft})
	if m4.dragging {
		t.Error("release should end dragging")
	}
}

func TestDragIgnoredAtFullCrop(t *testing.T) {
	m := mouseModel(120, 40, 0, 1)
	m.ready = true
	m.crop = fullCrop()
	pr := m.previewRect()
	cx, cy := pr.x+pr.w/2, pr.y+pr.h/2
	m2, _ := m.handleMouse(tea.MouseClickMsg{X: cx, Y: cy, Button: tea.MouseLeft})
	before := m2.crop
	m3, _ := m2.handleMouse(tea.MouseMotionMsg{X: cx + 5, Y: cy + 3, Button: tea.MouseLeft})
	if m3.crop != before {
		t.Error("drag at full crop must not pan")
	}
}
