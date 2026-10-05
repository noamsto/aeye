# Style rationale and layout pitfalls

Open when styling beyond the role classes, when a render looks wrong (crossing
edges, a thin strip, an arrow through a label), or before drawing a board past a
dozen nodes.

## Why the house style is what it is

- **Sketch + theme come from the hook** — the hand-drawn stroke reads as
  "explanatory sketch," not a rigid spec. If you ever render a file by hand,
  add `vars: { d2-config: { sketch: true; pad: 16 } }` to match; under the
  carousel it's redundant.
- **Color through roles; in your own classes, stroke not fill.** The semantic
  roles (SKILL.md, House style) are aeye-managed — it picks a theme-appropriate treatment, so
  `class: warn` is safe whatever theme renders. For a class you define yourself,
  tell it apart by a colored *stroke* + distinct *shape*: those read on both
  themes because the label keeps the theme's own contrasting text color, whereas a
  raw `fill` you set by hand is baked for one theme. Reserve hand-set `fill` for a
  genuine one-off — **carry a per-node distinction on the `stroke`, not the fill**
  (a colored border keeps the theme's own label + fill, so contrast never depends
  on the fill's luminance). The render's contrast pass re-inks a filled node's
  label — named colors included — so a stray `fill` stays readable, but that's a
  backstop, not a license: it needs the `aeye` binary on PATH and flattens the
  label to one ink, where a stroke keeps the theme's text palette intact.
- **A `classes` block alone draws nothing** — SKILL.md shows it as `text` on
  purpose. Drop it into a diagram that has shapes, as the worked examples do.

## Aesthetic do / don't

- **Keep it to ~12 nodes.** Past that, split into separate diagrams.
- **Distinguish roles by stroke + shape, not a rainbow of fills** — see the
  house palette in SKILL.md.
- **Label every non-obvious edge.** An unlabeled arrow asks the reader to guess.
- **Don't echo the target's name in an edge label.** An arrow into a container
  titled "X lifecycle" needs no `"lifecycle"` label — the duplicate text stacks
  on the container's own title right where the edge enters. Label an edge only
  with what the target's name doesn't already say.
- **One concept per diagram.** Don't merge the architecture and the data model.
- **Direction follows the structure** — see Flow direction in SKILL.md.
- **Containers are for meaning, not for layout.** Group a genuine subsystem (it
  becomes a carousel drill-in region too). Grouping will *not* fold a long chain:
  under ELK a nested container's `direction:` is ignored — children follow the
  root direction. Measured on a 9-node board, adding `direction: down` to each
  container moved the width from 2897px to 2841px.
- **Don't reshape a flow with `grid-rows` / `grid-columns`.** Under ELK they place
  badly. `grid-columns: 3` over three mutually exclusive outcomes laid them in a
  row with arrows running between them — the alternatives read as a three-stage
  pipeline, and one edge crossed a node's label. `grid-rows: 2` put a store behind
  the start node, doubling its edge back across another branch.
- **An arrow cutting through a node's label?** Switch to ELK —
  `vars: { d2-config: { layout-engine: elk } }`. The default engine (dagre)
  draws straight lines that can cross a box; ELK routes orthogonally from node
  borders, so edges attach instead of slicing through text. A good default for
  any branchy flow with a node that several edges converge on, not just ERDs.
