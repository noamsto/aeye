package main

import "testing"

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
}
