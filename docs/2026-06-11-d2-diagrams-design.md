# aeye: D2 diagrams in the carousel

**Date:** 2026-06-11
**Status:** Design approved, pending spec review
**Origin:** Brainstormed out of the D2 non-goal in `docs/2026-06-10-design.md` (§Non-Goals 1).

## Summary

Let the agent draw diagrams that appear in the carousel. When a diagram would
clarify an explanation, the agent **`Write`s a `.d2` file** to a scratch dir; a
`PostToolUse` hook — the same surface that already captures images — renders it
**browser-free** (`d2 → svg → resvg → png`) into the per-pane manifest, and the
carousel shows it like any other image.

This reuses the proven `images.sh` machinery (PostToolUse payload, `cwd`-relative
path resolution, manifest keying) instead of parsing the transcript, so there is
no new, unverified hook pattern. The cost is one visible `Write` tool call per
diagram — but the chat prose stays clean (no raw ```` ```d2 ```` block clutters
the reply).

Two behavioral decisions shape the feature:

- **Agent-proactive** — the agent decides when a diagram helps and writes one
  unprompted (not gated behind a user request or a slash command). This requires
  the "when to diagram" guidance to be *ambient* (in context every turn), so it
  is injected by a `SessionStart` hook — but only when a carousel host is
  present (see §Host-gating).
- **Auto-open once per session** — the first diagram of a session opens the
  carousel so the user notices it exists; after that, diagrams render to the
  manifest silently (the carousel refreshes if open, and is never force-opened
  again).

The manifest JSONL stays the stable interface — diagrams are appended as
ordinary `type:"image"` entries, so the viewer needs **zero display changes**.

## Goals

- The agent proactively writes `.d2` files where a picture beats prose
  (architecture, data flow, state machines, pipelines, entity relationships).
- A `PostToolUse` hook renders those files to PNG browser-free and appends them
  to the manifest; the carousel displays them with no viewer-display changes.
- The carousel auto-opens **once per session** on the first diagram, then leaves
  open/closed state under user control.
- The proactive nudge loads only where it can pay off (tmux or kitty-graphics
  host present).

## Non-Goals (explicit, to prevent scope creep)

1. **Codebase scanning / infra-from-code generation.** Tools like
   `heathdutton/claude-d2-diagrams` scan Terraform/K8s/Docker to *generate*
   architecture docs as committed files. Out of scope — this feature only
   renders a diagram the agent is *already drawing* to explain something.
2. **Mermaid for GitHub docs.** GitHub renders Mermaid inline but not D2; D2
   renders browser-free for the terminal but Mermaid's renderer needs Chromium.
   These are different surfaces with a separate authoring guideline ("emit D2 for
   the carousel, Mermaid for GitHub docs") — not part of this skill, and there is
   **no** Mermaid→D2 transpilation step (no reliable converter exists; the agent
   emits the right language per surface).
3. **Animated diagrams.** The carousel rasterizes `svg → png` and displays a
   static frame (the kitty *protocol* supports animation, but the viewer
   transmits PNG via `f=100` and decodes only the first frame). D2 "animated
   traffic flow" is SVG/CSS animation and dies on rasterization.
4. **D2 syntax auto-fix.** On a parse error the hook skips and logs (see §Error
   handling). We borrow the *validate* instinct, not the auto-fix machinery.

## Architecture

### Three components

**(a) Ambient guidance — `SessionStart` hook.**
Injects the "when to diagram" guidance into context every session (via the
hook's stdout / `additionalContext`), gated to hosts where the carousel can
render (see §Host-gating). The full guidance
(~10 lines) lives here so the agent's judgment about *when* to diagram is
reliable, not just a pointer:

- `Write` a `.d2` file when a diagram clarifies: architecture, data flow, state
  machines, pipelines, entity relationships.
- Write it to the **scratch dir the hook injects** (an absolute path under the
  carousel state dir, *outside* any repo — so `git status` stays clean and there
  is no cleanup burden); never into the working project.
- Do **not** diagram trivial/linear/one-step things; one diagram per concept;
  prose stays primary — a diagram supplements, never replaces, the explanation.

Because the agent needs a concrete absolute path to write to, the `SessionStart`
hook resolves the scratch dir (`<state-dir>/images/diagrams/src/`) and embeds it
literally in the injected text — the agent does not have to guess env vars. The
detailed d2 dialect/cheatsheet stays in an on-demand skill body (loaded only when
actually drawing) to keep the ambient footprint small.

**(b) Render hook — `PostToolUse` hook (`adapters/claude-code/plugin/scripts/diagrams.sh`).**
Fires after every tool call; structured like `images.sh`:

1. **Fast-bail:** read `tool_input.file_path` from the payload; exit immediately
   unless it ends in `.d2` and the file exists (resolving relative paths against
   `cwd`, exactly as `images.sh` does). The hook runs on *every* tool call, so the
   non-diagram path stays cheap.
2. Derive the manifest key with the **exact same logic as `images.sh`**:
   `${TMUX_PANE:-${CLAUDE_CODE_SESSION_ID}}`, strip a leading `%`, and apply the
   same `^[A-Za-z0-9_@:.-]+$` traversal guard — otherwise diagrams land in a
   different manifest than the captured images and the carousel splits in two.
3. Content-hash the `.d2` file contents → `<hash>.png`. **Render** only if that
   file is absent (re-writing identical source is a no-op; an edited diagram is a
   new hash). Render browser-free: `d2 <file.d2> <hash>.svg` then
   `resvg <hash>.svg <hash>.png` — never `d2 ... .png` (that shells to Chromium).
4. **Append** `{type:"image", path, source:"d2", ts, mtime}` to the per-pane
   manifest — guarded by a `path`-dedup check (mirroring `images.sh`'s
   `(path,mtime)` guard), *independent* of the render step, so a diagram missing
   from the manifest is re-added even when its PNG is already cached. The viewer
   decodes only `path` + `source` (`gallery_render.go:14`), so `type`/`ts`/`mtime`
   are inert decoration kept for manifest-format parity; `source:"d2"` records
   provenance.
5. If a new diagram rendered **and** this session has not auto-opened yet, call
   the viewer in `--ensure-open` mode and drop the once-per-session marker (see
   §Auto-open).

This hook coexists with `images.sh` under `PostToolUse` (the two fire
independently on the same event — the design already anticipates multiple
PostToolUse hooks). It triggers on any tool whose target is a `.d2` file —
`Write`, `Edit`, or `MultiEdit` — so editing a diagram re-renders it.

**Why not a `Stop` hook?** The earlier design read the transcript on `Stop` and
parsed ```` ```d2 ```` blocks. Nothing in aeye or lazytmux reads
`transcript_path` today, making that an unproven pattern; the `.d2`-file route
reuses the verified `PostToolUse` surface and keeps the chat free of raw d2
source. We accept one visible `Write` tool call per diagram in exchange.

**(c) Viewer change — `--ensure-open` on `scripts/tmux-claude-images.sh`.**
Today the script *toggles* (an existing viewer is killed). Add an
`--ensure-open` flag that opens-if-closed and **no-ops if already open** (skips
the kill branch in `launch_tmux`/`launch_kitty`). Manual toggle behavior is
unchanged; only the hook uses `--ensure-open`.

### Storage: diagrams are primary artifacts, not cache

Rendered diagram PNG files live in **`${AEYE_DIR:-…}/images/diagrams/`** —
durable, alongside the manifest. The `.d2` sources the agent writes sit beside
them in **`images/diagrams/src/`**. Neither is placed in the transcode cache
(`aeye-imgcache`): that cache holds *derived* artifacts regenerable
from a source image, so it is safely evictable. A diagram PNG referenced by the
manifest must persist — cache eviction would leave dangling paths and gaps in the
carousel. (The `.d2` source surviving means a diagram is always re-renderable,
but the manifest points at the PNG, so the PNG is the artifact that must stay.)

### Data flow

```
agent Writes <scratch>/images/diagrams/src/<name>.d2
  → PostToolUse hook (diagrams.sh)
    → file_path ends in .d2 and exists? (bail if not)
    → hash contents → d2 file→svg→png → images/diagrams/<hash>.png
    → append {type:"image", source:"d2", …} to manifest (path-dedup)
    → first diagram this session? → tmux-claude-images --ensure-open + touch marker
  → carousel refreshes → diagram appears
```

## Decisions

### Host-gating (#3 from review)

The `SessionStart` injection is guarded by
`[[ -n $TMUX || -n $KITTY_LISTEN_ON ]]`. Without tmux or a kitty-graphics host
there is nowhere to display a diagram, so the nudge would be pure context waste
and chat clutter. Gating to a present host means the proactive bias appears only
where it can pay off — and a developer who never uses tmux/kitty never sees it.
No opt-out flag for now (YAGNI); add an env kill-switch later if the nudge proves
noisy *even with a host*.

### Auto-open once per session (#4 from review)

The first diagram of a session opens the carousel (so the user discovers it);
subsequent diagrams render to the manifest only (refresh-if-open, never
force-open). This avoids the nag where the hook reopens a carousel the user
deliberately closed. Mechanism: a per-key marker file
`${AEYE_DIR:-…}/images/<key>.opened`. First render with no marker →
`--ensure-open` + touch marker; later renders skip the open. No viewer changes
and no need to distinguish *how* the carousel was closed — which the stricter
"dismiss sentinel" alternative would have required (the Go viewer writing a
marker on `q`-quit). New diagrams still land in the manifest and appear the
moment the user reopens. (Minor: `.opened` markers are not garbage-collected;
they share the cleanup story of the per-key manifests themselves, and a reused
tmux pane id at most suppresses one auto-open — acceptable.)

### Browser-free rasterization; markdown left unsupported (#43)

The raster step is `d2 → svg → resvg → png`, never `d2 … .png`. d2 generates the
SVG in pure Go (LaTeX included — MathJax runs in a Go JS engine and is baked in
as `<path>` glyphs), but d2's *own* PNG export shells out to headless Chromium.
resvg (pure Rust) does the pixel step instead, keeping the pipeline hermetic and
cron/SSH-safe — the same reason D2 was chosen over Mermaid (whose renderer needs
Chromium) in the first place.

The one cost: resvg is a static SVG rasterizer and does not implement
`<foreignObject>`. d2 emits `|md` / `|markdown` block bodies as live HTML inside
a `<foreignObject>` (it defers their layout to a browser), so those nodes paint
blank while `d2` still exits 0 — a silent failure that looks like a missing
entity. `|code` (native `<text>`) and `|latex`/`|tex` (`<path>`) are pre-rendered
to SVG primitives at generation time and rasterize fine; only markdown is left as
deferred HTML.

**Rejected: a browser rasterizer to render markdown.** Headless Chromium /
Playwright / `d2 … .png` would paint `<foreignObject>` correctly, but adopting
one reverts the founding browser-free constraint for *every* diagram: ~200 MB of
Chromium + font/GPU libs, slower renders, and flakiness in headless/cron runs.
The payoff is small — markdown adds nothing over plain quoted labels +
`style.bold/italic/font-size` + `|latex`/`|code`, except mixed inline formatting
in one node, which is an anti-pattern for ≤12-node sketches. So markdown stays
unsupported: the SKILL and SessionStart guidance steer to plain labels, and after
a successful render the hook checks the generated SVG for `<foreignObject>` (the
exact element resvg drops) and surfaces a warning (`additionalContext` +
`render-errors.log`) instead of failing silently. Checking the rendered SVG
rather than grepping the source is exact — it catches every markdown syntax,
never false-positives on the literal text `|md` inside a label, and only fires
when a render actually happened (renderers absent stays a silent no-op). If rich
text is ever needed, the cheaper path is a targeted hybrid — the detection
already exists, so only those diagrams would opt into a browser raster behind an
env flag (e.g. `AEYE_D2_BROWSER=1`), leaving the default hermetic.

## Error handling (define errors out of existence)

- **`d2` or `resvg` not on PATH** → hook no-ops silently, matching the viewer's
  "not installed → do nothing" convention.
- **Malformed d2** (`d2` exits non-zero) → skip that file, append the error to
  `images/diagrams/render-errors.log` (one line: timestamp + hash + stderr),
  **no manifest entry**. Never inject a broken image.
- **No tmux/kitty host** → `--ensure-open` no-ops, same as the toggle today.

## Known trade-offs (accepted)

- **A `Write` tool call is visible per diagram.** The agent writes a `.d2` file
  rather than printing the source inline, so the reply shows a `Write` tool-use
  (collapsed file diff) instead of a raw d2 block. Chat prose stays clean; the
  tradeoff is a tool call where a chat code block would otherwise be.
- **Render latency.** The diagram appears a beat after the `Write`, once the
  `PostToolUse` hook runs and the carousel refreshes.
- **Two new dependencies.** `d2` and `resvg` join `jq`/`tmux`/`kitty`. The
  viewer decodes raster only (no SVG path in `gallery_cache.go`), so `resvg` is
  mandatory for the browser-free pipeline. Both must be packaged in the flake;
  absent → graceful no-op.

## Testing

`bats` tests mirroring `tests/adapter.bats`, with fixture `PostToolUse` payloads
(like `tests/fixtures/hook-write-image.json`):

- `.d2` Write → asserts manifest entry + rendered PNG under `images/diagrams/`;
- duplicate Write (same contents) → asserts no second entry;
- malformed d2 → asserts skip + `render-errors.log` line, no manifest entry;
- `d2` absent on PATH → asserts clean no-op;
- fast-bail: non-`.d2` `file_path` → asserts no render / no entry;
- `Edit` of an existing `.d2` → asserts re-render (new hash, new entry);
- `--ensure-open` against an already-open pane → asserts it does **not** kill it;
- once-per-session: second diagram with marker present → asserts no second open.

## Files

| Path | Change |
|------|--------|
| `adapters/claude-code/plugin/scripts/diagrams.sh` | **new** — `PostToolUse` hook: on a `.d2` `Write`/`Edit`, render `d2→svg→png`, append manifest, once-per-session ensure-open. |
| `adapters/claude-code/plugin/scripts/diagram-guidance.sh` | **new** — `SessionStart` hook: host-gated ambient nudge with the resolved scratch dir embedded. |
| `adapters/claude-code/plugin/hooks/hooks.json` | add the second `PostToolUse` entry + a `SessionStart` registration. |
| `adapters/claude-code/plugin/skills/diagrams/SKILL.md` | **new** — on-demand d2 dialect/cheatsheet (sibling of `image-gallery`). |
| `scripts/tmux-claude-images.sh` | add `--ensure-open` (open-if-closed; no kill). |
| `flake.nix` | package `d2` + `resvg` into the plugin/devShell runtime. |
| `tests/diagrams.bats` | **new** — render/dedup/error/once-per-session tests. |
