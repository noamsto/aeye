# agent-carousel: rich, legible D2 diagrams

## Summary

The D2-diagrams feature (see `2026-06-11-d2-diagrams-design.md`) renders `.d2`
files into the carousel. Dogfooding it surfaced one shipping bug and several
quality gaps that, together, mean the diagrams are neither *worth looking at*
nor *worth drawing* yet:

1. **No text renders.** `d2` embeds fonts as `@font-face`; `resvg` does not
   support `@font-face`, so every label is dropped — diagrams ship as boxes
   with no words.
2. **The authoring skill is shallow.** `skills/diagrams/SKILL.md` teaches only
   boxes/arrows/containers — no ERDs, sequence diagrams, classes, theming,
   icons, flow-direction, or aesthetic guidance. Output is plain.
3. **The viewer under-serves diagrams.** Small diagrams render tiny (no
   upscale), there is no zoom/pan, and duplicate manifest entries show twice.

Goal: **diagrams worth drawing AND worth looking at.** Delivered in three
phases, each with its own implementation plan.

## Goals

- Text (incl. bold/italic) renders reliably, browser-free.
- The diagram theme follows the user's light/dark mode at render time.
- The authoring skill produces rich, beautiful, legible diagrams by default.
- The viewer treats diagrams as first-class (legible at rest, crisp when
  zoomed).

## Non-Goals

- Reintroducing the headless-browser dependency the original design avoided
  (`d2`'s native PNG path). The pipeline stays `d2 → svg → resvg → png`.
- Premium TALA layout engine (not bundled).
- Per-viewer adaptive output (a PNG is a fixed render; mode is chosen at render
  time, not at display time).

## Phase 0 — Fix the render pipeline (live bug, highest priority)

Without text, nothing downstream matters. Scope is `diagrams.sh`, the nix font
packaging (bundle + `AGENT_CAROUSEL_D2_FONT_DIR`), and a regression test.

### Text rendering

`resvg` skips `@font-face` and then drops text whose `font-family` it cannot
resolve (`d2-<n>-font-bold`, etc.). Two parts: give resvg fonts to use, and
point the SVG's text at them.

**Bundle the fonts — don't depend on host fontconfig.** Relying on whatever sans
the host has is fragile: DejaVu Sans here ships no bold/italic faces, and Noto
may be absent elsewhere. Bundle the exact fonts d2 itself embeds — `d2 --help`
documents its defaults as **Source Sans 3** (regular/bold/italic/semibold) +
**Source Code Pro** (mono). Both are pinnable nixpkgs packages (`source-sans`,
`source-code-pro`), referenced by store path — **no font blobs vendored into
git**. The nix wrapper exports the font dir; the hook renders hermetically:

```sh
resvg --skip-system-fonts --use-fonts-dir "$AGENT_CAROUSEL_D2_FONT_DIR" ...
```

**Remap the synthetic names** so the text points at the bundled family:

```sh
sed -E 's/d2-[0-9]+-font-[a-z]+/Source Sans 3/g' "$svg" > "$svg.fixed"
```

Verified live: a hermetic render (system fonts disabled, only the bundled dir)
drops resvg's `No match for font-family` warnings from 15 to **0** — all text
resolves, identically on every host, in d2's intended typeface. The regex is
anchored to the `-font-<style>` suffix, so it rewrites only the 6 font
references and leaves the ~116 other `d2-<id>` class references untouched.

Non-nix consumers (the plugin can run outside nix): when
`AGENT_CAROUSEL_D2_FONT_DIR` is unset, fall back to remapping to a system family
(`AGENT_CAROUSEL_D2_FONT`, default `Noto Sans`) with system fonts enabled.

### Bold / italic fidelity

d2 distinguishes weight by font *file*, not `font-weight`, so the blanket remap
renders all labels at regular weight unless we add a weight/style signal.
Bundling fixes the *availability* half — the Source Sans 3 bundle ships real
`Bold` (weight 700) and `Italic` faces (verified via `fc-scan`), so the faces
exist for resvg to pick.

*Selecting* them is the fiddly half: the family name is quoted in the SVG
(`font-family: "d2-<n>-font-bold"`), so injecting `font-weight` into the name
breaks it (verified: 14 dropped labels). Real fidelity needs rewriting d2's CSS
*rules* (`.text-bold`, `.text-italic`) to add `font-weight`/`font-style` as
separate declarations — and the exact selector must match d2's emitted CSS (a
naive `.text-bold{` substitution did **not** match in testing). Treated as a
render-tested implementation detail in the Phase 0 plan. If it proves fiddly,
regular-weight-only is an acceptable Phase 0 ship (text is legible); bold/italic
is the polish pass.

### Theme by light/dark mode

A PNG is a fixed render, so the theme is chosen when the hook renders:

| Mode | Default theme-id |
|---|---|
| dark | `200` Dark Mauve |
| light | `105` Buttered Toast |

Overridable via `AGENT_CAROUSEL_D2_THEME` / `AGENT_CAROUSEL_D2_DARK_THEME`.
Mode is read from a configurable signal (env first; no fragile terminal
auto-detection — define the error out of existence). `--sketch` is on by
default, overridable.

### Testing

`tests/diagrams.bats` gains a regression test that renders a labelled diagram
and asserts **resvg emits zero `No match for font-family` warnings** (capture
stderr). This is deterministic — it directly proves every label's font
resolved — and far more robust than a PNG byte-size or pixel heuristic. So a
silent regression to textless boxes can never ship again.

