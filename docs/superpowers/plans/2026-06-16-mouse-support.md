# Mouse Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add click-to-select, wheel-navigate, pointer-anchored wheel-zoom, and drag-to-pan to the aeye carousel.

**Architecture:** Mouse is enabled as a per-`View` property (`v.MouseMode = tea.MouseModeCellMotion`) in this bubbletea v2 fork. A new `gallery_mouse.go` holds pure geometry helpers (screen cell → filmstrip index / preview rect) plus a `handleMouse` orchestrator dispatched from `Update`. All actions reuse existing mutators (`selectIndex`, `zoomBy`, `panBy`) and repaint exactly like the keyboard paths.

**Tech Stack:** Go, `charm.land/bubbletea/v2`, `charm.land/lipgloss/v2`.

**Spec:** `docs/superpowers/specs/2026-06-16-mouse-support-design.md`

---

## File Structure

- **Create `gallery_mouse.go`** — `rect` type; geometry helpers `previewRect`, `filmstripCellRects`, `filmstripHit`, `overFilmstripBand`; `handleMouse`; `zoomAt`.
- **Create `gallery_mouse_test.go`** — table-driven tests for the geometry helpers and `zoomAt` anchoring.
- **Modify `gallery.go`** — enable mouse in `View()` (line ~349); add `case tea.MouseMsg` to `Update` (line ~188); add four drag-state fields to `galleryModel` (struct at line ~83).

All geometry mirrors `renderView`'s `JoinVertical(title, subtitle, previewArea, filmstrip, legend)` stack: title+subtitle occupy rows 0–1, the preview band is `height - stripH - 6` rows tall starting at row 2, the filmstrip is `stripH + 2` rows, the legend is the last 2.

---

## Task 1: Geometry helpers + rect type

**Files:**
- Create: `gallery_mouse.go`
- Test: `gallery_mouse_test.go`

- [ ] **Step 1: Write failing tests**

Create `gallery_mouse_test.go`:

