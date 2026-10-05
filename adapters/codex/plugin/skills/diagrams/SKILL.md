---
name: diagrams
description: Use when a picture beats prose — drawing architecture, data flow, state machines, pipelines, or entity relationships as a D2 diagram that renders into the aeye viewer. Covers where to write the file, house style, and core syntax; deeper references load on demand.
---

# Diagrams (D2 → carousel)

When structure is clearer seen than read, write a [D2](https://d2lang.com)
diagram as a `.d2` file. A `PostToolUse` hook renders it browser-free
(`aeye render-diagram` → svg → resvg → png) into the per-pane image manifest,
and the carousel shows it like any other image — auto-opening once per session.

Every `d2` block here and in the references is a complete, single-board source —
copy one and edit it.

## When to draw (and when not to)

- **Do:** architecture, data flow, state machines, pipelines, entity
  relationships — anything with branching or feedback that a sentence flattens.
- **Don't:** trivial or linear one-step things; a list that's already a list;
  restating prose. One diagram per concept. Prose stays primary — the diagram
  supplements the explanation, it doesn't replace it.

## Where to write it

Write the `.d2` file to the scratch dir named in the SessionStart guidance —
an absolute path under the carousel state dir
(`<state-dir>/images/diagrams/src/<name>.d2`), **outside any repo**. These are
throwaway diagram sources, not project artifacts; never write `.d2` files into
the working project.

`<name>` is the caption the carousel shows under the diagram, so name the file
for the concept it draws, in kebab-case — `auth-token-flow`, `render-pipeline`,
`origins-erd` — not `diagram` or `untitled`. The name is also what retires the
previous render: the hook prunes any earlier image filed under the same name, so
to revise a diagram, overwrite the same file and it replaces the old one in the
carousel instead of stacking beside it.

That pruning only fires if the write lands under the right name, so write **one
`.d2` per tool call, named by absolute path**. The hook reads the paths an
`apply_patch` call adds or updates (`*** Add File:` / `*** Update File:`),
resolved against the *project* cwd (write the absolute path even though `apply_patch` prefers relative ones: a path containing `..` renders but is never adopted); every `.d2` the patch names renders. A `.d2`
written by a shell command (heredoc, `sed -i`) is not picked up — use
`apply_patch`.

Only a `.d2` under the scratch dir's `src/` is adopted into the carousel. A
`.d2` anywhere else still renders but files nothing, and a `<name>-check.d2` /
`<name>-test.d2` basename is never adopted even there — so verify a render by
copying the source to a `-check.d2` in the scratch dir. A `/tmp/a.d2` round-trip
renders but files nothing.

## House style

The hook applies the sketch look, the mode-appropriate theme, and a set of
**semantic role classes** automatically — you do **not** put `sketch`, `theme`,
or those classes in the file. Tag a shape or edge with a role and aeye colors it
for the rendered theme — a soft pastel fill on light, a bright accent border +
title on dark (no heavy blocks either way):

| `class:` | meaning |
|----------|---------|
| `warn`   | error / danger / the broken (before) case |
| `good`   | success / the fixed (after) case |
| `accent` | emphasis / focus |
| `info`   | neutral highlight |

```d2
before: BEFORE { class: warn }
after: AFTER { class: good }
before -> after: fix { class: good }
```

For your own *structural* distinctions (service vs store vs external), define
extra classes and tell them apart by **stroke + shape**, not fill, so they read
on both themes:

```text
classes: {
  svc:   { style: { stroke: "#1565C0"; stroke-width: 2 } }
  store: { shape: cylinder; style: { stroke: "#2E7D32"; stroke-width: 2 } }
  ext:   { style: { stroke: "#E65100"; stroke-width: 2; stroke-dash: 3 } }
}
```

A `classes` block alone draws nothing — it's shown as `text` here on purpose;
drop it into a diagram that has shapes. Carry per-node distinctions on the
`stroke`, not a hand-set `fill` (a raw fill is baked for one theme).

## Flow direction

Pick the axis from the diagram's structure, not from the preview's shape: **the
chain runs along the long axis, the branching across the wide one.** The carousel
has zoom, pan, and region drill-in, so a diagram that needs panning is fine — one
that's illegible at any zoom is not. Never cut content or squash a layout to make
something fit the frame.

`direction: right` is the right default for a short chain. Reach for
`direction: down` when the branching is the widest thing on the board, or when the
chain's edges carry labels: an edge label consumes space *along* the flow axis
(layered engines lay labels out as dummy nodes, so each one occupies a layer). A
labeled five-stage chain that renders as a 5:1 strip left-to-right comes out
1:1.6 top-to-bottom, same content. Sequence diagrams and deep trees stay `down`.

An extreme ratio in either axis is a symptom, not the bug — nodes carrying prose
is the usual cause. Fix the content per the next section; the proportions follow.

## What goes in a node

- **Nodes are the things; edges carry what happens between them.** Follow the
  convention of the kind of diagram you're drawing: in state machines and ERDs the
  nodes are nouns and the edge carries the verb, event, or guard — a three-way
  verdict is a condition on a transition, not three stages, so it belongs on the
  arrows. Data flow diagrams invert this (processes are verb phrases, the flows
  are the nouns); don't carry the rule across that line.
- **~3 short lines per node.** A node's width is set by its longest label line
  before layout runs, so paragraph labels multiply the board's width and then no
  direction rescues it. If a box needs more room, it's prose, or it's two nodes.
- **No conclusions, no recommendations.** "SO THE LOW END IS UNREACHABLE" is the
  argument; "THREE POSSIBLE FIXES" is the remediation. Both belong in the message
  text — the diagram shows the mechanism the reader reasons *from*.
- **One abstraction level per canvas.** Mechanism, observed numbers, conclusion,
  and fix list are four diagrams' worth of material, not one.

Moving the verbs onto the edges is the highest-leverage edit available — it usually
deletes nodes and crossings at once, and
[crossings damage comprehension more than any other layout property](https://link.springer.com/chapter/10.1007/3-540-63938-1_67).

## Core syntax

Shapes are bare identifiers; a label after `:` overrides the name. Connect them
with arrows, nest them with `{ }`:

- `a -> b` directed, `a <-> b` bidirectional, `a -- b` undirected.
- Label any edge: `a -> b: enqueue`. Label every edge that isn't obvious.
- Containers nest with `name: { ... }`; reach a child as `parent.child`.
- Inline map keys separate with `;` or newlines — **never commas**
  (`shape: cylinder; style.stroke: red`, not `shape: cylinder, ...`).
- Pick a shape with `shape:` — `cylinder`, `person`, `queue`, `cloud`,
  `sql_table`, `sequence_diagram`, and more.
- Escape `$` in label text as `\$` — **one** backslash. A bare `$` starts a
  variable substitution (`${var}`), and `\\$` escapes the backslash instead,
  leaving the `$` live; either way a literal `"$5,000,000"` fails to compile with
  *"substitutions must begin on {"* and nothing renders. Scan labels for `$`
  before writing the file.
- Write labels as **plain quoted strings** (use `\n` for line breaks); do **not**
  use `|md` / `|markdown` block bodies — **`title:` included**, the most common
  slip. D2 emits markdown as an HTML `<foreignObject>`, which the carousel's
  static rasterizer (resvg) cannot paint. The render hook detects this and
  **suppresses the whole diagram** until you rewrite the block as a plain quoted
  label — a `|md` title means no diagram, not just a blank title. (`|code` and `|latex`/`|tex` are fine: they compile to native SVG.)

A complete minimal diagram — request path through a small service:

```d2
direction: right
client: {shape: person}
api: API
db: {shape: cylinder}
client -> api: request
api -> db: query
api -> client: response
```

## Check the render

No pane at all, rather than a blank one, means the file never compiled: the hook
reports the D2 error back to you, and also logs it to
`<state-dir>/images/diagrams/render-errors.log`.

Look at the rendered PNG once before you rely on the diagram — the renders land
beside the source, at `<state-dir>/images/diagrams/<hash>-{light,dark}.png`. A
size measurement catches none of the failures that actually happen: alternatives
that read as a pipeline, an edge crossing a node's label, an unconnected node
parked in a corner where it reads as a mistake.

## References

Each sits in `references/` beside this file; open it when its trigger fires.

- `references/rich-constructs-and-examples.md` — containers, ERDs, sequence
  diagrams, classes, icons, styling vocab, and worked architecture / ERD / state
  machine / pipeline examples to lift. Open before drawing anything past a flat
  flow.
- `references/style-and-layout.md` — why the house style is what it is, plus
  layout do/don'ts (node budget, ELK vs dagre, `grid-*`, container direction).
  Open when a render looks wrong or the board is past ~12 nodes.
- `references/github-embedding.md` — shipping a diagram in a README, PR body, or
  issue as a committed light/dark SVG pair. Open when a diagram must outlive the
  carousel.

## Requirements

Rendering needs `aeye` (it embeds the d2 compiler) and `resvg` on PATH. If either
is missing the hook no-ops silently (no diagram, no error) — install both to
enable diagrams. Theme choice and the contrast pass run inside `aeye`.
