# Cursor adapter

Captures image paths from Cursor Agent `Read` / `Write` / `Shell` tool calls
and D2 diagram renders into the aeye carousel. On `sessionStart`, injects
scratch-dir guidance via `additional_context` and resets the per-pane
manifest.

Primary runtime: Cursor CLI (`cursor-agent`) inside tmux. Hooks live in
user/project `hooks.json` — not a Cursor plugin.

## Requirements

On PATH:

- `aeye` — the carousel viewer
- `tmux-claude-images` — toggle that opens the viewer
- `resvg` — D2 → PNG raster (also needs `d2` for diagrams)

## Install

From the repo (idempotent):

```bash
adapters/cursor/install.sh
```

What it does:

- Merges aeye hook entries into `~/.cursor/hooks.json` (replaces any prior
  aeye entries; leaves other hooks alone)
- Symlinks skills into `~/.cursor/skills/aeye-diagrams` and
  `~/.cursor/skills/aeye-image-gallery`

Override the Cursor config home with `AEYE_CURSOR_HOME` (default
`$HOME/.cursor`).

Requires `jq` on PATH.

## Smoke test

1. **Read a PNG** — after a session `Read`s any `.png`, the path appears in
   `$AEYE_DIR/images/*.jsonl` (default state dir:
   `${AEYE_DIR:-${CLAUDE_STATUS_DIR:-/tmp/claude-status}}`).
2. **Write a `.d2`** under the scratch dir named in the SessionStart
   guidance → a PNG lands in the carousel (needs `d2` + `resvg`).
3. Open the carousel: `tmux-claude-images`.

## Limitations

From the [hook-contract spike](../../docs/superpowers/spikes/2026-07-28-cursor-hook-contract.md):

- **tmux-primary** — designed for `cursor-agent` in tmux.
- **No browser/MCP screenshot capture** — only Read/Write/Shell path
  extraction.
- **Resume backfill deferred** — `sessionStart` is new-conversation-only;
  `transcript_path` is null there.
- **Failed tool calls not captured** — `postToolUseFailure` is not hooked
  (v1 matches Claude/Codex: `postToolUse` only).
- **Cursor IDE plugin hooks unproven** — CLI `hooks.json` is the supported
  path; IDE plugin-bundled hooks remain an unproven follow-up.