```go
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
	if m.overFilmstripBand(2) || m.overFilmstripBand(m.height - 1) {
		t.Error("preview/legend rows should report false")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `direnv exec . go test ./... -run 'TestRect|TestFilmstrip|TestPreviewRect|TestOverFilmstrip' 2>&1 | tail`
Expected: FAIL — `undefined: rect`, `m.filmstripCellRects` etc.

- [ ] **Step 3: Create `gallery_mouse.go` with the helpers**

```go
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
// and centers it in the preview band, which starts at row 2 (title+subtitle)
// and is height-stripH-6 rows tall.
func (m galleryModel) previewRect() rect {
	const bandTop = 2
	bandH := m.height - m.l.stripH - 6
	boxW, boxH := m.l.previewW+2, m.l.previewH+2
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `direnv exec . go test ./... -run 'TestRect|TestFilmstrip|TestPreviewRect|TestOverFilmstrip' 2>&1 | tail`
Expected: PASS (ok).

- [ ] **Step 5: Commit**

```bash
git add gallery_mouse.go gallery_mouse_test.go
git commit -m "feat: mouse hit-test geometry helpers (#47)"
```

---

## Task 2: Enable mouse, dispatch, click-select + wheel-navigate

**Files:**
- Modify: `gallery.go` (struct ~83, `Update` ~188, `View` ~349)
- Modify: `gallery_mouse.go` (add `handleMouse`, drop the temporary `var _`)

- [ ] **Step 1: Add drag-state fields to `galleryModel`**

In `gallery.go`, inside the `galleryModel` struct (after `vecGen uint64` at line ~102):

```go
	// Mouse drag state for preview panning.
	dragging             bool
	lastDragX, lastDragY int
```

- [ ] **Step 2: Enable mouse in `View()`**

In `gallery.go` `View()` (after `v.AltScreen = true`, line ~349):

```go
	v.MouseMode = tea.MouseModeCellMotion
```

- [ ] **Step 3: Dispatch mouse events in `Update`**

In `gallery.go` `Update`, add a case after the `tea.KeyPressMsg` block (before `case vectorKickMsg:`, line ~295):

```go
	case tea.MouseMsg:
		var cmd tea.Cmd
		m, cmd = m.handleMouse(msg)
		return m, cmd
```

- [ ] **Step 4: Implement `handleMouse` (click + wheel navigation only for now)**

In `gallery_mouse.go`, remove the `var _ = tea.MouseLeft` line and add:

```go
// handleMouse turns mouse events into the same actions as the keyboard paths,
// then schedules a sharp d2 re-render like every other input.
func (m galleryModel) handleMouse(msg tea.MouseMsg) (galleryModel, tea.Cmd) {
	if !m.ready || len(m.images) == 0 {
		return m, nil
	}
	e := msg.Mouse()
	switch msg.(type) {
	case tea.MouseWheelMsg:
		dir := 0
		switch e.Button {
		case tea.MouseWheelUp:
			dir = -1
		case tea.MouseWheelDown:
			dir = +1
		}
		if dir != 0 && m.overFilmstripBand(e.Y) {
			m.selectIndex(m.cursor + dir)
		}
	case tea.MouseClickMsg:
		if e.Button == tea.MouseLeft {
			if idx, ok := m.filmstripHit(e.X, e.Y); ok {
				m.selectIndex(idx)
			}
		}
	}
	return m, m.scheduleVector()
}
```

- [ ] **Step 5: Build and run the full suite**

Run: `direnv exec . go build ./... && direnv exec . go test ./... 2>&1 | tail`
Expected: build clean, all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add gallery.go gallery_mouse.go
git commit -m "feat: click-select and wheel-navigate the filmstrip (#47)"
```

---

## Task 3: Pointer-anchored wheel zoom over the preview

**Files:**
- Modify: `gallery_mouse.go` (`handleMouse` wheel branch, add `zoomAt`)
- Test: `gallery_mouse_test.go`

- [ ] **Step 1: Write a failing test for `zoomAt` anchoring**

Add to `gallery_mouse_test.go`:

```go
func TestZoomAtKeepsPointStationary(t *testing.T) {
	m := mouseModel(120, 40, 0, 5)
	m.crop = cropFrac{0, 0, 1, 1}
	pr := m.previewRect()
	// Pointer near the top-left quarter of the preview.
	sx, sy := pr.x+pr.w/4, pr.y+pr.h/4
	fx := (float64(sx-pr.x) + 0.5) / float64(pr.w)
	fy := (float64(sy-pr.y) + 0.5) / float64(pr.h)
	want := cropFrac{m.crop.x0 + fx*m.crop.w(), m.crop.y0 + fy*m.crop.h(), 0, 0}
	m.zoomAt(sx, sy, 1.25)
	// The image point under the pointer must map back to the same screen frac.
	gotX := m.crop.x0 + fx*m.crop.w()
	gotY := m.crop.y0 + fy*m.crop.h()
	if !approx(gotX, want.x0) || !approx(gotY, want.y0) {
		t.Errorf("anchor drifted: got (%v,%v) want (%v,%v)", gotX, gotY, want.x0, want.y0)
	}
	if m.crop.isFull() {
		t.Error("zoom-in must shrink the crop")
	}
}
```

- [ ] **Step 2: Run it to verify failure**

Run: `direnv exec . go test ./... -run TestZoomAt 2>&1 | tail`
Expected: FAIL — `m.zoomAt undefined`.

- [ ] **Step 3: Add `zoomAt` and wire it into the wheel branch**

In `gallery_mouse.go`, add:

```go
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
```

Then change the wheel branch of `handleMouse` so the preview zooms and the
filmstrip navigates:

```go
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
		} else if m.overFilmstripBand(e.Y) {
			m.selectIndex(m.cursor + dir)
		}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `direnv exec . go test ./... -run TestZoomAt 2>&1 | tail`
Expected: PASS.

- [ ] **Step 5: Build + full suite**

Run: `direnv exec . go build ./... && direnv exec . go test ./... 2>&1 | tail`
Expected: build clean, all PASS.

- [ ] **Step 6: Commit**

