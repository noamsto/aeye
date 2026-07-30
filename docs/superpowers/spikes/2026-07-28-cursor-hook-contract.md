# Spike: Cursor plugin hook runtime contract (aeye #157)

Date: 2026-07-28 (scaffold), 2026-07-30 (contract proven)
Version tested: `cursor-agent` / Cursor `2026.07.23-e383d2b`

## GATE: PASS for hooks.json install on the CLI runtime; FAIL for plugin-bundled hooks on the CLI

Cursor Agent (the primary runtime per spec — CLI inside tmux) **fires
user/project `hooks.json` hooks with full payloads**, including working
`additional_context` injection on both `sessionStart` and `postToolUse`.

**Plugin-bundled `hooks/hooks.json` never fired on the CLI** — not in `-p`
print mode, not in interactive mode in tmux, with the plugin passed via
`--plugin-dir` or discovered through `~/.cursor/plugins/local/` (relative
`./scripts/…` and `${CURSOR_PLUGIN_ROOT}` command forms both tried; probe log
stayed 0 bytes across all runs while agent tool calls completed). No cached
marketplace plugin under `~/.cursor/plugins/cache/` ships a `hooks.json`
either. Plugin hooks remain an IDE-only possibility (unproven — needs a human
Reload Window + IDE Agent exercise).

**Consequence:** distribution for the CLI runtime must be a `hooks.json`
install (user `~/.cursor/hooks.json` or project `.cursor/hooks.json`), not a
Cursor plugin. The spec's locked "Marketplace-ready plugin" decision does not
hold for the primary runtime; see issue #157 for the resolution.

## Proven contract (CLI, verbatim payloads)

### sessionStart (user hook, fired in `-p` print mode)

```json
{
  "conversation_id": "afafb0bf-9b2e-4e2a-8857-88e522b364e6",
  "generation_id": "afafb0bf-9b2e-4e2a-8857-88e522b364e6",
  "model": "cursor-grok-4.5-high",
  "is_background_agent": false,
  "session_id": "afafb0bf-9b2e-4e2a-8857-88e522b364e6",
  "hook_event_name": "sessionStart",
  "cursor_version": "2026.07.23-e383d2b",
  "workspace_roots": ["/tmp/aeye-spike-work"],
  "user_email": "noam@factify.com",
  "transcript_path": null
}
```

- No `cwd` key on sessionStart; `workspace_roots` array instead.
- No Claude-like `source` field (no startup/resume distinction).
- `transcript_path: null` at sessionStart.
- `{additional_context: "…"}` output **is injected into model context**
  (probe marker confirmed seen by the model).

### postToolUse (fired for Shell, Write, Read in `-p` print mode)

Shell:

```json
{
  "conversation_id": "48a6813a-…",
  "generation_id": "48a6813a-…",
  "model": "grok-4.5",
  "tool_name": "Shell",
  "tool_input": { "command": "ls sample.png", "cwd": "", "timeout": 30000 },
  "tool_output": "{\"output\":\"sample.png\\n\",\"exitCode\":0}",
  "duration": 79.471,
  "tool_use_id": "3215f49c-…",
  "cwd": "",
  "session_id": "48a6813a-…",
  "hook_event_name": "postToolUse",
  "cursor_version": "2026.07.23-e383d2b",
  "workspace_roots": ["/tmp/aeye-spike-work"],
  "user_email": "noam@factify.com",
  "transcript_path": "/home/noams/.cursor/projects/tmp-aeye-spike-work/agent-transcripts/<id>/<id>.jsonl"
}
```

Write:

```json
{
  "tool_name": "Write",
  "tool_input": { "file_path": "/tmp/aeye-spike-work/diagram2.d2", "content": "a -> b\n" },
  "tool_output": "{\"file_path\":\"/tmp/aeye-spike-work/diagram2.d2\",\"success\":true}"
}
```

Read of a real PNG:

```json
{
  "tool_name": "Read",
  "tool_input": { "file_path": "/tmp/aeye-spike-work/real.png" },
  "tool_output": "{\"file_path\":\"/tmp/aeye-spike-work/real.png\",\"content_length\":0}"
}
```

Field notes for the adapter (Phase 1+ must honor these):

- `conversation_id` == `session_id` == `generation_id` (same UUID) in CLI
  sessions. Prefer `conversation_id // session_id`.
- `cwd` is present but an **empty string** in CLI runs — resolve relative
  paths against `workspace_roots[0]` instead.
- Read/Write path field is **`tool_input.file_path`** (absolute in practice).
- **`tool_output` is a JSON-encoded string**, not an object. Shell output
  lives at `.tool_output | fromjson | .output`; a plain `.. | strings` walk
  sees only the raw envelope string. `scan_response_image_path` must
  `fromjson?` first.
- `transcript_path` is **set** on postToolUse payloads (per-conversation
  `.jsonl` under `~/.cursor/projects/<slug>/agent-transcripts/`) even though
  it is `null` at sessionStart → resume backfill can key off the transcript
  dir, or read the path from the first postToolUse.
- `additional_context` output on postToolUse **is injected** (marker seen by
  model; the pre-3.9.8 context-drop bug from the Cursor forum is fixed in
  this build).
- Failed tool calls route to `postToolUseFailure`, not `postToolUse` (a Read
  of a degenerate 1×1 png failed with a tool stream error and produced no
  postToolUse entry). Matcher list for capture should consider both events;
  v1 hooks `postToolUse` only, matching Claude/Codex parity.
- `tool_use_id` can contain a literal newline (`call-…\nfc_…`) — never parse
  payloads line-wise; always `jq` the whole object.

### Matcher names confirmed

`Read`, `Write`, `Shell` — exact tool names in `tool_name`, matching the
docs' matcher vocabulary.

## Method log

| Date | Attempt | Result |
|------|---------|--------|
| 2026-07-28 | `agent -f -p …` ×2, plugin via `~/.cursor/plugins/local` | Plugin hooks did not fire (0-byte probe log); user sessionStart hook fired |
| 2026-07-30 | `cursor-agent --plugin-dir … -p -f` (plugin, `${CURSOR_PLUGIN_ROOT}` cmd) | Plugin hooks did not fire |
| 2026-07-30 | same, relative `./scripts/probe.sh` command form | Plugin hooks did not fire |
| 2026-07-30 | interactive `cursor-agent --plugin-dir …` in tmux | Plugin hooks did not fire |
| 2026-07-30 | user `~/.cursor/hooks.json` postToolUse, `-p` mode | Fired for Shell + Write (payloads above) |
| 2026-07-30 | user hooks.json postToolUse + postToolUseFailure, Read of real 64×64 png | postToolUse fired for Read; failure event silent on success |
| 2026-07-30 | sessionStart `{additional_context}` via user hook | Marker visible to model (YES-marker) |
| 2026-07-30 | postToolUse `{additional_context}` via user hook | Marker visible to model (YES-postmarker) |

## If IDE plugin hooks are later proven

The adapter can add a thin `.cursor-plugin` package that reuses the same
hook scripts; the capture contract above is unchanged. Until then the plugin
layout is not built.
