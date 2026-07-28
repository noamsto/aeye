# Spike: Cursor plugin hook runtime contract (aeye #157)

Date: 2026-07-28
Version tested: `cursor agent` / Cursor `2026.07.23-e383d2b`
Method: throwaway `aeye-spike` local plugin at
`~/.cursor-aeye-spike/plugin/`, symlinked to
`~/.cursor/plugins/local/aeye-spike`, with teed `scripts/probe.sh` logging to
`/tmp/aeye-cursor-hook-probe.log`. Probe returns
`{additional_context:"AEYE_CURSOR_SPIKE_CONTEXT_MARKER"}` on `sessionStart`.

## GATE: UNPROVEN (BLOCKED on human reload + IDE Agent exercise)

Plugin-bundled `sessionStart` / `postToolUse` hooks were **not observed** in this
run. The spike is scaffolded and installed; **do not build `adapters/cursor/`**
until a human completes the steps below and updates this doc to PASS or FAIL.

### What was exercised automatically

| Attempt | Result |
|---------|--------|
| `agent -f -p …` in `/tmp/aeye-spike-work` (Read `sample.png`, Write `diagram.d2`, Shell `echo aeye-spike-done`) ×2 | Agent completed (`OK`); **`/tmp/aeye-cursor-hook-probe.log` stayed 0 bytes** |
| Manual stdin to `probe.sh` (sanity) | Probe script logs and returns marker JSON on synthetic `sessionStart` |

### Adjacent evidence (user hook, not plugin spike)

`~/.cursor/hooks.json` has a `sessionStart` hook (`cat > /tmp/cursor-hook.json`).
That hook **did fire** on the same `agent -f` sessions. Observed stdin (verbatim
keys from `/tmp/cursor-hook.json`):

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

Implications (from user hook only — **not confirmed for plugin hooks**):
- `cwd` absent; `workspace_roots` array present instead.
- `transcript_path` was `null` on CLI Agent sessions → resume backfill may be
  blocked or need another source if this holds for IDE Agent too.
- No Claude-like `source` field observed on `sessionStart`.
- `postToolUse` not observed (no user hook registered for it).

### Spike install state (left in place)

```
~/.cursor-aeye-spike/plugin/
  .cursor-plugin/plugin.json   # name: aeye-spike
  hooks/hooks.json             # sessionStart + postToolUse (Read|Write|Shell)
  scripts/probe.sh             # teed stdin → log; marker on sessionStart
~/.cursor/plugins/local/aeye-spike → ~/.cursor-aeye-spike/plugin
```

## Human next steps (required to prove or kill the gate)

1. **Reload Window** — `Developer: Reload Window` so Cursor picks up the new local
   plugin symlink.
2. **Customize → Plugins / Hooks** — enable or trust `aeye-spike` if prompted;
   record any UI gate.
3. **Truncate log:** `truncate -s 0 /tmp/aeye-cursor-hook-probe.log`
4. In **tmux**, start **Cursor Agent** (IDE Agent pane, not only `agent -f` CLI)
   against `/tmp/aeye-spike-work` and cause:
   - a **new** Agent conversation (`sessionStart`)
   - `Read` of an existing `.png`
   - `Write` of a tiny `.d2` and of a `.png` (or copy)
   - a `Shell` that prints a path under cwd to a png
5. `cat /tmp/aeye-cursor-hook-probe.log` — confirm entries with
   `hook_event_name` `sessionStart` and `postToolUse`.
6. Ask the model whether it sees `AEYE_CURSOR_SPIKE_CONTEXT_MARKER` in context.
7. Update this file: GATE **PASS** or **FAIL**, paste verbatim stdin keys and
   path field names from the log; then remove spike per brief Step 6 cleanup.

## Fields to record on PASS (checklist — unfilled until proven)

- [ ] `sessionStart` fired
- [ ] `postToolUse` fired (Read / Write / Shell)
- [ ] Stdin keys: `conversation_id`, `session_id`, `cwd`, `tool_name`,
  `tool_input`, `tool_output`, `transcript_path`, `hook_event_name`, …
- [ ] Read/Write path field: `tool_input.file_path` vs `.path` vs other
- [ ] Env: `PLUGIN_ROOT`, `CURSOR_PLUGIN_ROOT`, `CLAUDE_PLUGIN_ROOT`; command `pwd`
- [ ] `additional_context` accepted on `sessionStart` (marker visible?)
- [ ] `sessionStart` `source` (startup/resume) if present
- [ ] `transcript_path` null or set

## If GATE becomes FAIL

Comment on [#157](https://github.com/noamsto/aeye/issues/157); stop adapter work.
