# pi adapter

Captures image paths from pi `read` / `write` / `edit` / `bash` tool calls and
D2 diagram renders into the aeye carousel. Ships as a [pi package][pi-pkg]: a
TypeScript extension plus the `image-gallery` / `diagrams` skills.

[pi-pkg]: https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/packages.md

The extension translates pi's lifecycle events into the same normalized hook
payload the Claude/Codex/Cursor adapters feed their scripts, runs the shell
capture scripts under `scripts/`, and injects the SessionStart diagram guidance
and any diagram compile warning back into the conversation.

## Requirements

On PATH:

- `aeye` — the carousel viewer (embeds the d2 compiler; `aeye render-diagram`)
- `tmux-claude-images` — toggle that opens the viewer
- `jq` — the capture scripts parse the hook payload with it
- `resvg` — SVG → PNG raster for diagram renders

## Install

```bash
pi install <path-to-aeye>/adapters/pi
```

This adds the package to your pi settings (`~/.pi/agent/settings.json`), which
loads the extension from `extensions/` and the skills from `skills/`. To try it
without installing, load the extension file directly:

```bash
pi -e <path-to-aeye>/adapters/pi/extensions/aeye.ts
```

(Loading the file directly skips the skills — install the package for those.)

## What it does

| pi event | Action |
|----------|--------|
| `tool_result` (read/write/edit/bash) | Append touched images to the manifest; render `.d2` files and append them (with their SVG as a zoom vector) |
| `session_start` | Inject diagram guidance; clear a foreign manifest and stamp ownership; on `resume`/`fork` rebuild the manifest from the session history |
| `/aeye` command | Open the carousel (runs `tmux-claude-images` with the session id exported) |

Keying matches every other adapter: inside tmux the manifest is
`<tmux server pid>-<pane>`; outside tmux it is the pi session id (exported as
`$AEYE_SESSION_ID`).

## Smoke test

1. **Read a PNG** — after a `read` of any `.png`, the path appears in
   `$AEYE_DIR/images/*.jsonl` (default state dir:
   `${AEYE_DIR:-${CLAUDE_STATUS_DIR:-/tmp/claude-status}}`).
2. **Write a `.d2`** under the scratch dir named in the SessionStart guidance →
   a PNG lands in the carousel (needs `aeye` + `resvg`).
3. Open the carousel: `tmux-claude-images` (or `/aeye` inside pi).

## Limitations

- **tmux-primary** — designed for pi in tmux, where the pane key is shared with
  the toggle. Outside tmux the manifest is keyed by the pi session id; the
  extension exports `$AEYE_SESSION_ID` so the toggle the agent runs finds it,
  but a carousel opened from an unrelated shell needs that variable set.
- **Images via explicit paths + bash output** — a `read`/`write`/`edit` path, or
  an image path echoed in bash output under the project cwd. An image only
  *mentioned* in a command is not captured (matching the Claude adapter).
- **Resume backfill** — rebuilt from pi's session history (`getBranch()`), so it
  recovers images and diagrams but not an image whose only trace was a bash
  output that has since been compacted away.
- **Diagram warnings are appended to the tool result** — pi has no
  `additionalContext` channel, so a D2 compile error or a markdown-blank
  suppression is added as a text block on the tool result the model just saw.
