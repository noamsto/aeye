# Rich constructs and worked examples

Open when you need syntax beyond the core (containers, ERDs, sequence diagrams,
classes, icons, styling) or a complete diagram to lift and adapt. Every diagram
here is a complete, single-board D2 source.

**Grouping** — wrap a subsystem in a container to give the layout structure and
a labeled boundary; edges can cross in and out. A container's name renders as a
visible label on the boundary, so name it meaningfully (`backend`, not `g1`).
Containers also become drill-in **regions** in the carousel — Tab cycles them
and `]` `[` drill in and out — so when the diagram has real subsystems, group
them into containers and the reader gets navigation for free. Only group what's
genuinely a subsystem, though: don't wrap a flat three-node flow in a container
it doesn't need just to manufacture regions.

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
title: Checkout service { near: top-center; style: { stroke-width: 0; fill: transparent } }
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
