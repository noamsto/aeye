# Spike: Codex hook runtime contract (aeye #122)

Date: 2026-07-13
Version tested: `codex-cli 0.144.1`, model `gpt-5.6-sol`
Method: throwaway `aeye-spike` plugin (echo probe) installed via a local
marketplace, driven by `codex exec --skip-git-repo-check --dangerously-bypass-hook-trust`
against `/tmp/aeye-spike-work`; payloads captured to `/tmp/aeye-hook-probe.log`.

## GATE: PASS

`PostToolUse` and `SessionStart` command hooks fire in 0.144.1. The adapter is
buildable. The contract is essentially Claude-equivalent — simpler than the spec
feared.

## Confirmed contract

### Common payload fields (stdin JSON, verbatim keys)
`session_id`, `transcript_path`, `cwd`, `hook_event_name`, `model`,
`permission_mode`. `turn_id` present on PostToolUse, absent on SessionStart.

### SessionStart
```json
{"session_id":"…","transcript_path":"/home/noams/.codex/sessions/2026/07/13/rollout-….jsonl",
 "cwd":"/tmp/aeye-spike-work","hook_event_name":"SessionStart","model":"gpt-5.6-sol",
 "permission_mode":"default","source":"startup"}
```
- `source` observed: `startup`. (docs: also `resume`/`clear`/`compact`.)
- `transcript_path` is populated — resume backfill has its input.
- The probe's `hookSpecificOutput.additionalContext` JSON was accepted (hook
  reported Completed); shape is identical to Claude's.

### PostToolUse — payload is NORMALIZED (key finding)

Even though the model used the unified `exec`/JS transport internally, the hook
payload reports clean tool names and structured input:

| `tool_name` | `tool_input` | `tool_response` |
|---|---|---|
| `apply_patch` | `{"command":"*** Begin Patch\n*** Add File: diagram.d2\n+a -> b: hi\n*** End Patch"}` | `"Exit code: 0…Success. Updated the following files:\nA diagram.d2\n"` |
| `view_image` | `{"path":"/tmp/aeye-spike-work/sample.png"}` | `[{"type":"input_image","image_url":"data:…"}]` |
| `Bash` | `{"command":"echo aeye-spike-done"}` | `"aeye-spike-done\n"` |

Consequences:
- **Live matcher uses clean names**: `apply_patch|view_image|Bash|mcp__*` — NOT
  `exec`. The `exec` transport is unwrapped before the hook fires.
- **No JS-string decoding on the hook path.** `apply_patch` → parse
  `tool_input.command` for `*** Add/Update File:`; `view_image` → `tool_input.path`
  directly; screenshots → scan `tool_response`.
- Paths are as the model wrote them — `view_image.path` was absolute here.

### Env + working directory
- `PLUGIN_ROOT` = `CLAUDE_PLUGIN_ROOT` = `~/.codex/plugins/cache/<marketplace>/<plugin>/<version>/`
  (both set, identical value).
- `PLUGIN_DATA` = `~/.codex/plugins/data/<plugin>-<marketplace>` (writable).
- Command working directory = session `cwd` (`/tmp/aeye-spike-work`). Scripts MUST
  be invoked via `"$PLUGIN_ROOT"/…`.

### Not empirically confirmed (low risk)
- Whether `PostToolUse` honors `hookSpecificOutput.additionalContext` (the
  `diagrams.sh` markdown-blank warning uses it). SessionStart honors it; assume
  PostToolUse does too, confirm during Task 2.3.

## Transcript shape (for backfill, Phase 3) — RAW, not normalized

The rollout `.jsonl` stores the raw `exec`/JS transport (unlike the hook):
```
custom_tool_call name=exec  input=`const r = await tools.apply_patch("*** Begin Patch\n*** Add File: diagram.d2\n…*** End Patch"); text(r);`
custom_tool_call name=exec  input=`const r = await tools.view_image({path:"/tmp/aeye-spike-work/sample.png"}); image(r.image_url);`
custom_tool_call name=exec  input=`const r = await tools.exec_command({cmd:"echo aeye-spike-done",workdir:"…"});`
```
Backfill MUST unwrap this: extract the `tools.apply_patch("…")` string arg
(JSON-escaped) and the `tools.view_image({path:…})` path. Note the JS object uses
an **unquoted** key (`path:`), so the extractor cannot assume JSON. Older sessions
also show the direct `custom_tool_call name=apply_patch` (raw envelope) and
`function_call name=exec_command` (`arguments` JSON) forms — the iterator handles
all three.

## Install & hook-trust (Task 0.2 partial)

- `codex plugin marketplace add <local-root>` + `codex plugin add <plugin>@<marketplace>`
  **copies** the plugin into `~/.codex/plugins/cache/<mp>/<plugin>/<version>/`. A
  read-only `/nix/store` source is therefore copied at install time (safe to point
  a marketplace at a store path).
- Neither `marketplace add` nor `plugin add` prompted for trust in this run.
- **Hooks did NOT run until `--dangerously-bypass-hook-trust` was passed** — there
  is a runtime hook-trust gate.

### Task 0.2 RESOLVED — trust mechanism (docs + local probe)

Codex hook trust is:
- **Interactive** via the in-Codex `/hooks` command. "Installing or enabling a
  plugin doesn't automatically trust its hooks; Codex skips plugin-bundled hooks
  until you review and trust the current hook definition."
- **Hash-based**: "Codex records trust against the hook's current hash, so new or
  changed hooks are marked for review and skipped until trusted." → every aeye
  update that changes `hooks.json` or a hook script re-triggers review.
- Persistence store is not a plaintext file or an obvious sqlite table (checked
  `state_5.sqlite`/`logs_2.sqlite`, config.toml, and the plugins dir — nothing
  fakeable declaratively).

Non-interactive routes:
- `--dangerously-bypass-hook-trust` — per-invocation only, DANGEROUS, not for
  normal interactive sessions.
- **Managed hooks** via `requirements.toml` (enterprise/MDM, "trusted by policy")
  — not a user-level Home-Manager mechanism; no `requirements.toml` present, no
  CLI surface for it; out of scope.

**Decision for Task 4.1 (degraded, as the spec anticipated):** Nix provides the
marketplace source + exports the `AEYE_D2_*` env; the user runs `codex plugin add`
(or it's pre-added) and **`/hooks` once to trust aeye**. Document that a plugin
update requires re-running `/hooks` (hash-based trust). No fully-declarative
auto-trust is available without the enterprise policy path.
