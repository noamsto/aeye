# Carousel kitty-path fixes + zoom-to-fill

Date: 2026-06-14 · Issue: #15

Three problems surfaced while testing the carousel in kitty (no tmux). The first
two are launcher bugs in `scripts/tmux-claude-images.sh`; the third is a
zoom-behavior change in the viewer.

## 1. Binary not found by the kitty/tmux server

`launch_kitty` / `launch_tmux` hand the bare binary name (`${AEYE_BIN:-aeye}`)
to `kitty @ launch` and `tmux split-window`. Those commands run the child in the
**server's** environment, which never saw the PATH the nix wrapper injects via
`runtimeInputs`. So the server can't find `aeye` — "Failed to launch child:
aeye". (Pre-rename installs failed identically as `agent-carousel`.)

**Fix:** resolve `${AEYE_BIN:-aeye}` to an absolute path once, in the wrapper
(whose PATH *does* include it), via `command -v`. Pass that absolute path to
both launchers, so the server's PATH is irrelevant. If resolution fails, print a
clear error and exit.

## 2. Viewer opens below instead of to the right

`launch_kitty` uses `--next-to` but no `--location`. `vsplit`/`hsplit` are
honored only in the `splits` layout; in a stacking layout (`fat`, the observed
default) the new window lands in the bottom row.

**Fix:** inside the existing `KITTY_WINDOW_ID` guard, set the tab to the `splits`
layout (`kitty @ goto-layout --match window_id:$KITTY_WINDOW_ID splits`) and add
`--location=vsplit`. Yields a right-hand split, mirroring the tmux path's
`split-window -h`. When `KITTY_WINDOW_ID` is unset, fall back to a plain launch
(no placement). Tradeoff: `goto-layout` rearranges any other windows already in
Claude's tab; acceptable, and visually identical to `fat` when one window
remains.

## 3. Zoom doesn't fill the box for wide diagrams

`zoomBy` shrinks the crop equally on both axes, keeping it square in *fraction*
space. For a non-square image the crop therefore inherits the source's pixel
aspect ratio, so a ~7:1 diagram stays a thin, letterboxed strip at every zoom
level: content magnifies, but the rendered rectangle never grows into the
vertical letterbox. The preview box is ~16:9 (`previewBoxCols = 355`).

**Fix — zoom-to-fill (snap):**

- **Rest** (full crop): whole image, letterboxed — unchanged.
- **Zoom-in while letterboxed:** snap the crop to the largest box-aspect rectangle
  centered on the current view (full-height, ~25%-width slice for a 7:1 diagram),
  so it fills the box. "Letterboxed" means the crop's pixel aspect doesn't match
  the box — the rest view of a non-square image, *and* a region framed by Tab that
  is wider or taller than the box (Tab keeps the whole region, so a wide step
  group letterboxes). Triggering on fill-state rather than only the full crop is
  what makes Tab-then-zoom fill instead of magnifying the strip. When the crop
  already fills (square-ish image, or an already-snapped crop), skip the snap.
- **Further zoom-in:** shrink the crop **uniformly** about its center (one
  clamped scale factor for both axes — replaces the current independent-axis
  clamp, which distorts). Floor the smaller fraction side at `1/zoomMax`.
- **Zoom-out:** grow uniformly; if either side would reach ≥ 1, snap back to the
  full rest crop. Round-trips `+`/`−` exactly.
- **Pan:** unchanged. Base-fill is non-full, so `!isFull()` keeps `hjkl` panning.

Reuses the box-aspect math from `frameRegion`, extracted into a shared helper so
free-zoom and region-framing agree on what "fill the box" means.

### Accepted consequences

- Tall/portrait images fill *width* by cropping top/bottom on zoom; pan
  vertically to move. Uniform consequence of the fill rule.
- Non-diagram bitmaps below box resolution still letterbox at deep zoom
  (no-upscale ceiling). Diagrams fill sharply because `kickVector` re-rasterizes
  the SVG and `vectorReadyMsg` upscales via `fitToBox`.

## Affected files

- `scripts/tmux-claude-images.sh` — changes 1 & 2.
- `gallery_zoom.go` — `zoomBy` rewrite, `baseFillCrop`, uniform scale helpers.
- `gallery_regions.go` — extract the box-aspect fraction helper from
  `frameRegion`.
