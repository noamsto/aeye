# Reset image manifest on fresh SessionStart

Issue: [#24](https://github.com/noamsto/aeye/issues/24)

## Problem

The viewer reads a per-key manifest at
`${AEYE_DIR:-${CLAUDE_STATUS_DIR:-/tmp/claude-status}}/images/<key>.jsonl`, where
`<key>` is the tmux pane id (or the Claude Code session id outside tmux). These
files live in `/tmp/claude-status` and persist until reboot — nothing clears
them. tmux **reuses pane ids**, so a new conversation in a reused pane inherits
the previous conversation's `<pane>.jsonl`, and the carousel shows diagrams and
images the current session never touched.

## Fix

A SessionStart hook, `session-reset.sh`, that removes the current key's manifest
when a session starts fresh, leaving it intact when work is merely continuing.

The SessionStart payload carries a `source` field:

| `source`  | Meaning                        | Action  |
| --------- | ------------------------------ | ------- |
| `startup` | brand-new session              | reset   |
| `clear`   | `/clear` — context wiped       | reset   |
| `resume`  | resuming an existing session   | keep    |
| `compact` | auto-compaction mid-task       | keep    |

`resume`/`compact` keep the manifest because the conversation — and the
relevance of its images — continues.

### Script

`adapters/claude-code/plugin/scripts/session-reset.sh`:

1. Read the payload on stdin; extract `source` with `jq`. If it is not
   `startup` or `clear`, `exit 0`.
2. Resolve the key **exactly** as `images.sh`/`diagrams.sh` do, so we clear the
   right file: `pane_id="${TMUX_PANE:-${CLAUDE_CODE_SESSION_ID:-}}"`, strip a
   leading `%`, and validate against `^[A-Za-z0-9_@:.-]+$` (path-traversal
   guard — the key becomes a filename). No key → `exit 0`.
3. `rm -f "$IMAGES_DIR/$pane_file.jsonl"`. Nothing else.

Not host-gated (unlike `diagram-guidance.sh`): the manifest path is the same
regardless of host, and clearing it is harmless everywhere. Outside tmux the key
is the session id, which is unique per session, so `startup` is naturally a
no-op there (no prior file) while `/clear` — which keeps the session id — still
benefits.

### Wiring

Add a second entry to the `SessionStart` array in `hooks.json`, alongside
`diagram-guidance.sh`. The two are independent (guidance emits context; reset
deletes a file), so order does not matter.

## Out of scope

- The shared, content-hashed `diagrams/*.png`, `*.svg`, and `src/*.d2` — these
  are referenced by other panes' manifests and pruning them is a separate GC
  concern. We clear only the per-key index; the viewer tolerates a missing
  manifest (renders the themed empty state).

## Testing

bats (`tests/session-reset.bats`), mirroring `guidance.bats`:

- `source=startup` → manifest removed
- `source=clear` → manifest removed
- `source=resume` → manifest kept
- `source=compact` → manifest kept
- no key (no `TMUX_PANE`, no `CLAUDE_CODE_SESSION_ID`) → no-op, exit 0
- outside tmux: keys by `CLAUDE_CODE_SESSION_ID` and removes that manifest
- missing manifest → exit 0, no error

Plus `hooks-json.bats`: assert `SessionStart` also runs `session-reset.sh`.
