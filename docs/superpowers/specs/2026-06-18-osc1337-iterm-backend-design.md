# OSC 1337 raster rendering (iTerm2 / wezterm) — design

Issue: [#60](https://github.com/noamsto/aeye/issues/60) — follow-up. The sixel half
landed in `8fc9d61` (PR #66); this is the remaining OSC 1337 half.

## Problem

aeye renders crisp images on:

- **kitty / ghostty** via the kitty graphics protocol with unicode placeholders (`backendKitty`).
- **wezterm / foot / any sixel-capable terminal** via chafa sixel (`backendRaster`).

Everywhere else it falls back to chafa block-art (`backendSymbols`). The one
graphics-capable terminal still stuck on block-art is **iTerm2**: it speaks neither
kitty placeholders nor sixel — only the iTerm2 inline-image protocol (OSC 1337).

Separately, **wezterm** currently gets sixel, which is palette-indexed (chafa
quantizes to ≤256 colors and dithers). wezterm also speaks OSC 1337, which ships the
real PNG bytes for full 24-bit color — strictly higher fidelity. So wezterm should
prefer OSC 1337 too.

## Goal

Add an OSC 1337 rendering path so:

- **iTerm2** renders crisp instead of block-art.
- **wezterm** renders via OSC 1337 (true-color) instead of sixel.

foot (sixel-only) and the in-tmux DA1-probe path keep sixel. kitty/ghostty unchanged.
Alacritty (no graphics protocol) stays on block-art — out of scope.

## Key realization: OSC 1337 reuses the entire raster lifecycle

`chafa -f iterm` emits OSC 1337 with **cell-based** sizing, structurally identical to
the sixel payload — a single blob painted at cursor coordinates:

```
\e]1337;File=inline=1;width=<cols>;height=<rows>;preserveAspectRatio=0:<base64>\a
```

(wrapped in the same `\e[?25l` / `\e[?25h` cursor toggles `renderSixel` already strips).

So OSC 1337 and sixel share the **whole** out-of-band lifecycle already built for
`backendRaster`: blank-hole placeholders in `View()` (`blankBlock`), the debounced
`schedulePaint` → `rasterPaintMsg` repaint, `paintSixelAt` cursor positioning,
`tea.ClearScreen` teardown, and the absence of any kitty-style persistent-store clear.
The **only** difference is the chafa `-f` flag: `sixels` vs `iterm`.

## Approach (chosen: A — reuse `backendRaster` + a format field)

Keep the single `backendRaster` enum. Add a `rasterFormat` ("sixels" | "iterm") that
the renderer reads. Every existing `m.backend == backendRaster` guard keeps working
untouched; only the chafa invocation changes.

Rejected — **B, a new `backendITerm` enum**: would duplicate every raster guard and
the full Update/View lifecycle plumbing for a one-flag difference. More surface, more
places to forget a case. A pulls the variation down into one field.

## Design

### Selection — `chooseGridBackend`

Today (`gallery_render.go:187`):

```go
func chooseGridBackend(termname string, inTmux bool, weztermPane, term string, probeSixel func() bool) gridBackend
```

Change it to also return the raster format and to accept the two env signals that
identify iTerm2 / wezterm:

```go
func chooseGridBackend(termname string, inTmux bool, termProgram, lcTerminal, weztermPane, term string, probeSixel func() bool) (gridBackend, string)
```

`rasterFormat` is `""` for non-raster results. The two raster values are named
constants, not bare strings, so a typo can't silently pick the wrong chafa format:

```go
const (
	formatSixel = "sixels"
	formatITerm = "iterm"
)
```

**Param footgun**: this pushes `chooseGridBackend` to six positional string-ish args,
easy to transpose at the single call site. The plan keeps them positional (matching the
existing 4-arg style) but the call site is the only caller and `TestChooseGridBackend`
covers each signal independently, so a transposition fails a test. If the call site
reads awkwardly, group the env inputs into a small `termEnv` struct — but that's a
refinement, not required.

New branch order (kitty allowlist stays first, unchanged):

| Condition | backend | format |
|---|---|---|
| termname prefix `xterm-kitty` / `xterm-ghostty` | kitty | "" |
| not in tmux & (`TERM_PROGRAM=iTerm.app` or `LC_TERMINAL=iTerm2`) | raster | iterm |
| not in tmux & (`TERM_PROGRAM=WezTerm` or `WEZTERM_PANE` set) | raster | iterm |
| not in tmux & `TERM` prefix `foot` | raster | sixels |
| `probeSixel()` positive (incl. in-tmux) | raster | sixels |
| else | symbols | "" |

**Why `!inTmux` gates the OSC 1337 branches**: inside tmux, `TERM_PROGRAM` becomes
`tmux` and `WEZTERM_PANE` isn't forwarded, so iTerm2/wezterm can't be reliably
identified — exactly why the existing wezterm-sixel branch is already `!inTmux`. In
tmux, behavior is unchanged: the DA1 probe reflects tmux's own sixel capability and
selects sixel. This deliberately avoids the OSC-1337-through-tmux-passthrough problem,
which is out of scope.

This flips standalone wezterm from sixel → OSC 1337 (the intended fidelity upgrade).

Call site (`gallery.go:564`) passes the new env vars and destructures both returns;
the model gains a `rasterFormat string` field alongside `backend` (`gallery.go:86`).

### Render

`renderSixel` (`gallery_raster.go:111`) generalizes to take the format. Rename to
`renderRaster(format, pngPath string, cols, rows int) string`; body is identical
except `-f <format>`. Its two callers `paintPreview` / `paintStrip`
(`gallery_raster.go:146,156`) pass `m.rasterFormat`. The doc comment updates to note
both formats fill the cell box (chafa sizes both by `--size` in cells).

To make the format selection testable without executing chafa, factor the command
construction into `rasterArgs(format, pngPath string, cols, rows int) []string`
returning `["-f", format, "--size", "<cols>x<rows>", pngPath]` — mirroring the
existing `symbolsArgs` seam (`gallery_render.go:296`). `renderRaster` calls
`exec.Command("chafa", rasterArgs(...)...)`.

**No per-image format fallback**: if `chafa -f iterm` fails, `renderRaster` returns ""
and `paintSixelAt` no-ops (blank hole), with no fall-back to sixel. Acceptable —
chafa supports both formats uniformly, so an iterm failure implies chafa is broken for
sixel too. Same failure mode the sixel path already has.

`paintSixelAt`, `schedulePaint`, `rasterPaintMsg`, `paintRaster`, and the `View()`
blank-hole path need **no** change — they're already format-agnostic.

### Teardown

Unchanged. `deleteAll()` is kitty-only; OSC 1337 leaves no persistent store, so the
existing `tea.ClearScreen` raster teardown covers it (same as sixel).

## Testing

- **`TestChooseGridBackend`** (`gallery_test.go:44`) — update for the new signature
  and add cases:
  - `TERM_PROGRAM=iTerm.app` (not in tmux) → (raster, iterm)
  - `LC_TERMINAL=iTerm2` (not in tmux) → (raster, iterm)
  - `TERM_PROGRAM=WezTerm` (not in tmux) → (raster, iterm)
  - `WEZTERM_PANE` set (not in tmux) → (raster, iterm)
  - `TERM=foot` (not in tmux) → (raster, sixels)
  - iTerm2/wezterm env **but in tmux** → falls to probe (sixels when probe positive,
    symbols when negative)
  - kitty/ghostty allowlist unchanged
- **`TestRasterArgs`** — assert `rasterArgs(formatITerm, "/a/b.png", 20, 10)` ==
  `["-f", "iterm", "--size", "20x10", "/a/b.png"]` and the sixel equivalent. Pure, no
  chafa execution — mirrors the existing `TestSymbolsArgs` (`gallery_test.go:103`).
  This is the format-selection guarantee; no test executes chafa today.
- **Optional render smoke test** — run `renderRaster(formatITerm, png, …)` on a
  generated temp PNG (precedent: `gallery_cache_test.go:74`) and assert the payload
  starts with `\e]1337;File=` and has the cursor wrappers stripped; skip when chafa is
  absent via `exec.LookPath("chafa")` (precedent: the d2 skip at
  `gallery_regions_test.go:82`). Nice-to-have, not load-bearing — `TestRasterArgs`
  already pins the format.
- **Live verification** — wezterm via the `.#verify` devShell (added in #59): confirm
  OSC 1337 renders crisp and that navigation/scroll/clear still behave. iTerm2 itself
  is macOS-only and not verified locally; we trust the OSC 1337 format spec + chafa.

## Out of scope

- OSC 1337 through tmux passthrough (in-tmux iTerm2/wezterm keep sixel-or-symbols).
- Animated/video/GIF playback.
- Alacritty and other no-graphics terminals (chafa block-art stays their path).
- Live iTerm2 verification (no macOS host).
