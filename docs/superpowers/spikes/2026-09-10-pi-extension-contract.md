# Spike: pi extension runtime contract (aeye pi adapter)

Date: 2026-09-10
pi version tested: `0.85.1`

pi is a TypeScript-extension harness — it has **no shell hook system** (unlike
Claude Code / Codex / Cursor). So the pi adapter cannot be a `hooks.json`; it is
a [pi package](../../../adapters/pi) whose extension subscribes to lifecycle
events, translates them into the same normalized payload the other adapters feed
their scripts, and runs the shell capture pipeline unchanged.

## Proven contract

### Events used

| Event | Use |
|-------|-----|
| `session_start { reason }` | `reason` is `"startup" \| "reload" \| "new" \| "resume" \| "fork"`. Guidance (`diagram-guidance.sh`), manifest reset (`session-reset.sh`), and resume/fork rebuild (`session-backfill.sh`). |
| `before_agent_start` | Returns `{ message: { customType, content, display } }` — the only way to inject context-only guidance; maps to the Claude/Codex SessionStart `additionalContext`. Injected once per session. |
| `tool_result` | `{ toolName, input, content, details, isError }`. The capture payload is built from `toolName` + `input` + the **text** blocks of `content`. Handlers may return `{ content }` to append our diagram warning to the same tool result the model sees. |

Built-in tool names are lowercase: `read`, `write`, `edit`, `bash`, `grep`,
`find`, `ls` (plus `powershell` on Windows). Only `read`/`write`/`edit`/`bash`
are captured.

### Session id / keying

- `ctx.sessionManager.getSessionId()` yields the session UUID.
- Inside tmux the shell core keys by `<server pid>-<pane>` (capture + toggle
  agree without the id); the id is only the owner stamp.
- Outside tmux the id keys the manifest. The extension exports it as
  `AEYE_SESSION_ID` (a new agent-neutral variable) so the toggle and the
  viewer's owner check recognize a non-Claude session without faking
  `CLAUDE_CODE_SESSION_ID`.

### Resume/fork backfill

`ctx.sessionManager.getBranch()` returns the active entry path. Assistant
messages carry `toolCall` blocks (`id`, `name`, `arguments`); the matching
`toolResult` message carries the output text where a bash screenshot path may
live; user `!` commands appear as `bashExecution` messages (`command`,
`output`). The extension replays these in chronological order into a temp
JSONL file and `session-backfill.sh` rebuilds the manifest through the same
extractors the live hook uses. Image content blocks are dropped (the path comes
from the input; base64 would only bloat the file).

### Warnings

pi has no `additionalContext` channel. A D2 compile error or a markdown-blank
suppression is returned from the `tool_result` handler as an extra `text` block
appended to the tool result — same effect, surfaced on the turn that caused it.

## Method log

| Date | Check | Result |
|------|-------|--------|
| 2026-09-10 | `pi --offline -e adapters/pi --list-models` | exit 0 |
| 2026-09-10 | Node 24 type-strip import of `extensions/aeye.ts` | loads; default is a function |
| 2026-09-10 | Mock-`pi` harness: `session_start` → `before_agent_start` | guidance injected exactly once |
| 2026-09-10 | Mock-`pi` harness: `tool_result` read/write | image + `.d2` captured; carousel opened once |
| 2026-09-10 | Mock-`pi` harness: failing `render-diagram` | warning appended to the tool result |
| 2026-09-10 | Mock-`pi` harness: `session_start {reason:"resume"}` with a branch | foreign manifest wiped; historical read + bash screenshot replayed; owner stamped |

The shell scripts the extension drives are covered by `tests/pi/` (bats).

## Limitations

- **tmux-primary.** Outside tmux the manifest is keyed by the session id; the
  agent's own toggle invocation sees `$AEYE_SESSION_ID`, but a shell that never
  ran under pi needs it set.
- Backfill cannot recover a `.d2` whose only trace was a `$var`-built command
  path (the variable lived in the agent's shell), nor an image whose only trace
  was a compacted-away bash output.
