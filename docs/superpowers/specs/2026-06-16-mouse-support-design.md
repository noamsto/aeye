# Mouse support for the aeye carousel

**Date:** 2026-06-16
**Status:** Approved design (scope A)

## Goal

Add mouse control to the carousel viewer so the four most-expected interactions
work without touching the keyboard. Region click-to-drill on diagrams is
explicitly **deferred** to a follow-up (see Non-goals) because it needs the
letterbox-aware inverse mapping that the other interactions don't.

## Scope (this PR)

| Input | Region | Action | Reuses |
|-------|--------|--------|--------|
| Left click | filmstrip cell | select that image | `selectIndex` |
| Wheel up / down | filmstrip | previous / next image | `selectIndex(cursor∓1)` |
| Wheel up / down | preview | zoom in / out, anchored at the pointer | `zoomBy` + `panBy` |
| Left drag | preview | pan the zoomed crop | `panBy` |

Mouse is **always on** for the session. Native terminal text-selection still
works while holding Shift.

## Non-goals (deferred)

- **Click a diagram region to drill in.** Correct hit-testing requires inverting
  the renderer's fit-to-box math (source aspect vs the fixed ~16:9 box, plus
  non-square cell pixels `cellPxW/cellPxH`) to turn a screen cell into an
  image-space fraction. That inversion, with its own tests, is a focused
  follow-up. Nothing in this PR should pretend the preview image fills the inner
  box.
- Middle/right-click, hover effects, click-to-open. Out of scope.

## Architecture

### Enabling mouse
In `View()` (`gallery.go:343`), set `v.MouseMode = tea.MouseModeCellMotion`
alongside the existing `v.AltScreen = true`. Cell-motion mode delivers click,
release, wheel, and motion-while-button-held (needed for drag) without flooding
events on idle hover. This is a per-`View` property in this bubbletea v2 fork
(`charm.land/bubbletea/v2`), not a `tea.NewProgram` option.

### New file: `gallery_mouse.go`
All mouse logic lives here, matching the existing `gallery_zoom.go` /
`gallery_regions.go` split. `Update` (`gallery.go:187`) gains a thin dispatch:

```go
case tea.MouseMsg:
    return m.handleMouse(msg)
```

`tea.MouseMsg` is the interface implemented by all four concrete event types, so
one case catches them; `handleMouse` type-switches on
`MouseClickMsg / MouseReleaseMsg / MouseWheelMsg / MouseMotionMsg`. Each carries
`X, Y int` (zero-based, origin top-left) and `Button`.

### Shared layout geometry (refactor `renderView`)
`renderView` currently computes all positions inline and exposes none. Extract
two pure helpers used by **both** `renderView` and `handleMouse`, so hit-testing
can never drift from rendering:

- `previewRect() rect` — the inner image cell area of the centered, framed
  preview box.
- `filmstripCellRects() []rect` — one rect per visible thumbnail cell, in
  filmstrip order (cell `i` ⇒ image index `stripStart(cursor,…) + i`).

`rect` is a small `{x, y, w, h int}` value with a `contains(x, y) bool` method.
The vertical bands fall out of the `JoinVertical(title, subtitle, previewArea,
filmstrip, legend)` stack:

| Band | Rows |
|------|------|
| title / subtitle | y = 0, 1 |
| preview | 2 ≤ y < 2 + (height − stripH − 6) |
| filmstrip | next stripH + 2 rows |
| legend | last 2 rows |

Helpers derive purely from `m.l` (computeLayout output), `m.width`, `m.height`,
and `m.cursor` / `len(m.images)`.

## Interaction details

### Filmstrip click & wheel
- **Click:** find the cell rect containing `(X, Y)`; if any, `selectIndex(start + i)`.
- **Wheel up:** `selectIndex(m.cursor - 1)`. **Wheel down:** `selectIndex(m.cursor + 1)`.
  (`selectIndex` already clamps and unpins.)

### Preview wheel zoom (pointer-anchored)
Wheel over the preview rect zooms using the same factors as the `z`/`Z` keys:
- Record the pointed image-space point *before* zoom (within the current crop):
  `fx = (X − inner.x + 0.5) / inner.w`, `fy = (Y − inner.y + 0.5) / inner.h`,
  each clamped to [0,1]. (This treats the inner box as the crop window; it is
  approximate under letterbox, which is acceptable for *anchoring* — we are not
  resolving a region, just keeping the pointed spot roughly stationary.)
- `zoomBy(factor)` (zooms about the crop center, preserving existing behavior).
- Then a single `panBy` nudges the crop so the pointed fraction stays near the
  pointer: shift by `(fx − 0.5, fy − 0.5)` scaled by how much the crop shrank.
  `panBy` clamps within `[0, 1−w]`, so this is safe at the edges and no-ops at
  full crop.

### Preview drag pan
One mechanism, click-vs-drag disambiguation:
- `MouseClickMsg` (left) inside the preview rect → record `lastDragX/Y`, set
  `dragging = true`, `dragMoved = false`.
- `MouseMotionMsg` (left button held) while `dragging` → if the crop is not full
  (pan is actually possible), set `dragMoved = true` and
  `panBy(−Δx / inner.w, −Δy / inner.h)` (drag direction follows the cursor),
  then update `lastDragX/Y`. At full crop, ignore motion (no dead-drag
  classification — see edge cases).
- `MouseReleaseMsg` → `dragging = false`. (No click action on plain-image
  release in this PR; region-drill — the future click action — is deferred.)

### Undefined regions
Wheel or click over the title/subtitle/legend bands, or a preview wheel when no
image is decoded, is a no-op.

## New model state
Four small fields on `galleryModel`:

```go
dragging           bool
dragMoved          bool
lastDragX, lastDragY int
```

## Edge cases

- **Tiny panes:** band math is derived from `computeLayout`, which clamps; clicks
  outside any rect are no-ops.
- **Full crop drag:** drag does nothing because `panBy` no-ops at full crop, and
  motion is ignored so it is never misclassified as a drag.
- **Filmstrip centering:** offset is `(width − totalCellsWidth) / 2`, mirroring
  `lipgloss.PlaceHorizontal(Center)`; a partial last window uses
  `min(stripCols, n − start)` cells.

## Testing

Pure geometry → table-driven unit tests in `gallery_mouse_test.go`, matching the
style of `gallery_zoom_test.go`:

- `filmstripCellRects`: full window, partial last window, single cell, centering
  offset; a click X maps to the expected image index; clicks in gutters / outside
  map to none.
- `previewRect`: inner area for representative pane sizes.
- Band classification: a `(x, y)` resolves to the correct region (title /
  preview / filmstrip / legend / none).
- Wheel navigation: `cursor` clamps at both ends.

**Manual verification (tmux):** the viewer runs in a tmux split. tmux only
forwards mouse events to an app that has requested mouse reporting, and behavior
interacts with the user's `tmux mouse on` setting and SGR encoding through
passthrough. Manually confirm in a real tmux session: click-select, wheel both
ways over filmstrip and preview, drag-pan when zoomed, and that Shift-drag still
does native text selection.

## Out-of-scope follow-up
Region click-to-drill: invert the fit-to-box mapping (crop pixel aspect vs box,
`cellPxW/cellPxH`) to convert a preview cell to an image-space fraction, then
find the child region at the current `regionPath` containing it and `drillIn`.
Its own PR, with its own aspect-ratio tests.
