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

// zoomAt zooms by factor about the preview center (matching the z/Z keys), then
// pans so the image point under (sx, sy) stays beneath the pointer. The fx/fy
// fraction treats the inner box as the crop window — approximate under
// letterbox, which is fine for anchoring (we keep the spot stationary, we do
// not resolve a pixel).
func (m *galleryModel) zoomAt(sx, sy int, factor float64) {
	pr := m.previewRect()
	fx := clampF((float64(sx-pr.x)+0.5)/float64(pr.w), 0, 1)
	fy := clampF((float64(sy-pr.y)+0.5)/float64(pr.h), 0, 1)
	bx := m.crop.x0 + fx*m.crop.w()
	by := m.crop.y0 + fy*m.crop.h()
	m.zoomBy(factor)
	w, h := m.crop.w(), m.crop.h()
	// Want bx == x0 + fx*w; panBy shifts x0 by dx*w, so dx = ((bx-fx*w)-x0)/w.
	m.panBy(((bx-fx*w)-m.crop.x0)/w, ((by-fy*h)-m.crop.y0)/h)
}

// handleMouse turns mouse events into the same actions as the keyboard paths,
// then schedules a sharp d2 re-render like every other input.
func (m galleryModel) handleMouse(msg tea.MouseMsg) (galleryModel, tea.Cmd) {
	if !m.ready || len(m.images) == 0 {
		return m, nil
	}
	e := msg.Mouse()
	changed := false
	switch msg.(type) {
	case tea.MouseWheelMsg:
		dir := 0
		switch e.Button {
		case tea.MouseWheelUp:
			dir = -1
		case tea.MouseWheelDown:
			dir = +1
		}
		if dir == 0 {
			break
		}
		if m.previewRect().contains(e.X, e.Y) {
			factor := 1.25
			if dir > 0 {
				factor = 1 / 1.25
			}
			m.zoomAt(e.X, e.Y, factor)
			m.transmitPreviewOnly()
			changed = true
		} else if m.overFilmstripBand(e.Y) {
			m.selectIndex(m.cursor + dir)
			changed = true
		}
	case tea.MouseClickMsg:
		if e.Button == tea.MouseLeft {
			if idx, ok := m.filmstripHit(e.X, e.Y); ok {
				m.selectIndex(idx)
				changed = true
			} else if m.previewRect().contains(e.X, e.Y) {
				m.dragging = true
				m.lastDragX, m.lastDragY = e.X, e.Y
			}
		}
	case tea.MouseMotionMsg:
		// panBy no-ops at full crop, so dragging an unzoomed image does nothing.
		if m.dragging && e.Button == tea.MouseLeft && !m.crop.isFull() {
			pr := m.previewRect()
			dx := float64(e.X-m.lastDragX) / float64(pr.w)
			dy := float64(e.Y-m.lastDragY) / float64(pr.h)
			if dx != 0 || dy != 0 {
				m.panBy(-dx, -dy) // crop moves opposite the cursor
				m.transmitPreviewOnly()
				m.lastDragX, m.lastDragY = e.X, e.Y
				changed = true
			}
		}
	case tea.MouseReleaseMsg:
		m.dragging = false
	}
	if !changed {
		return m, nil
	}
	return m, m.scheduleVector()
}
