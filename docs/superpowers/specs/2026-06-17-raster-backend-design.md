# Crisp real-pixel rendering on non-kitty terminals (sixel backend)

**Date:** 2026-06-17
**Status:** Approved design
**Issue:** [#60](https://github.com/noamsto/aeye/issues/60) — follow-up to #58 / #59
**Depends on:** PR #59 (`feat/58-ghostty-wezterm`) — provides the `.#verify` devShell
(wezterm) used for manual host verification. #60 is branched off #59; #59 merges
first.

## Goal

Render crisp, real-pixel thumbnails on terminals that support **sixel** but
**not** the kitty unicode-placeholder protocol — today they fall back to chafa
block-art. A new pluggable `backendRaster` emits sixel and paints out-of-band,
mirroring the store/place/clear lifecycle the kitty backend already uses.

| Terminal | Today | With this change |
|----------|-------|------------------|
| kitty / ghostty | crisp (kitty placeholders) | unchanged |
| wezterm | chafa | crisp (**sixel**) |
| any host inside a sixel-capable tmux | chafa | crisp (**sixel**) |
| foot | chafa | crisp (**sixel**, when launchable) |
| iTerm2 | chafa | unchanged (OSC-1337 deferred — see below) |
| Alacritty | chafa | unchanged (no protocol) |

## Scope: sixel only

Issue #60 names two protocols (sixel and OSC-1337). Reading the launcher
(`scripts/tmux-claude-images.sh`) shows **OSC-1337 has no reachable path today**,
so this issue ships **sixel only**. The launcher resolves a host MODE to pick its
split mechanism:

| Signal | MODE | The viewer runs… |
|--------|------|------------------|
| `$TMUX` | tmux | inside tmux |
| `$KITTY_WINDOW_ID` | kitty | standalone in kitty |
| `$WEZTERM_PANE` | wezterm | standalone in wezterm |
| `$TERM=xterm-ghostty*` / `$GHOSTTY_RESOURCES_DIR` | ghostty | standalone in ghostty |
| else | none | not launched |

There is **no iTerm2 or foot standalone launch mode**. So every reachable
non-kitty host is covered by sixel:

- **wezterm standalone** (`MODE=wezterm`) → sixel. Reachable, verifiable.
- **anything inside tmux** (`MODE=tmux`) → sixel iff tmux passes it. Reachable.
- iTerm2 standalone → `MODE=none`, never launched. iTerm2-in-tmux → tmux cannot
  capture OSC-1337 and iTerm2 has no sixel → chafa. So OSC-1337 would be
  **unverifiable dead code** until an iTerm2 *launch* mode exists (launch-layer
  work, like #58/#59). Deferred (see follow-ups).

Both **contexts** are in scope: standalone (the #59 launch modes) and inside tmux
(the dominant aeye context, via tmux's native sixel capture).

## Non-goals

- **OSC-1337 / iTerm2.** Unreachable through the current launcher; deferred to a
  follow-up paired with an iTerm2 launch mode.
- **Sharp d2 vector re-render for raster.** The `vectorReadyMsg`/resvg path
  (`gallery.go:310`) stays kitty-only. chafa rasterizes the cached PNG; resvg
  sharpening on zoom is a focused follow-up.
- **Animated/▶ video, GIF playback.**
- **Alacritty** and any terminal with no sixel — chafa stays their path.

## Architecture

### Why not probe for the kitty path

aeye's kitty backend renders via **unicode placeholders** (U+10EEEE), a capability
*separate* from basic kitty graphics with **no capability query**. A generic
kitty-graphics probe would false-positive on wezterm — it speaks kitty graphics
but not placeholders ([wezterm#986](https://github.com/wezterm/wezterm/issues/986))
— and pick `backendKitty`, rendering nothing. So the `xterm-kitty`/`xterm-ghostty`
termname check is the **correct discriminator**: an allowlist of the two terminals
known to support placeholders, not a shortcut for "graphics." It stays as-is.
(Terminal identity is also resolved upstream by the launcher to choose the split
mechanism, so this detection is consistent with what the system already knows.)

### Backend selection (`chooseGridBackend`, `gallery_render.go:139`)

`gridBackend` gains `backendRaster` alongside `backendKitty` / `backendSymbols`.
With one wire format, no format enum is needed. Selection order:

1. termname `xterm-kitty` / `xterm-ghostty` → **kitty** (unchanged — no probe, so
   the common crisp path keeps its zero-latency startup).
2. **Standalone env fast-path (no probe)** — only when **`$TMUX` is unset**, using
   the same signals the launcher uses so the two never diverge:
   - `$WEZTERM_PANE` set → **raster** (wezterm standalone)
   - `$TERM` prefix `foot` → **raster** (foot, when run directly)
3. **Else — DA1 sixel probe:** sixel attribute present → **raster**; else →
   **symbols** (chafa, unchanged).

The `$TMUX` gate is load-bearing, mirroring the launcher's "tmux first" priority.
`$WEZTERM_PANE` is **not** in tmux's `update-environment`, so a session started
from wezterm leaks a stale `$WEZTERM_PANE` into every tmux pane — an ungated
fast-path would fire inside tmux, skip the probe, and emit sixel into a tmux that
may not pass it. Gating on `$TMUX` routes every in-tmux case to the probe, which
is the *only* reliable signal there: whether *this* tmux passes sixel is a
capability terminal identity cannot answer. So the probe pays its ~150 ms only
where env can't *safely* identify the host. `chooseGridBackend` takes the inputs
it needs beyond `termname` (`$TMUX`, env, probe result) and stays a pure function
of them, so selection is unit-testable without a live terminal.

### Capability probe (`gallery_raster.go`)

`probeSixel() bool`, called from `runGallery` (`gallery.go:500`) **only when steps
1–2 don't already resolve the backend** (so the wezterm-standalone fast-path stays
zero-latency), and always **before** `tea.NewProgram(...).Run()` grabs the tty:

- Put `/dev/tty` in raw mode (`github.com/charmbracelet/x/term`, already a dep —
  `MakeRaw(fd)`/`Restore(fd, state)`), write Primary Device
  Attributes `\x1b[c`, read the `\x1b[?…c` reply with a ~150 ms deadline, drain
  to the `c` terminator, restore mode.
- Sixel-capable iff the attribute list contains `4`. tmux 3.6 answers DA1 with
  its own capabilities (includes `4` when built `--enable-sixel` over a
  sixel-capable outer terminal).
- Timeout / no reply → `false` (chafa). The bounded drain is essential: a late
  reply would otherwise land on bubbletea's stdin and be misparsed as keystrokes
  (the same hazard the kitty `q=2` suppression addresses, `gallery_render.go:158`).

### Geometry — reuse, do not re-derive

The mouse-support work already extracted the carousel's cell geometry as the
single source of truth, shared by `renderView` and hit-testing:

- `previewRect()` (`gallery_mouse.go:21`) → inner image cell area of the preview.
- `filmstripCellRects()` (`gallery_mouse.go:37`) → one rect per visible thumb
  (outer cell, including its 1-cell border each side).

The raster backend becomes a **third consumer** of these helpers, so paint
coordinates provably cannot drift from what `renderView` lays out — no
sentinel-scanning, no replicated layout math. For paint we inset each filmstrip
rect by 1 to the inner `stripW×stripH` image area (the border is drawn by
lipgloss as text and must not be overpainted).

### Paint lifecycle (`gallery_raster.go`)

`paintRaster()` is the raster analog of `transmitView()` (`gallery.go:134`),
called at the same sites (`WindowSizeMsg`, `selectIndex`, `reload`,
`transmitPreviewOnly`, and post-`settle`). For the preview rect and each visible
thumb rect:

1. `\x1b7` save cursor → `\x1b[{row};{col}H` move to the rect's top-left.
2. Emit the image via chafa sized to the cell box:
   `chafa -f sixels --size {w}x{h} <cachedPNG>`, output captured and written to
   `/dev/tty` (the kitty path's out-of-band sink). chafa's non-tty default cell
   geometry is 10×20 px (confirmed: `--size 8x4` → an 80×80 px sixel), exactly
   matching cellPxW:cellPxH (`gallery_cache.go:27`), so no `--font-ratio` tuning
   is needed. Strip chafa's `\x1b[?25l`/`\x1b[?25h` cursor-toggle wrapper, as
   `symbolsBlock` already does (`gallery_render.go:255`).
3. `\x1b8` restore cursor.

**Raw, not tmux-passthrough.** tmux's native sixel capture wants the raw DCS so
it can place the image in its pane grid and clip it; a standalone terminal paints
at the cursor with the identical bytes. So unlike the kitty path, raster does
**not** wrap sequences in `tmuxPassthrough` (`gallery_render.go:148`).

### Holes in `View()` (`renderView`, `gallery.go:371`)

The preview/thumb backend switches (`gallery.go:383` and `gallery.go:422`) grow a
raster arm that emits a **blank** `previewW×previewH` / `stripW×stripH` block of
spaces — a hole the out-of-band image fills. (kitty emits `placeholderBlock`;
symbols emits `symbolsBlock`; raster emits spaces.)

### Coexistence with bubbletea's renderer

bubbletea v2 diffs cells: a line whose text is unchanged frame-to-frame is not
rewritten, so a painted overlay survives. Correctness rests on one invariant —
**whenever a hole's content should change, either its surrounding text changes
(so bubbletea rewrites those cells, erasing the stale overlay) or we repaint:**

- Selection change → new preview/thumbs painted over the same rects (sixel is
  opaque; overwrites). We repaint via the existing `selectIndex` site.
- Visible-thumb count shrinks near the ends → those cells go from bordered-thumb
  to blank, a text change bubbletea rewrites → stale overlay erased. No explicit
  per-image delete needed (raster has no kitty-style `deleteAll`).
- Resize / first frame → `settleMsg` returns `tea.ClearScreen` (`gallery.go:333`)
  which wipes everything; for raster we additionally schedule a paint *after* the
  clear+redraw (a short follow-up tick), since the overlay must land on top of
  the freshly-drawn blank holes.

The carousel is a fixed full-screen alt-screen TUI that never scrolls, which
removes the scroll/reflow tracking the issue flags for the general case.

### Module layout

- **New `gallery_raster.go`** — `probeSixel`, the selection-branch helper,
  `paintRaster`, and the chafa sixel invocation. Mirrors how the kitty primitives
  sit in `gallery_render.go`.
- **`gallery_render.go`** — `chooseGridBackend` grows the raster branch; add the
  blank-hole emitter beside `placeholderBlock` / `symbolsBlock`.
- **`gallery.go`** — `runGallery` runs the probe; `paintRaster` is wired at the
  existing repaint sites; the `settleMsg` arm schedules the post-clear raster
  paint.
- **New `gallery_raster_test.go`** — host-independent unit tests.

## Edge cases

- **tmux without sixel support** → probe returns false → chafa. Honest fallback.
- **Probe timeout** → chafa. Never emit raster bytes to an unconfirmed terminal
  (garbage on screen).
- **Tiny panes** → `previewRect`/`filmstripCellRects` already return empty/clamped
  rects; `paintRaster` skips empty rects.
- **Partial last filmstrip window** → handled by `filmstripCellRects` (`ncells`
  clamp); paint only the returned rects.
- **chafa missing** → raster emission yields nothing; the View still shows the
  blank hole. (Matches `symbolsBlock`'s `[img]` fallback philosophy; chafa is
  already a hard dependency of the symbols path.)

## Testing

**Host-independent unit tests (`gallery_raster_test.go`)** — the real safety net:

- **Selection:** `(termname, $TMUX, env, probeResult)` → expected backend,
  covering the kitty fast-path, standalone env-identified wezterm (`$WEZTERM_PANE`
  with `$TMUX` unset) and foot, the in-tmux → probe route (true/false) **including
  the leaked-`$WEZTERM_PANE`-in-tmux case** (must still probe, not fast-path), and
  the chafa fallthrough.
- **Probe parser:** representative DA1 replies → sixel yes/no (`4` present /
  absent, with and without other attributes, truncated/garbage → no).
- **Paint geometry:** `paintRaster` over a fake tty sink emits cursor-move +
  chafa output at the rects from `previewRect`/`filmstripCellRects` (filmstrip
  inset by 1), for representative pane sizes and a partial window.

`previewRect`/`filmstripCellRects` themselves are already covered by
`gallery_mouse_test.go`.

**Manual host verification (`nix develop .#verify`, from #59):**

- **wezterm, no tmux:** sixel renders crisp; navigation/zoom/pan repaint cleanly;
  no stale tiles when the strip window shifts.
- **wezterm inside tmux:** probe detects sixel; renders crisp and survives
  redraws; with a sixel-less tmux, falls back to chafa.
- **foot** (if available): sixel path.
- Alacritty (chafa unchanged) — sanity check the fallback still holds.

## Out-of-scope follow-ups

- **OSC-1337 + an iTerm2 launch mode** — add an iTerm2 standalone mode to the
  toggle script (launch-layer work), then a small `backendRaster` format branch
  to emit `chafa -f iterm` for it.
- Sharp resvg re-render for raster diagrams on zoom (extend `vectorReadyMsg`
  beyond `backendKitty`).
