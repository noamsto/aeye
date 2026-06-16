# Backfill image/diagram manifest on resume (#44)

## Problem

Image/diagram capture is live-only. The `images.sh`/`diagrams.sh` PostToolUse
hooks append to a per-pane manifest (`images/<pane>.jsonl`) as each tool call
happens. Nothing reconstructs that manifest after the fact, so a resumed session
(`claude --resume`) can bring up an empty carousel even though the transcript
records every image the prior session touched:

- **Resume in a different pane/terminal** — the manifest is keyed by `$TMUX_PANE`
  (falling back to `$CLAUDE_CODE_SESSION_ID`). A new pane gets a fresh, empty
  manifest; prior captures are stranded under the old pane key.
- **Manifest gone/never existed** — a cleared manifest or a clean pane leaves the
  whole back-catalog invisible until the agent re-touches an image.

`session-reset.sh` keeps the manifest on `resume|compact`, but "keep" is a no-op:
it does not *rebuild* a manifest that is not there.

## Approach

A new SessionStart hook, `session-backfill.sh`, fires on `source=resume` and
replays the session transcript to reconstruct the per-pane manifest from the
images and diagrams the prior session touched. Path-extraction (and `.d2`
rendering) is factored into a shared lib so the live hooks and the backfill use
one source of truth and cannot drift.

Scope is **resume only** for v1. `compact` keeps the manifest intact, so backfill
there is low value and is intentionally out of scope.

```
SessionStart(resume) ─▶ session-backfill.sh
                          ├─ gate on source=resume; key pane/manifest (as session-reset.sh)
                          ├─ read transcript_path from payload
                          ├─ grep candidate lines (image ext or .d2) from transcript
                          ├─ per candidate line ─▶ lib/manifest-extract.sh
                          │                          ├─ extract_image_path(payload)
                          │                          ├─ extract_d2_path(payload)
                          │                          └─ render_d2(src)
                          ├─ append (dedup vs manifest ∪ this-run; ts = line timestamp)
                          └─ write .owner = $CLAUDE_CODE_SESSION_ID

PostToolUse ─▶ images.sh   ─┐
PostToolUse ─▶ diagrams.sh ─┴─▶ source lib/manifest-extract.sh (behavior unchanged)
```

## Components

### `scripts/lib/manifest-extract.sh` (new)

Pure helpers — no manifest writes, no toggle, no keying. Each echoes a result or
nothing and returns 0 (must not trip `set -e` in callers).

- `extract_image_path "$payload"` — the two-phase logic currently inline in
  `images.sh`: Phase 1 tries `tool_input.file_path|path|output_path`; Phase 2
  scans `tool_response` strings for an embedded path. Applies the
  extension + on-disk existence checks and `cwd`-relative resolution. Echoes a
  resolved existing image path or nothing.
- `extract_d2_path "$payload"` — the `.d2` candidate resolution from
  `diagrams.sh` (`tool_input.file_path`, `cwd`-resolve, `.d2` + existence check).
  Echoes a resolved existing `.d2` path or nothing.
- `render_d2 "$src"` — the render block from `diagrams.sh` (d2 → svg →
  d2-fix-fonts → svg-contrast → resvg → png), cached by source hash, skip when the
  png already exists. Renderers absent → silent no-op. Echoes the png path (and
  its svg sidecar path) or nothing on failure. Does **not** append or toggle.

### `images.sh` / `diagrams.sh` (modified)

Source the lib and replace the now-extracted inline blocks with calls. Their
preamble and side effects are unchanged: keying, the `.owner` self-heal,
append, the markdown `<foreignObject>` warning, and (`diagrams.sh` only) the
`--ensure-open` toggle all stay put. The markdown warning stays in `diagrams.sh`
because it is live-hook UX, not part of rendering.

### `session-backfill.sh` (new SessionStart hook)

1. Read payload; gate on `source=resume` (else `exit 0`).
2. Key the pane/manifest with the same logic and path-traversal guard as
   `session-reset.sh`.
3. Read `transcript_path`; missing/empty/unreadable → `exit 0`.
4. `grep -nE '\.(png|jpe?g|gif|webp|bmp|d2)'` the transcript to get candidate
   lines only (most lines — Bash, etc. — never reach `jq`).
5. For each candidate line, build a synthetic hook payload and run it through the
   extractors (see Data flow). Append image entries and rendered-diagram entries,
   dedup-guarded, with `ts` taken from the transcript line.
6. Write `.owner = $CLAUDE_CODE_SESSION_ID` so the live hooks recognise the
   rebuilt manifest as owned by this session and do not self-heal it away.

### `hooks/hooks.json` (modified)

Register `session-backfill.sh` as a second SessionStart hook entry alongside
`diagram-guidance.sh` and `session-reset.sh`.

## Data flow: the replay

Each transcript line becomes a **synthetic hook payload**, so no tool_use ↔
tool_result join is needed — each line independently yields at most one path:

- An **assistant** line's `tool_use` block → `{tool_name, tool_input: .input, cwd}`.
  Catches `Read`/`Write` images and `.d2` writes via Phase 1.
- A **user** line's `tool_result` → `{tool_response: <content>, cwd}`.
  Catches screenshot paths via Phase 2.

`cwd` and `timestamp` are read from the same transcript line (both present on
assistant and user lines). For each extracted path:

- **Image**: append `{type:"image", path, source, ts, mtime}` with `ts` = the
  line's timestamp.
- **`.d2`**: `render_d2` first; on success append
  `{type:"image", path:<png>, vector:<svg>, source:"d2", ts, mtime}`.

Every append is dedup-guarded against `(existing manifest ∪ paths already
appended this run)`, keying images by source path and diagrams by png path, and
is skip-if-missing — so a kept-manifest resume never doubles up and the first
occurrence's timestamp wins.

## Error handling / edge cases

- No `transcript_path`, unreadable, or empty → clean `exit 0` (matches the
  no-op discipline of the sibling hooks).
- `source != resume` → `exit 0`. `session-reset.sh` still owns `startup|clear`.
- Path-traversal guard on the pane key, identical to the sibling hooks.
- Renderers (`d2`/`resvg`) absent → `render_d2` no-op; image entries still
  backfill.
- Lib functions echo-or-empty and `return 0` so `path=$(extract_image_path …)`
  under `set -euo pipefail` never aborts the caller.
- Repeated resumes are idempotent via the dedup guard.

## Known limitation

A screenshot path lives in the *persisted* tool_result. MCP image tools that
persist a base64 image block (rather than a path string) will not backfill;
`Read`/`Write`/`.d2` calls (path in `tool_input`) always will. This is inherent
to reconstructing from the transcript and is accepted for v1 rather than worked
around.

## Testing

- `tests/manifest-extract.bats` (new) — unit-test the lib functions against
  payload fixtures (reuse existing `hook-*.json` fixtures).
- `tests/session-backfill.bats` (new) — feed a small fixture transcript
  (`tests/fixtures/transcript-*.jsonl`); assert: rebuilt manifest has the right
  entries, `ts` ordering follows the transcript, dedup against a pre-seeded
  manifest, `.owner` is written, and resume-only gating (non-resume → no-op).
- `tests/adapter.bats` (covers `images.sh`) / `tests/diagrams.bats` /
  `tests/d2-render-real.bats` must stay green after the lib extraction — proof of
  zero behavior change.
