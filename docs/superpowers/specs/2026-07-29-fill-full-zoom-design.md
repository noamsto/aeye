# Gradual zoom + explicit fill/full toggle

## Problem

For wide or tall images (diagrams especially), the first zoom step (`z`, `+/-`,
or mouse wheel) **snaps** from the letterboxed rest view to a box-aspect fill
crop. That suddenly crops the sides (or top/bottom) so the preview packs the
box. The jump feels like the image “clips into” the preview size, and the same
path runs for mouse wheel zoom via `zoomAt` → `zoomBy`.

Users want gradual magnification of the full letterboxed image, and an
**intentional** way to pack the preview when they want fill.

## Goals

- Zoom in/out (keys and wheel) always scales the current crop about its center —
  no snap-to-fill on the first step.
- Default framing for a new selection is **full** (entire image, letterboxed).
- `f` toggles **full ↔ fill** at the current center.
- `0` still resets to full + unzoomed.
- Existing Tab-region magnify-in-place behavior is unchanged.

## Non-goals

- Sticky “prefer fill” mode remembered across images or sessions.
- Preserving magnification when toggling `f` (toggle is a framing rewrite).
- Changing `f` to restore a Tab-framed region (see Edge cases).
- chafa / non-kitty zoom behavior.

## Behavior

| Action | Result |
|--------|--------|
| Open / select / `0` | `fullCrop()` — whole image, letterboxed |
| `z` / `+` / `=` / wheel in | `scaleCropAbout` shrink; never snap to fill |
| `Z` / `-` / `_` / wheel out | Grow crop; when it spills past the image → `fullCrop()` |
| `f` from full (or non-fill crop) | `baseFillCrop()` — box-aspect, packed |
| `f` from a fill crop | `fullCrop()` |
| Square / crop already box-aspect at rest | `f` is a no-op |
| Pan / Tab then zoom | Still magnifies the framed crop in place (aspect preserved) |

Footer hint gains `· f fill` next to the existing zoom keys.

## Design

No new sticky model field. Fill vs full is derived from the crop:

- **full (rest):** `crop.isFull()`
- **fill (rest):** `cropFillsBox()` and not full (the `baseFillCrop` shape)
- Zoomed views are just smaller crops; mode for the next `f` uses
  `cropFillsBox()`: if the current crop’s aspect already matches the box, `f`
  goes to full; otherwise `f` goes to `baseFillCrop()` at the current center.

### `zoomBy`

Remove the branch:

```go
if m.crop.isFull() && !m.cropFillsBox() {
    m.crop = m.baseFillCrop()
    return
}
```

Zoom-in always `scaleCropAbout(m.crop, 1/factor)`. Zoom-out keeps the existing
grow-until-spill → `fullCrop()` path (that is ordinary unzoom, not a fill-mode
special case).

`zoomAt` (mouse) needs no change; it already calls `zoomBy`.

### `toggleFill`

New `galleryModel` method:

1. If `curImg == nil`, return.
2. If `cropFillsBox()` and not `isFull()` → `fullCrop()`.
3. Else → `baseFillCrop()` (no-op when the image already matches the box, since
   `baseFillCrop` equals full for that case).

Wire `case "f":` in `Update` like zoom: `toggleFill()` + `transmitPreviewOnly()`
(+ existing sharp d2 re-render scheduling, same as other preview-only keys).

### Tests

Replace / rewrite:

- `TestZoomBySnapsToBoxAspectFill` → first zoom on a wide image **preserves
  image aspect** (gradual), does not jump to `w=2/9`.
- `TestZoomOutFromFillSnapsToRest` → either drop or reframe as “zoom-out from a
  fill crop grows until full” without requiring a prior snap.

Add:

- `TestToggleFillFromFull` — wide model, `f` → fill crop centered.
- `TestToggleFillFromFill` — fill crop, `f` → full.
- `TestToggleFillNoopWhenSquare` — image aspect matches box → crop unchanged.

Keep framed-region magnify and long-side clamp tests.

## Edge cases

- **Unsized / not ready:** existing input guards; `f` never runs before layout.
- **Tab-framed region:** `f` still rewrites to `fullCrop` / `baseFillCrop` of the
  **whole image**. It does not restore the Tab frame. Re-Tab to re-frame.
  Deliberate simplicity for v1.
- **Already zoomed, then `f`:** jumps to fill-at-center or full image; does not
  keep the previous magnification. User zooms again from the new framing.

## Files

| Path | Change |
|------|--------|
| `gallery_zoom.go` | Remove snap in `zoomBy`; add `toggleFill` |
| `gallery.go` | Key `f`; footer hint |
| `gallery_zoom_test.go` | Replace snap tests; add toggle tests |

## Success criteria

- Wide diagram: repeated `z` / wheel enlarges smoothly while letterboxed; edges
  are not suddenly cropped on the first step.
- `f` packs the preview; `f` again shows the whole image.
- `0` and selection change still land on full unzoomed.
- Existing unit tests pass; new tests cover gradual first zoom and `f`.