```bash
git add gallery_mouse.go gallery_mouse_test.go
git commit -m "feat: pointer-anchored wheel zoom over the preview (#47)"
```

---

## Task 4: Left-drag to pan the preview

**Files:**
- Modify: `gallery_mouse.go` (`handleMouse` — add click/motion/release drag handling)

- [ ] **Step 1: Add click-start, motion-pan, and release branches**

In `gallery_mouse.go` `handleMouse`, extend the `MouseClickMsg` branch and add
two new cases. The full switch becomes:

```go
	switch msg.(type) {
	case tea.MouseWheelMsg:
		// ... unchanged from Task 3 ...
	case tea.MouseClickMsg:
		if e.Button == tea.MouseLeft {
			if idx, ok := m.filmstripHit(e.X, e.Y); ok {
				m.selectIndex(idx)
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
			}
		}
	case tea.MouseReleaseMsg:
		m.dragging = false
	}
```

> Note: the deferred region-click follow-up will need to distinguish a click
> from a drag on release; it will add a `dragMoved` flag then, when there is a
> reader for it.

- [ ] **Step 2: Build + full suite**

Run: `direnv exec . go build ./... && direnv exec . go test ./... 2>&1 | tail`
Expected: build clean, all PASS.

- [ ] **Step 3: Lint**

Run: `direnv exec . golangci-lint run 2>&1 | tail`
Expected: no new findings (if the repo lints in pre-commit, this mirrors it).

- [ ] **Step 4: Commit**

```bash
git add gallery_mouse.go
git commit -m "feat: left-drag to pan the zoomed preview (#47)"
```

---

## Task 5: Manual verification in tmux + open PR

The viewer runs in a tmux split, and tmux only forwards mouse events to an app
that has requested mouse reporting. This step cannot be unit-tested.

- [ ] **Step 1: Launch the viewer and exercise mouse**

Run the app per the project's run path (e.g. the `aeye` binary in a tmux pane
with a populated manifest). Confirm:
  - Click a filmstrip thumbnail → selection jumps to it.
  - Wheel over the filmstrip → selection moves prev/next; clamps at both ends.
  - Wheel over the preview → zooms toward the pointer; wheel-out returns to fit.
  - Drag the preview while zoomed → image pans with the cursor; release stops it.
  - Hold **Shift** and drag → native terminal text selection still works.
  - Toggle the user's `tmux` `mouse` setting off/on and re-confirm events arrive.

- [ ] **Step 2: Update the legend (optional polish)**

If the manual run shows the legend (`gallery.go:433-434`) should mention mouse,
add a short hint; otherwise leave it. Commit only if changed:

```bash
git add gallery.go
git commit -m "docs: note mouse controls in the legend (#47)"
```

- [ ] **Step 3: Push and open the PR**

```bash
git push -u origin feat/47-mouse-support
gh pr create --assignee @me --title "feat: mouse support for the carousel viewer" \
  --body "Closes #47. Click-select, wheel-navigate, pointer-anchored wheel-zoom, and drag-to-pan. Region click-to-drill deferred to a follow-up (see spec). Geometry is unit-tested; mouse-in-tmux verified manually."
```

---

## Self-Review notes

- **Spec coverage:** click-select (Task 2), wheel-navigate (Task 2), wheel-zoom anchored (Task 3), drag-pan (Task 4), enable+always-on (Task 2), tests (Tasks 1/3), tmux manual verify (Task 5). Region-drill explicitly deferred — no task, by design.
- **Type consistency:** `rect`/`contains`, `previewRect`, `filmstripCellRects`, `filmstripHit`, `overFilmstripBand`, `zoomAt`, and the four `dragging/dragMoved/lastDragX/lastDragY` fields are named identically across tasks.
- **Coupling caveat (carried from the spec review):** the geometry helpers mirror `renderView`'s lipgloss placement rather than sharing one source — placement is delegated to lipgloss, so a single-source extraction isn't clean. The band/centering arithmetic uses the same `computeLayout` outputs and constants, and Task 1's tests pin it. lipgloss centering may differ by one column at box edges; acceptable for these targets.
