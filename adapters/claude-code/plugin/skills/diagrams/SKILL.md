---
name: diagrams
description: Use when a picture beats prose — drawing architecture, data flow, state machines, pipelines, or entity relationships as a D2 diagram that renders into the aeye viewer. Produces beautiful, legible diagrams (sketch style, role-by-stroke palette) and covers the full authoring vocabulary plus where to write the file.
---

# Diagrams (D2 → carousel)

When structure is clearer seen than read, write a [D2](https://d2lang.com)
diagram as a `.d2` file. A `PostToolUse` hook renders it browser-free
(`d2 → svg → resvg → png`) into the per-pane image manifest, and the carousel
shows it like any other image — auto-opening once per session.

This skill is the authoring reference: house style, the syntax you'll reach
for, and worked examples you can lift verbatim. Every diagram below is a
complete, single-board D2 source — copy one and edit it.

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

## House style

The carousel hook applies the sketch look and the mode-appropriate theme to
every render automatically — you do **not** put `sketch` or `theme` in the
file. What you provide is `direction` and a role `classes` palette. Open each
diagram with this block, then add your shapes:

```text
# Role classes distinguish by stroke + shape (NOT fill) so they read on BOTH
# the light and dark theme the carousel may render.
classes: {
  svc:   { style: { stroke: "#1565C0"; stroke-width: 2 } }
  store: { shape: cylinder; style: { stroke: "#2E7D32"; stroke-width: 2 } }
  ext:   { style: { stroke: "#E65100"; stroke-width: 2; stroke-dash: 3 } }
}
```

Why these choices:

- **Sketch + theme come from the hook** — the hand-drawn stroke reads as
  "explanatory sketch," not a rigid spec. If you ever render a file by hand,
  add `vars: { d2-config: { sketch: true; pad: 16 } }` to match; under the
  carousel it's redundant.
- **Role by stroke + shape, not fill** — the carousel may render light or dark,
  so a fill-coded legend can vanish against the background. A colored *stroke*
  and a distinct *shape* survive both. Reserve `fill` for the rare emphasis.
- **A `classes` block alone draws nothing** — it's shown as `text` here on
  purpose. Drop it into a diagram that has shapes, as the worked examples do.

## Flow direction

Default to `direction: right`. The carousel preview is landscape, so a
left-to-right flow fills the frame; a tall stack wastes it. Use
`direction: down` only for things that are inherently vertical — sequence
diagrams and deep trees. When a chain gets long and thin, **group** related
nodes into a container instead of stringing one more box on the end.

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
- Escape `$` in label text as `\$` — a bare `$` starts a variable substitution
  (`${var}`), so a literal `"$5,000,000"` fails to compile with
  *"substitutions must begin on {"* and the diagram silently never renders.

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

## Rich constructs

**Grouping** — wrap a subsystem in a container to give the layout structure and
a labeled boundary; edges can cross in and out. A container's name renders as a
visible label on the boundary, so name it meaningfully (`backend`, not `g1`):

```d2
direction: right
gateway: API Gateway
backend: Backend {
  auth: Auth
  orders: Orders
  auth -> orders: token
}
gateway -> backend.auth: request
```

**ERDs** with `shape: sql_table` — fields take a type, and `constraint` renders
as a PK/FK/UNQ badge. Add `layout-engine: elk` for cleaner, orthogonal edge
routing on multi-table ERDs:

```d2
vars: { d2-config: { layout-engine: elk } }
users: {
  shape: sql_table
  id: int { constraint: primary_key }
  email: varchar { constraint: unique }
}
posts: {
  shape: sql_table
  id: int { constraint: primary_key }
  user_id: int { constraint: foreign_key }
}
posts.user_id -> users.id
```

**Sequence diagrams** with `shape: sequence_diagram` — children become
lifelines, ordered by first appearance, and `direction: down` keeps time
flowing top-to-bottom:

```d2
direction: down
flow: {
  shape: sequence_diagram
  client; api; db
  client -> api: POST /order
  api -> db: insert
  db -> api: ok
  api -> client: 201 Created
}
```

**Classes** factor shared styling. Define once in `classes:`, apply with
`class:` — this is the house palette in action:

```d2
direction: right
classes: {
  svc:   { style: { stroke: "#1565C0"; stroke-width: 2 } }
  store: { shape: cylinder; style: { stroke: "#2E7D32"; stroke-width: 2 } }
}
api: API { class: svc }
db: Postgres { class: store }
api -> db: query
```

**Icons** load an image into a shape — but the URL is **fetched at compile
time**, so keep icons out of diagrams this skill renders. The syntax, for
reference: `server: { icon: https://icons.terrastruct.com/infra/019-network.svg }`.

**Styling vocab** — reach for these inside `style: { ... }`: `stroke`,
`stroke-width`, `stroke-dash` (dashed = external/optional), `border-radius`,
`shadow: true`, `font-color`. Use `fill` sparingly, for genuine emphasis only.

**Captions and legends** — pin a free node near a shape or corner with `near`,
e.g. `near: top-center` for a title or `near: bottom-right` for a key.

## Worked examples

Each is a complete, single-board diagram. Lift one and adapt it.

**Architecture** — grouping plus the role palette; stroke + shape carry meaning:

```d2
direction: right
title: |md # Checkout service | { near: top-center }
classes: {
  svc:   { style: { stroke: "#1565C0"; stroke-width: 2 } }
  store: { shape: cylinder; style: { stroke: "#2E7D32"; stroke-width: 2 } }
  ext:   { style: { stroke: "#E65100"; stroke-width: 2; stroke-dash: 3 } }
}
web: Web App { class: svc }
core: Checkout {
  api: API { class: svc }
  worker: Worker { class: svc }
  api -> worker: enqueue job
}
db: Orders DB { class: store }
pay: Stripe { class: ext }
web -> core.api: place order
core.api -> db: write order
core.worker -> pay: charge card
```

**Entity relationships** — three tables, FK edges, ELK for clean routing:

```d2
vars: { d2-config: { layout-engine: elk } }
direction: right
users: {
  shape: sql_table
  id: int { constraint: primary_key }
  email: varchar { constraint: unique }
}
orders: {
  shape: sql_table
  id: int { constraint: primary_key }
  user_id: int { constraint: foreign_key }
  total: decimal
}
items: {
  shape: sql_table
  id: int { constraint: primary_key }
  order_id: int { constraint: foreign_key }
}
orders.user_id -> users.id
items.order_id -> orders.id
```

**State machine** — labeled transitions, self-loop for retry:

```d2
direction: right
queued -> running: dequeue
running -> done: success
running -> failed: error
failed -> queued: retry
running -> running: heartbeat
```

**Pipeline** — a stage chain with a branch; grouped so it stays wide, not thin:

```d2
direction: right
classes: {
  store: { shape: cylinder; style: { stroke: "#2E7D32"; stroke-width: 2 } }
}
ingest: Ingest
transform: Transform {
  clean: Clean
  enrich: Enrich
  clean -> enrich
}
warehouse: Warehouse { class: store }
alerts: Alerts
ingest -> transform.clean: raw events
transform.enrich -> warehouse: load
transform.enrich -> alerts: anomalies
```

## Aesthetic do / don't

- **Keep it to ~12 nodes.** Past that, split into separate diagrams.
- **Distinguish roles by stroke + shape, not a rainbow of fills** — see the
  house palette.
- **Label every non-obvious edge.** An unlabeled arrow asks the reader to guess.
- **One concept per diagram.** Don't merge the architecture and the data model.
- **`direction: right` unless it's inherently tall** (sequence, deep tree).
- **Let layout breathe** — prefer grouping over one long thin chain.

## Requirements

Rendering needs `d2` and `resvg` on PATH. If either is missing the hook no-ops
silently (no diagram, no error) — install both to enable diagrams.
