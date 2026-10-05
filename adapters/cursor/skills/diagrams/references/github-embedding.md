# Embedding a diagram in GitHub

Open when a diagram must outlive the carousel — a README, PR body, or issue.

A diagram can outlive the carousel — a README, a PR body, or an issue can carry
the same render. GitHub has no `d2` fence the way it has one for mermaid, and its
markdown sanitizer drops inline `<svg>` markup, so the diagram travels as a
**committed SVG file referenced as an image**.

**Ship both themes.** GitHub renders on a light or a dark page, and a render
baked for the wrong one arrives as dark ink on dark. Every carousel render
already produced the pair — the hook renders each source twice, leaving
`<hash>-light.svg` and `<hash>-dark.svg` beside the PNGs. To put a pair at a path
you choose, render it twice yourself; each call writes the `.svg` beside the
`.png` it names, so point them at the scratch dir and copy the SVGs out:

```bash
AEYE_D2_THEME=105 aeye render-diagram flow.d2 <scratch>/flow-light.png
AEYE_D2_THEME=200 aeye render-diagram flow.d2 <scratch>/flow-dark.png
```

Land them as `docs/assets/<name>.svg` (light) and `docs/assets/<name>-dark.svg`,
and **commit the `.d2` beside them** so the next person redraws from source
instead of reverse-engineering a picture. That committed source is the one `.d2`
that belongs inside a project; drafting still happens in the scratch dir.

**Reference the pair with `<picture>`**, light as the `<img>` fallback:

```html
<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/flow-dark.svg">
  <img alt="Ingest cleans and enriches events into the warehouse; anomalies branch to alerts" src="docs/assets/flow.svg" width="700">
</picture>
```

- **`alt` carries the diagram's content**, not its existence — it's what a screen
  reader reads and what a failed load leaves behind. "Architecture diagram" says
  nothing; name the mechanism, as the example does.
- **`width` is how you constrain it.** GitHub draws an SVG at its intrinsic size,
  so a wide board overflows the column. ~700px suits a README.
- **A relative path only resolves inside a committed file** (README, `docs/*.md`).
  An issue or PR body has no file to resolve against, so link the raw URL there,
  pinned to a commit rather than a branch, or it drifts:
  `https://raw.githubusercontent.com/<owner>/<repo>/<sha>/docs/assets/flow.svg`.
- **The SVG is self-contained** — the font is embedded as a data URI, so it draws
  the same for every reader with nothing fetched at view time. That's also why
  each file runs ~100–150 KB, and a themed pair is twice that: commit the
  diagrams a reader needs, not every draft.
- **`AEYE_D2_SKETCH=0` drops the hand-drawn stroke** for straight lines. Worth it
  where the diagram is reference documentation someone returns to, rather than an
  explanation passing through a conversation.

Open the page on GitHub in both color modes before you rely on it. The failure
this pattern exists to prevent is invisible from the files alone.
