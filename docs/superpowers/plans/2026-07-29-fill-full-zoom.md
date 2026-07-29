# Gradual Zoom + Fill/Full Toggle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make zoom (keys + wheel) always gradual for wide/tall images, and add `f` to toggle letterboxed full ↔ packed fill framing.

**Architecture:** Remove the first-zoom snap-to-`baseFillCrop` branch from `zoomBy` so every zoom step is `scaleCropAbout` / grow-until-full. Add `toggleFill()` that rewrites the crop between `fullCrop()` and `baseFillCrop()` based on `cropFillsBox()`. Wire `f` like other preview-only keys; mouse inherits the fixed `zoomBy` via existing `zoomAt`.

**Tech Stack:** Go, existing `gallery_zoom.go` crop math, bubbletea key handling in `gallery.go`.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-29-fill-full-zoom-design.md`
- No new sticky mode field on `galleryModel` — fill vs full is derived from the crop.
- `f` does not preserve magnification; it rewrites framing at the current center.
- Tab-framed regions: `f` still jumps to whole-image full/fill (does not restore the Tab frame).
- Match existing style: small methods, early returns, comments only for non-obvious why.
- Run tests with `go test -count=1 ./...` from the repo root (or worktree).

## File Structure

| Path | Responsibility |
|------|----------------|
| `gallery_zoom.go` | `zoomBy` (no snap), new `toggleFill` |
| `gallery_zoom_test.go` | Gradual-zoom + toggleFill unit tests |
| `gallery.go` | `case "f"` in `Update`; footer `navKeys` hint |

No new files.

---

### Task 1: Gradual first zoom (remove snap-to-fill)

**Files:**
- Modify: `gallery_zoom.go` (`zoomBy`, ~lines 98–123)
- Modify: `gallery_zoom_test.go` (`TestZoomBySnapsToBoxAspectFill`, `TestZoomOutFromFillSnapsToRest`, `TestZoomDeeperPreservesAspect`)

**Interfaces:**
- Consumes: `scaleCropAbout`, `fullCrop`, `wideModel`, existing `zoomBy(factor float64)`
- Produces: `zoomBy` with no fill snap; first zoom on a wide image preserves image aspect

- [ ] **Step 1: Rewrite the failing / intent-changed tests**

In `gallery_zoom_test.go`, replace `TestZoomBySnapsToBoxAspectFill` with:

```go
func TestZoomByFirstStepPreservesImageAspect(t *testing.T) {
	m := wideModel()
	m.zoomBy(1.25) // must magnify gradually — no snap to box-aspect fill
	if !approx(m.crop.w(), 1/1.25) || !approx(m.crop.h(), 1/1.25) {
		t.Errorf("first zoom-in crop = %+v, want w=h=0.8", m.crop)
	}
	if !approx(m.crop.cx(), 0.5) || !approx(m.crop.cy(), 0.5) {
		t.Errorf("first zoom must stay centered, got %+v", m.crop)
	}
}
```

Replace `TestZoomOutFromFillSnapsToRest` with a test that starts on an explicit fill crop (no prior snap):

```go
func TestZoomOutFromFillReachesFull(t *testing.T) {
	m := wideModel()
	m.crop = m.baseFillCrop() // full-height slice; h == 1
	m.zoomBy(1 / 1.25)
	if !m.crop.isFull() {
		t.Errorf("zoom-out from a full-height fill must reach full, got %+v", m.crop)
	}
}
```

Update `TestZoomDeeperPreservesAspect` so it no longer assumes a fill snap on the first press — zoom twice from full and assert aspect stays the image's (1:1 in crop fractions for a uniform scale from full):

```go
func TestZoomDeeperPreservesAspect(t *testing.T) {
	m := wideModel()
	m.zoomBy(1.25)
	want := m.crop.w() / m.crop.h()
	m.zoomBy(1.25)
	if got := m.crop.w() / m.crop.h(); !approx(got, want) {
		t.Errorf("deeper zoom changed aspect: %v -> %v", want, got)
	}
}
```

- [ ] **Step 2: Run tests — expect the new first-zoom test to FAIL while snap remains**

```bash
go test -count=1 -run 'ZoomByFirstStep|ZoomOutFromFill|ZoomDeeper' .
```

Expected: `TestZoomByFirstStepPreservesImageAspect` FAIL (crop still `w=2/9`, `h=1` from the snap). The other two may already PASS depending on paths.

- [ ] **Step 3: Remove the snap branch from `zoomBy`**

In `gallery_zoom.go`, replace `zoomBy` with:

```go
// zoomBy moves the crop one zoom step (factor > 1 zooms in). Every step scales
// the current crop about its center with aspect preserved — including the first
// zoom-in from the letterboxed rest view of a wide/tall image. Use toggleFill
// (key f) to pack the preview to the box. Zooming out grows the crop until it
// spills past the image, then snaps back to the rest view.
func (m *galleryModel) zoomBy(factor float64) {
	if factor > 1 {
		m.crop = scaleCropAbout(m.crop, 1/factor)
		return
	}
	if m.crop.isFull() {
		return
	}
	w, h := m.crop.w()/factor, m.crop.h()/factor
	if w >= 1 || h >= 1 {
		m.crop = fullCrop()
		return
	}
	m.crop = recenterScaled(m.crop.cx(), m.crop.cy(), w, h)
}
```

Leave `baseFillCrop` and `cropFillsBox` in place — Task 2 uses them.

- [ ] **Step 4: Run tests — expect PASS**

```bash
go test -count=1 -run 'ZoomBy|ZoomOutFromFill|ZoomDeeper|ZoomIn|PanBy|ResetZoom|Crop' .
```

Expected: PASS. Also run the full package once:

```bash
go test -count=1 ./...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add gallery_zoom.go gallery_zoom_test.go
git commit -m "$(cat <<'EOF'
fix(zoom): magnify gradually instead of snapping to fill

