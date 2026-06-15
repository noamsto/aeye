# Auto-contrast diagram label text against node fill

Issue: [#22](https://github.com/noamsto/aeye/issues/22)

## Problem

d2 colors every label with the theme's foreground color, never derived from a
node's `fill`. Under the carousel's dark theme (`AEYE_D2_THEME` resolves to a
dark theme such as `200`), label text is light (`#CDD6F4`). When a diagram sets
an explicit light `fill` — e.g. `discovery-conflict-space.d2` using `#dcfce7`,
`#ffedd5`, `#dbeafe` — the light text lands on a light fill and washes out. The
inverse (dark fill under a light theme) fails the same way.

The house style already says "role by stroke + shape, not fill," but the
renderer does not protect a diagram that sets a fill anyway. We fix this at the
renderer (so legibility is guaranteed) and reinforce it in guidance (so
authored diagrams stay clean at the source).

## Pipeline today

`diagrams.sh` runs, per `.d2` write:

```
d2 -t <theme> --sketch <src>.d2 <hash>.svg
d2-fix-fonts.sh <hash>.svg          # sed: remap @font-face family, inject bold/italic
resvg [...] <hash>.svg <hash>.png
append manifest line; --ensure-open
```

The new pass slots between `d2-fix-fonts.sh` and `resvg`.

## SVG structure we rely on

A node is a group whose class is `<base64-id> <user-class>`, holding a
`<g class="shape">` (the shape `<path>`/`<rect>` carries `fill="…"`) and one or
more `<text … class="… fill-N1">` labels:

```xml
<g class="d2hhdC52YWx1ZQ== covered">
  <g class="shape">
    <path … stroke="#16a34a" fill="#dcfce7" class="shape" style="stroke-width:2;"/>
    <path … class="sketch-overlay-B6"/>
  </g>
  <text x="…" y="…" fill="#CDD6F4" class="text-bold fill-N1" …>value</text>
</g>
```

Distinctions that matter:

- **Node/container labels** — `<text>` siblings of a `<g class="shape">` inside a
  node group. These must contrast the shape's `fill`. Containers have the same
  shape+label shape, so they are handled identically.
- **Edge labels** — `<text class="connection …">`, children of connection
  groups. They sit on the canvas, not on a fill. Out of scope: left untouched.
- **Markdown blocks** (title/legend) — rendered as `foreignObject` HTML with
  their own `--color-fg-*` CSS vars. Out of scope: not the reported problem and
  a different color model.

## Design

### `aeye svg-contrast <file>` (new subcommand)

A subcommand of the existing Go binary. It rewrites the SVG with a
**byte-preserving targeted edit** — not a full `encoding/xml` decode/encode,
which round-trips lossily (attribute reordering, self-closing normalization,
and the `<style>` CSS block and `foreignObject` HTML are not clean XML). We
only rewrite specific `fill` attributes and leave every other byte intact:

1. Scan element opening tags in document order (a lightweight tag scanner over
   the raw bytes). Maintain `currentShapeFill`.
2. On a shape element (`class` contains `shape`) with a literal `fill="#rrggbb"`,
   set `currentShapeFill`. Skip `none`/absent/`url(...)` fills (gradient/pattern
   — leave it; the theme default already contrasts the theme's own fill). This
   relies on d2's emission order: within a node group the shape precedes its
   label, and connection groups carry no `class="shape"` fill, so edges never
   pollute `currentShapeFill`.
3. On a `<text>` opening tag whose `class` does **not** contain `connection`,
   rewrite its `fill` to the contrasting ink for `currentShapeFill`:
   - relative luminance `L = 0.2126·R + 0.7152·G + 0.0722·B` on
     sRGB channels in `[0,1]` (linearization omitted — the simple weighted form
     is enough for a black/white decision and keeps the code small).
   - `L > 0.5` → dark ink `#13111C`; else light ink `#F5F5F5` (soft endpoints,
     not pure black/white, to stay in the sketch register).
   - If `currentShapeFill` is unset (a label before any shape fill — e.g. a
     bare label), leave the `<text>` untouched.
4. Write the result back atomically (`<file>.tmp` → rename), mirroring
   `d2-fix-fonts.sh`.

Idempotent: re-running recomputes from the shape fill (which we never touch),
so a second pass yields the same ink.

Exit non-zero only on unreadable/unparseable input; `diagrams.sh` treats any
failure as "skip the pass" (see wiring).

CLI shape: `aeye svg-contrast <file>` — the first arg selects the mode. The
existing `aeye <key>` viewer path is unchanged: `svg-contrast` is recognized as
`os.Args[1]`, everything else falls through to `runGallery`.

### `diagrams.sh` wiring

After the font fix, before resvg, run the pass through the same binary the
viewer uses, and degrade gracefully — a missing/old binary or a failed pass
just means we render without it (consistent with how a missing `d2`/`resvg`
no-ops):

```bash
contrast_bin="$(command -v "${AEYE_BIN:-aeye}" 2>/dev/null || true)"
if [[ -n $contrast_bin ]]; then
  "$contrast_bin" svg-contrast "$svg" 2>>"$err" || true
fi
```

(Placement and error-logging match the existing steps; final form lands in the
plan.)

### Guidance update

- `skills/diagrams/SKILL.md`: keep "role by stroke + shape, not fill"; add a
  line that the renderer auto-contrasts label text against an author-set fill,
  so authors don't hand-set `font-color`.
- `diagram-guidance.sh`: no behavioral change needed; only touch if the wording
  references contrast.

## Testing

**Go unit tests** (`svg_contrast_test.go`), table-driven over small SVG inputs:

- light fill (`#dcfce7`) node label → dark ink `#13111C`
- dark fill (`#1e1e2e`) node label → light ink `#F5F5F5`
- edge label (`class="connection"`) → unchanged
- `fill="none"` / gradient `url(...)` → unchanged
- idempotent: second pass equals first

**bats** (`tests/svg-contrast.bats`, mirroring `d2-fix-fonts.bats`): run the
built binary against a fixture SVG and assert the recolored fills, plus a
no-op when the input has no shape fills.

## Out of scope

- Markdown title/legend recoloring.
- Edge-label-vs-background contrast (edges already use the theme fg on the
  theme bg, which contrasts by construction).
- Stripping author fills / enforcing stroke-only (rejected: discards
  intentional emphasis; auto-contrast is the more faithful fix).

## Trade-offs

- Preserving fills + recoloring text (chosen) over stripping fills: more code,
  but keeps intent and fixes both polarity mismatches.
- Go subcommand over awk in `d2-fix-fonts.sh`: the node-vs-edge-vs-markdown
  distinction needs real XML walking; Go is testable in the repo's idiom and
  the binary already ships. Cost: `diagrams.sh` now optionally depends on the
  `aeye` binary (degrades gracefully when absent).
