# Manifest contract

The viewer reads a per-key manifest at
`${AGENT_CAROUSEL_DIR:-${CLAUDE_STATUS_DIR:-/tmp/claude-status}}/images/<key>.jsonl`.

- `<key>` is a tmux pane id with the leading `%` stripped, **or** a coding-agent
  session id (e.g. `$CLAUDE_CODE_SESSION_ID`) — whichever the capture adapter
  used to launch the viewer.
- One JSON object per line:

  ```json
  {"type":"image","path":"/abs/path.png","source":"Read","ts":"2026-06-10T12:00:00+0000","mtime":1717977600}
  {"type":"image","path":"/abs/diagram.png","vector":"/abs/diagram.svg","source":"d2","ts":"2026-06-10T12:00:00+0000","mtime":1717977600}
  ```

  - `path` — absolute path to an image file (png/jpg/jpeg/gif/webp/bmp).
  - `source` — free-form producer tag (the agent's tool name, e.g. `Read`/`Write`).
  - `ts` — ISO-8601 capture time.
  - `mtime` — source file mtime (epoch seconds), used for dedup.
  - `vector` — optional absolute path to the entry's vector source (an SVG, set
    by the d2 adapter). When present, the viewer re-renders this source at zoom
    for crisp magnification; absent for raster captures.

- Consumers MUST tolerate duplicate `(path, mtime)` lines (concurrent adapter
  firings).
- Any producer that appends valid lines shows up in the viewer. New agents are
  added as new adapters under `adapters/<agent>/`; the viewer never changes.