## Phase 1 — Rich, beautiful authoring skill

Rewrite `skills/diagrams/SKILL.md` as a single rich file (~250–350 lines /
~2.5–3k tokens — normal for a domain-reference skill; loaded only when the
agent is actually drawing). Structure:

1. Frontmatter — sharpen the trigger description.
2. When to draw / when not — keep the existing judgment, compressed.
3. Where to write — keep (scratch dir, never in-repo), compressed.
4. **House style (beautiful-by-default)** — copy-paste header:
   `vars: { d2-config: { sketch: true; pad: 16 } }` (theme is set by the
   hook, not the file), plus a `classes:` block with semantic roles
   (`svc` cool-blue, `store` green cylinder, `ext` amber/dashed). Evidence-
   backed: the palette appears in the official style docs and a peer d2 skill.
5. Core syntax — shapes, `->`/`<->`/`--`, containers, labels.
6. **Flow direction** — default `direction: right` (horizontal) to fill the
   carousel's landscape preview; use vertical (`down`) only for inherently-tall
   structures (sequence diagrams, deep trees); prefer grouping over long thin
   chains. Ties directly to the viewer's aspect-fit.
7. **Rich constructs** — groupings/nested containers, `sql_table` ERDs
   (`constraint: primary_key|foreign_key|unique`, `table.col -> table.col`,
   `layout-engine: elk` for row-precise edges), `shape: sequence_diagram`,
   `classes`/`class`, `icon:`, `near` legends, styling vocab (fill, stroke,
   stroke-dash, border-radius, shadow, font-color).
8. **3–4 worked examples** — architecture+grouping+icons, ERD, sequence/state,
   pipeline. **Every example render-tested** in bats so the doc can never ship
   syntax that doesn't compile (we hit the `,` vs `;` map-separator bug live).
9. Aesthetic do/don't — ≤~12 nodes, 2–3 intentional colors, one shape per role,
   label every edge, let layout breathe.
10. Requirements — `d2` + `resvg` on PATH.

## Phase 2 — Carousel display upgrades

The base zoom/pan work is already specced in `docs/plans/2026-06-10-zoom-pan.md`
(bitmap crop-magnify of the decoded source; "never upscale past source
resolution"). Land that plan first. The items below are **diagram-specific
extensions** that build on it — and two of them *revisit* its decisions, so
they must be reconciled with that plan, not assumed compatible:

- **Zoom + panning** — ship the existing zoom-pan plan as-is (bitmap crop).
- **Crisp vector zoom for diagrams (extension, revisits "bitmap-only")** — for
  `source:"d2"` entries, re-render from the vector source at the zoom level
  instead of magnifying pixels. Diagrams are the densest images and benefit
  most; requires keeping the `.d2`/SVG source reachable from the manifest.
- **Upscale small diagrams (extension, revisits "never upscale")** — at rest,
  let a small diagram fill the preview box (currently `scale >= 1` returns the
  source unchanged → tiny diagrams). Scope to `source:"d2"` to avoid blurring
  upscaled screenshots.
- **Maximize / fill preview** — a key to hide chrome and use the whole pane.
- **Viewer-side dedup** — `loadManifest` shows every decodable entry; dedup by
  path so a twice-referenced image appears once.
- **Nav wraparound** — optional; `l` at last → first.

## Known trade-off: role palette vs dark mode

The semantic `classes` use explicit fills (light blue/green/amber). Explicit
fills override theme colors, so the same classes on a dark theme would put
light boxes on a dark background. Phase 1 must resolve this — options: (a) ship
mode-paired palettes (light + dark fill sets), (b) pick role colors that read
on both backgrounds, or (c) let dark mode drop explicit fills and rely on the
theme. Picked in the Phase 1 plan; flagged here so it isn't discovered late.

## Error handling (define errors out of existence)

- Renderers absent → hook no-ops silently (unchanged).
- Font unavailable → mostly *defined out of existence*: the nix build pins the
  bundled font dir, so it is always present on nix hosts. Off-nix, fall back to
  a system family (`AGENT_CAROUSEL_D2_FONT`, default `Noto Sans`).
- Unknown/garbled mode signal → fall back to light defaults; never crash.

## Files

| File | Change |
|---|---|
| `adapters/claude-code/plugin/scripts/diagrams.sh` | Phase 0: font remap + hermetic `--use-fonts-dir`, bold/italic CSS rewrite, theme-by-mode + env overrides, sketch default |
| `flake.nix` / nix wrapper | Phase 0: add `source-sans` + `source-code-pro` deps; export `AGENT_CAROUSEL_D2_FONT_DIR` to the hook |
| `tests/diagrams.bats` | Phase 0: zero-`No match` regression test; Phase 1: render-test each skill example |
| `adapters/claude-code/plugin/skills/diagrams/SKILL.md` | Phase 1: full rewrite (rich + beautiful + flow direction) |
| `gallery.go`, `gallery_render.go`, `gallery_cache.go` | Phase 2: zoom/pan, maximize, upscale, dedup (extends zoom-pan plan; coordinate with the in-flight `feat/carousel-staleness-window-fixes` work) |

## Phasing & ordering

Phase 0 first (unblocks everything; it's a live bug). Phase 1 next (the
authoring win). Phase 2 last (display polish, coordinates with the in-flight
zoom-pan work). Each phase → its own implementation plan via writing-plans.