EOF
)"
```

---

### Task 2: `toggleFill` method (TDD)

**Files:**
- Modify: `gallery_zoom.go` (add `toggleFill` near `zoomBy`)
- Modify: `gallery_zoom_test.go` (add three tests)

**Interfaces:**
- Consumes: `baseFillCrop()`, `fullCrop()`, `cropFillsBox()`, `wideModel()`
- Produces: `func (m *galleryModel) toggleFill()`

- [ ] **Step 1: Write the failing tests**

Append to `gallery_zoom_test.go`:

```go
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
```

- [ ] **Step 2: Run tests — expect FAIL (undefined `toggleFill`)**

```bash
go test -count=1 -run 'ToggleFill' .
```

Expected: FAIL — `m.toggleFill undefined`.

- [ ] **Step 3: Implement `toggleFill`**

Add to `gallery_zoom.go` immediately after `zoomBy`:

```go
// toggleFill switches between letterboxed full-image framing and a box-aspect
// fill crop centered on the current view. Magnification is not preserved — this
// is a framing rewrite. When the image already matches the preview box,
// baseFillCrop equals fullCrop, so the call is a no-op.
func (m *galleryModel) toggleFill() {
	if m.curImg == nil {
		return
	}
	if m.cropFillsBox() && !m.crop.isFull() {
		m.crop = fullCrop()
		return
	}
	m.crop = m.baseFillCrop()
}
```

- [ ] **Step 4: Run tests — expect PASS**

```bash
go test -count=1 -run 'ToggleFill|ZoomBy|ZoomOutFromFill' .
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add gallery_zoom.go gallery_zoom_test.go
git commit -m "$(cat <<'EOF'
feat(zoom): add toggleFill for full ↔ packed framing

EOF
)"
```

---

### Task 3: Wire `f` key + footer hint

**Files:**
- Modify: `gallery.go` (`Update` key switch ~514–520; `navKeys` ~948)

**Interfaces:**
- Consumes: `toggleFill()`, `transmitPreviewOnly()`; fall-through already runs `scheduleVector` + `schedulePaint` for key cases that don't early-return
- Produces: `f` key toggles framing; footer mentions `f fill`

- [ ] **Step 1: Add the key case**

In `gallery.go`, immediately after the zoom-out case (`"Z", "-", "_"`), add:

```go
		case "f":
			m.toggleFill()
			m.transmitPreviewOnly()
```

Do **not** early-return — the switch already falls through to `cmd = m.scheduleVector()` so d2 sharp re-render stays consistent with `z`/`Z`.

- [ ] **Step 2: Update the footer hint**

Replace:

```go
	navKeys := "h/l move · n/p page · g/G ends · z/Z zoom · hjkl pan · 0 reset"
```

with:

```go
	navKeys := "h/l move · n/p page · g/G ends · z/Z zoom · f fill · hjkl pan · 0 reset"
```

- [ ] **Step 3: Build + full test suite**

```bash
go build ./... && go test -count=1 ./...
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add gallery.go
git commit -m "$(cat <<'EOF'
feat(zoom): bind f to fill/full toggle and hint it in the footer

EOF
)"
```

---

### Task 4: Manual kitty smoke (optional gate before PR)

**Files:** none (verification only)

- [ ] **Step 1: Build the binary**

```bash
go build -o /tmp/aeye .
# or: nix build .#default
```

- [ ] **Step 2: Open a wide diagram in the carousel (kitty + tmux) and check**

- Rest view letterboxes the whole diagram.
- Wheel / `z` enlarges gradually; first step does **not** crop the sides.
- `f` packs the preview (sides cropped); `f` again restores the whole image.
- `0` returns to full unzoomed.
- Filmstrip does not flicker on `f` / zoom (preview-only retransmit).

- [ ] **Step 3: No commit** unless you fixed something found in smoke.

---

## Self-Review (plan vs spec)

| Spec requirement | Task |
|------------------|------|
| Gradual zoom, no snap | Task 1 |
| Default full on select/`0` | Unchanged existing behavior; Task 1 must not break it |
| `f` toggles full ↔ fill | Tasks 2–3 |
| Square / matched aspect no-op | Task 2 `TestToggleFillNoopWhenMatched` |
| Mouse inherits gradual zoom | Task 1 (`zoomAt` → `zoomBy`) |
| Footer `f fill` | Task 3 |
| Tab frame not restored by `f` | Documented; no code beyond `toggleFill` using whole-image crops |
| Tests for gradual + toggle | Tasks 1–2 |

No placeholders. Method name `toggleFill()` consistent across tasks. `baseFillCrop` / `cropFillsBox` unchanged signatures.
