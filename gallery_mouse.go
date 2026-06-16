package main

import (
	tea "charm.land/bubbletea/v2"
)

// rect is a screen-cell rectangle in the carousel's coordinate space (origin
// top-left, y down). Hit-testing mirrors the geometry renderView hands to
// lipgloss; the two must change together.
type rect struct{ x, y, w, h int }

func (r rect) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

// previewRect is the inner image cell area of the centered, framed preview box.
// renderView frames a previewW×previewH block in a rounded border (+1 per side)
// and centers it in the preview band, which starts at row 2 (title+subtitle).
// previewArea (the lipgloss.Place container) is height-stripH-6 rows; the framed
// box of size (previewW+2)×(previewH+2) is centered within it.
func (m galleryModel) previewRect() rect {
	const bandTop = 2
	bandH := m.height - m.l.stripH - 6
	boxW, boxH := m.l.previewW+2, m.l.previewH+2
	if bandH <= 0 || boxH > bandH {
		return rect{}
	}
	boxLeft := (m.width - boxW) / 2
	boxTop := bandTop + (bandH-boxH)/2
	return rect{x: boxLeft + 1, y: boxTop + 1, w: m.l.previewW, h: m.l.previewH}
}

// filmstripCellRects returns one rect per visible thumbnail in window order;
// cell i corresponds to image index stripStart(cursor,...) + i. Each cell is
// stripW+2 wide (rounded border), separated by stripGutter blanks, the row
// centered in width and placed in the band below the preview.
func (m galleryModel) filmstripCellRects() []rect {
	n := len(m.images)
	start := stripStart(m.cursor, m.l.stripCols, n)
	ncells := m.l.stripCols
	if start+ncells > n {
		ncells = n - start
	}
	if ncells <= 0 {
		return nil
	}
	cellW := m.l.stripW + 2
	totalW := ncells*cellW + (ncells-1)*stripGutter
	left := (m.width - totalW) / 2
	top := m.height - m.l.stripH - 4 // 2 (title+subtitle) + preview band height
	rects := make([]rect, ncells)
	for i := range rects {
		rects[i] = rect{x: left + i*(cellW+stripGutter), y: top, w: cellW, h: m.l.stripH + 2}
	}
	return rects
}

// filmstripHit maps a click to the image index of the cell under it.
func (m galleryModel) filmstripHit(x, y int) (int, bool) {
	start := stripStart(m.cursor, m.l.stripCols, len(m.images))
	for i, r := range m.filmstripCellRects() {
		if r.contains(x, y) {
			return start + i, true
		}
	}
	return 0, false
}

// overFilmstripBand reports whether y falls in the filmstrip's row band (used
// for wheel navigation, so the wheel works over the gutters too).
func (m galleryModel) overFilmstripBand(y int) bool {
	top := m.height - m.l.stripH - 4
	return y >= top && y < top+m.l.stripH+2
}

var _ = tea.MouseLeft // keep the tea import until handleMouse lands (Task 2)
