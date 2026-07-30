# Cursor Agent Adapter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `adapters/cursor/` so Cursor Agent in tmux captures Read/Write/Shell image touches and D2 diagrams into the aeye carousel at parity with Claude Code / Codex, Marketplace-ready.

**Architecture:** Thin Cursor shim over vendored `adapters/core/` (Codex pattern). Cursor-shaped `hooks/hooks.json` (`postToolUse` / `sessionStart`, flat `{command,matcher}` entries), `lib/shim.sh` for path + session extraction, ported hook scripts, adapted skills. Viewer and manifest schema unchanged.

**Tech Stack:** Bash + `jq`, `bats`, Cursor Agent hooks/plugins (`.cursor-plugin/`), existing `aeye render-diagram` + `resvg`, tmux carousel (`tmux-claude-images`).

**Spec:** `docs/superpowers/specs/2026-07-27-cursor-adapter-design.md` · **Issue:** [#157](https://github.com/noamsto/aeye/issues/157)

## Global Constraints

- Hook scripts are Bash + `jq` only — match Claude/Codex style (`set -euo pipefail`, echo-or-nothing helpers that return 0 on miss).
- Cursor `hooks.json` uses **camelCase** events and the **flat** script shape `{ "command": "./scripts/….sh", "matcher": "…" }` plus top-level `"version": 1` — **not** Claude/Codex nested `{ "hooks": [ { "type": "command", … } ] }`.
- SessionStart guidance / diagram warnings must emit Cursor JSON: `{ "additional_context": "…" }` (snake_case), **not** Claude `hookSpecificOutput.additionalContext`.
- Manifest schema, state dir, owner/lock sidecars, d2 cache layout are FROZEN.
- Vendor `adapters/core/` into `plugin/scripts/core/` (copy, not symlink). Keep in sync via `just sync-cursor-core` + `tests/cursor/core-sync.bats`.
- Primary runtime: Cursor Agent in **tmux** (`TMUX_PANE` keying). Browser/MCP screenshot capture and nix-config HM wiring are out of this plan.
- Prefer work on a feature branch / worktree (`feat/157-cursor-adapter`); do not force-push `main`.
- Phase 1+ unlocked only if Spike 0.1 GATE = PASS.

## File map

| Path | Responsibility |
|------|----------------|
| `docs/superpowers/spikes/2026-07-28-cursor-hook-contract.md` | Empirical hook contract |
| `adapters/cursor/.cursor-plugin/marketplace.json` | Marketplace root |
| `adapters/cursor/plugin/.cursor-plugin/plugin.json` | Plugin manifest |
| `adapters/cursor/plugin/hooks/hooks.json` | Hook registration |
| `adapters/cursor/plugin/scripts/lib/shim.sh` | Session id + path extraction |
| `adapters/cursor/plugin/scripts/core/*` | Vendored core |
| `adapters/cursor/plugin/scripts/{images,diagrams,diagram-guidance,session-reset,session-backfill}.sh` | Hook entrypoints |
| `adapters/cursor/plugin/skills/{diagrams,image-gallery}/` | Skills |
| `adapters/cursor/README.md` | Install + smoke |
| `tests/cursor/*.bats`, `tests/fixtures/cursor/` | Adapter tests |
| `justfile` | `sync-cursor-core` |
| `docs/INSTALL.md`, root `README.md` | User-facing install |
| `adapters/core/manifest-extract.sh` | Also scan `.tool_output` (Cursor name) |

---

## Phase 0 — Spike (BLOCKING GATE)

### Task 0.1: Prove Cursor plugin hooks fire

**Why:** Docs claim `postToolUse` / `sessionStart`; Marketplace/local plugin behavior is unproven. If hooks do not fire, stop.

**Files:**
- Create (throwaway): `~/.cursor-aeye-spike/plugin/` (manifest + hooks + `scripts/probe.sh`)
- Create: `docs/superpowers/spikes/2026-07-28-cursor-hook-contract.md`

**Interfaces:**
- Produces: confirmed stdin JSON keys, env vars, `additional_context` behavior, Read/Write `tool_input` path field name(s), whether `sessionStart` has `source` / only fires on new conversations, `transcript_path` presence.

- [ ] **Step 1: Scaffold a local spike plugin**

```bash
mkdir -p ~/.cursor-aeye-spike/plugin/{.cursor-plugin,hooks,scripts}
```

`~/.cursor-aeye-spike/plugin/.cursor-plugin/plugin.json`:
```json
{
  "name": "aeye-spike",
  "version": "0.0.1",
  "description": "Throwaway probe for Cursor hook contract",
  "author": { "name": "Noam Stolero" }
}
```

`~/.cursor-aeye-spike/plugin/hooks/hooks.json`:
```json
{
  "version": 1,
  "hooks": {
    "sessionStart": [
      { "command": "./scripts/probe.sh" }
    ],
    "postToolUse": [
      {
        "command": "./scripts/probe.sh",
        "matcher": "Read|Write|Shell"
      }
    ]
  }
}
```

`~/.cursor-aeye-spike/plugin/scripts/probe.sh` (chmod +x):
```bash
#!/usr/bin/env bash
set -euo pipefail
{
  printf '=== %s ===\n' "$(date -Is)"
  printf 'pwd=%s\n' "$(pwd)"
  printf 'env PLUGIN_ROOT=%s CURSOR_PLUGIN_ROOT=%s CLAUDE_PLUGIN_ROOT=%s\n' \
    "${PLUGIN_ROOT:-}" "${CURSOR_PLUGIN_ROOT:-}" "${CLAUDE_PLUGIN_ROOT:-}"
  printf 'STDIN:\n'
  cat
  printf '\n'
} >>/tmp/aeye-cursor-hook-probe.log 2>&1

# If this looks like sessionStart, also try injecting context (spike confirms shape).
payload="$(tail -n 200 /tmp/aeye-cursor-hook-probe.log | sed -n '/^{/,$p' | head -c 200000 || true)"
# Safer: re-read stdin was already consumed — so duplicate via tee in a revised probe:
# Prefer rewriting probe to: payload=$(tee -a log); then branch on hook_event_name.
```

Rewrite probe so stdin is teed (do not use the broken “re-read” sketch above):

```bash
#!/usr/bin/env bash
set -euo pipefail
LOG=/tmp/aeye-cursor-hook-probe.log
payload="$(tee -a "$LOG")"
{
  printf '=== %s ===\n' "$(date -Is)"
  printf 'pwd=%s PLUGIN_ROOT=%s CURSOR_PLUGIN_ROOT=%s CLAUDE_PLUGIN_ROOT=%s\n' \
    "$(pwd)" "${PLUGIN_ROOT:-}" "${CURSOR_PLUGIN_ROOT:-}" "${CLAUDE_PLUGIN_ROOT:-}"
  printf 'STDIN:\n%s\n' "$payload"
} >>"$LOG"

event="$(jq -r '.hook_event_name // empty' <<<"$payload" 2>/dev/null || true)"
if [[ $event == sessionStart ]]; then
  jq -nc '{additional_context:"AEYE_CURSOR_SPIKE_CONTEXT_MARKER"}'
fi
```

- [ ] **Step 2: Install via local plugins path**

```bash
mkdir -p ~/.cursor/plugins/local
ln -sfn ~/.cursor-aeye-spike/plugin ~/.cursor/plugins/local/aeye-spike
```

Reload Cursor window (`Developer: Reload Window`). Record any trust / enable UI under Customize → Plugins/Hooks.

- [ ] **Step 3: Drive Cursor Agent in tmux**

In a tmux pane, start Cursor Agent against a throwaway dir. Cause:
1. A new Agent conversation (sessionStart)
2. `Read` of an existing `.png`
3. `Write` of a tiny `.d2` and of a `.png` (or copy)
4. A `Shell` that prints a path under cwd to a png

Then:
```bash
cat /tmp/aeye-cursor-hook-probe.log
```

Confirm the spike marker appeared in the Agent context (ask the model if it sees `AEYE_CURSOR_SPIKE_CONTEXT_MARKER`).

- [ ] **Step 4: Write the spike deliverable**

In `docs/superpowers/spikes/2026-07-28-cursor-hook-contract.md` record GATE PASS/FAIL and verbatim:

- Whether `sessionStart` / `postToolUse` fired
- Exact stdin keys (`conversation_id`, `session_id`, `cwd`, `tool_name`, `tool_input`, `tool_output`, `transcript_path`, `hook_event_name`, …)
- Path field for Read/Write (`tool_input.file_path` vs `.path` vs other)
- Env vars for plugin root; command cwd
- Whether `{additional_context:…}` was accepted on sessionStart (and optionally postToolUse)
- Whether sessionStart has a Claude-like `source` (startup/resume) — docs say new-composer-only; confirm
- `transcript_path` null or set; implications for backfill

- [ ] **Step 5: GATE**

- FAIL (hooks never fire) → STOP. Comment on #157; do not build the adapter.
- PASS → proceed; note any field deltas Phase 1+ must honor.

- [ ] **Step 6: Clean up spike install + commit spike doc**

```bash
rm -f ~/.cursor/plugins/local/aeye-spike
gtrash put ~/.cursor-aeye-spike || rm -rf ~/.cursor-aeye-spike
git add docs/superpowers/spikes/2026-07-28-cursor-hook-contract.md
git commit -m "docs(spike): confirm Cursor hook runtime contract (#157)"
```

---

## Phase 1 — Scaffold + core scan tweak

### Task 1.1: Teach core to scan Cursor `tool_output`

**Files:**
- Modify: `adapters/core/manifest-extract.sh` (`scan_response_image_path` jq)
- Modify: `adapters/codex/plugin/scripts/core/manifest-extract.sh` (re-vendor via just)
- Test: existing bats that cover `scan_response_image_path` / response scan (locate with `rg scan_response_image_path tests`)

**Interfaces:**
- Produces: `scan_response_image_path` also walks `.tool_output` when `.tool_response` is absent/empty (Cursor name).

- [ ] **Step 1: Find and run existing coverage**

```bash
rg -n 'scan_response_image_path|tool_response' tests adapters/core
bats tests/manifest-extract.bats tests/adapter.bats 2>/dev/null || bats --recursive tests/
```

- [ ] **Step 2: Add a failing assertion** (extend an existing bats or add `tests/cursor/tool-output-scan.bats` temporarily under tests that source core):

Payload with only `tool_output` string embedding a cwd-scoped png must be found after the change.

- [ ] **Step 3: Patch jq in `scan_response_image_path`**

Change the walk source from `.tool_response` to:

```jq
[.tool_response // .tool_output | .. | strings
```

Keep the same capture / cwd / existence guards.

- [ ] **Step 4: Re-vendor Codex core + run tests**

```bash
just sync-codex-core
bats tests/codex/core-sync.bats
bats --recursive tests/   # or the narrower set that covers extract
```

- [ ] **Step 5: Commit**

```bash
git add adapters/core/manifest-extract.sh adapters/codex/plugin/scripts/core/manifest-extract.sh tests
git commit -m "fix(core): scan Cursor tool_output in response image path (#157)"
```

### Task 1.2: Scaffold `adapters/cursor/` plugin skeleton

**Files:**
- Create: layout under `adapters/cursor/` per spec
- Create: `justfile` recipe `sync-cursor-core`
- Create: `tests/cursor/core-sync.bats`

**Interfaces:**
- Produces: empty-but-valid plugin installable at `~/.cursor/plugins/local/aeye`; `just sync-cursor-core` copies core.

- [ ] **Step 1: Create directories + vendor core**

```bash
mkdir -p adapters/cursor/plugin/{.cursor-plugin,hooks,scripts/{lib,core},skills,assets}
mkdir -p adapters/cursor/.cursor-plugin
justfile addition — see Step 3
cp adapters/core/manifest-extract.sh adapters/core/manifest-lifecycle.sh \
  adapters/cursor/plugin/scripts/core/
```

- [ ] **Step 2: Write manifests**

`adapters/cursor/plugin/.cursor-plugin/plugin.json`:
```json
{
  "name": "aeye",
  "version": "0.1.0",
  "description": "Capture images this Cursor Agent session touches and browse them in a tmux/kitty carousel.",
  "author": { "name": "Noam Stolero" },
  "repository": "https://github.com/noamsto/aeye",
  "license": "MIT",
  "keywords": ["carousel", "diagrams", "d2", "tmux", "images"],
  "logo": "assets/logo.svg"
}
```

Copy or symlink-equivalent logo: reuse an existing repo logo if present (`ls docs/assets`), else omit `logo` until an SVG is added under `adapters/cursor/plugin/assets/logo.svg`.

`adapters/cursor/.cursor-plugin/marketplace.json`:
```json
{
  "name": "aeye",
  "owner": { "name": "noamsto" },
  "metadata": {
    "description": "aeye Cursor Agent capture adapter"
  },
  "plugins": [
    {
      "name": "aeye",
      "source": "./plugin",
      "description": "Image carousel capture for Cursor Agent (tmux)."
    }
  ]
}
```

`adapters/cursor/plugin/hooks/hooks.json` (matchers from spike; default below):
```json
{
  "version": 1,
  "hooks": {
    "postToolUse": [
      {
        "command": "./scripts/images.sh",
        "matcher": "Read|Write|Shell"
      },
      {
        "command": "./scripts/diagrams.sh",
        "matcher": "Write|Shell"
      }
    ],
    "sessionStart": [
      { "command": "./scripts/diagram-guidance.sh" },
      { "command": "./scripts/session-reset.sh" },
      { "command": "./scripts/session-backfill.sh" }
    ]
  }
}
```

Placeholder scripts (chmod +x) that `exit 0` until Phase 2–3 replace them:
```bash
#!/usr/bin/env bash
exit 0
```
for each of `images.sh`, `diagrams.sh`, `diagram-guidance.sh`, `session-reset.sh`, `session-backfill.sh`.

- [ ] **Step 3: Add `just sync-cursor-core` + core-sync bats**

In `justfile` after `sync-codex-core`:
```just
sync-cursor-core:
    cp adapters/core/manifest-extract.sh adapters/core/manifest-lifecycle.sh adapters/cursor/plugin/scripts/core/
```

`tests/cursor/core-sync.bats` — copy from `tests/codex/core-sync.bats`, replace paths/`sync-codex-core` → `sync-cursor-core`.

- [ ] **Step 4: Commit**

```bash
git add adapters/cursor justfile tests/cursor/core-sync.bats
git commit -m "feat(cursor): scaffold plugin layout + vendor core (#157)"
```

---

## Phase 2 — Shim (TDD)

### Task 2.1: `lib/shim.sh` path + session extraction

**Files:**
- Create: `adapters/cursor/plugin/scripts/lib/shim.sh`
- Create: `tests/cursor/extract.bats`
- Create: `tests/fixtures/cursor/` (reuse pngs from `tests/fixtures/codex/` via copy or relative paths)

**Interfaces:**
- Consumes: `scan_response_image_path` from vendored core; spike-confirmed field names.
- Produces:
  - `cursor_session_id PAYLOAD` → `conversation_id` // `session_id` // empty
  - `cursor_extract_touched_paths PAYLOAD` → newline-separated existing image/`.d2` paths

Adjust field names in tests/implementation if the spike differs (e.g. only `path`, not `file_path`).

- [ ] **Step 1: Write failing bats**

`tests/cursor/extract.bats`:
```bash
#!/usr/bin/env bats

setup() {
	ROOT="$(dirname "$(dirname "$BATS_TEST_DIRNAME")")"
	LIB="$ROOT/adapters/cursor/plugin/scripts/lib/shim.sh"
	FIXTURES="$ROOT/tests/fixtures/cursor"
	# shellcheck source=/dev/null
	source "$LIB"
}

@test "cursor_extract_touched_paths: Write of existing png via file_path" {
	PNG="$FIXTURES/shot.png"
	payload="$(jq -nc --arg p "$PNG" '{tool_name:"Write",tool_input:{file_path:$p},cwd:"/repo"}')"
	run cursor_extract_touched_paths "$payload"
	[ "$status" -eq 0 ]
	[ "$output" = "$PNG" ]
}

@test "cursor_extract_touched_paths: Read of existing png via path" {
	PNG="$FIXTURES/shot.png"
	payload="$(jq -nc --arg p "$PNG" '{tool_name:"Read",tool_input:{path:$p},cwd:"/repo"}')"
	run cursor_extract_touched_paths "$payload"
	[ "$status" -eq 0 ]
	[ "$output" = "$PNG" ]
}

@test "cursor_extract_touched_paths: Write relative d2 resolves against cwd" {
	D2="$BATS_TEST_TMPDIR/auth-flow.d2"
	printf 'a -> b\n' >"$D2"
	payload="$(jq -nc --arg c "$BATS_TEST_TMPDIR" '{tool_name:"Write",tool_input:{file_path:"auth-flow.d2"},cwd:$c}')"
	run cursor_extract_touched_paths "$payload"
	[ "$status" -eq 0 ]
	[ "$output" = "$D2" ]
}

@test "cursor_extract_touched_paths: Write README.md is not echoed" {
	payload="$(jq -nc --arg c "$BATS_TEST_TMPDIR" '{tool_name:"Write",tool_input:{file_path:"README.md"},cwd:$c}')"
	printf 'x\n' >"$BATS_TEST_TMPDIR/README.md"
	run cursor_extract_touched_paths "$payload"
	[ "$status" -eq 0 ]
	[ -z "$output" ]
}

@test "cursor_extract_touched_paths: Shell tool_output embedding cwd png is captured" {
	PNG="$FIXTURES/shot.png"
	# Ensure PNG is under a cwd we set to its dirname for Phase-2 guard
	cwd="$(dirname "$PNG")"
	payload="$(jq -nc --arg c "$cwd" --arg p "$PNG" \
		'{tool_name:"Shell",tool_input:{command:"ls"},tool_output:("saved to "+$p),cwd:$c}')"
	paths=()
	while IFS= read -r p; do [[ -n $p ]] && paths+=("$p"); done < <(cursor_extract_touched_paths "$payload")
	printf '%s\n' "${paths[@]}" | grep -qF "$PNG"
}

@test "cursor_session_id: prefers conversation_id" {
	payload="$(jq -nc '{conversation_id:"conv-1",session_id:"sess-2"}')"
	run cursor_session_id "$payload"
	[ "$status" -eq 0 ]
	[ "$output" = "conv-1" ]
}

@test "cursor_session_id: falls back to session_id" {
	payload="$(jq -nc '{session_id:"sess-2"}')"
	run cursor_session_id "$payload"
	[ "$status" -eq 0 ]
	[ "$output" = "sess-2" ]
}

@test "cursor_session_id: missing -> empty" {
	payload="$(jq -nc '{cwd:"/repo"}')"
	run cursor_session_id "$payload"
	[ "$status" -eq 0 ]
	[ -z "$output" ]
}
```

```bash
mkdir -p tests/fixtures/cursor
cp tests/fixtures/codex/shot.png tests/fixtures/cursor/shot.png 2>/dev/null \
  || cp tests/fixtures/codex/*.png tests/fixtures/cursor/ \
  || (printf 'need a small png fixture' >&2; exit 1)
```

- [ ] **Step 2: Run → fail**

```bash
bats tests/cursor/extract.bats
```
Expected: fail (shim missing / functions undefined).

- [ ] **Step 3: Implement shim**

`adapters/cursor/plugin/scripts/lib/shim.sh`:
```bash
#!/usr/bin/env bash
# Cursor-specific session id + path extraction. Pure helpers: echo-or-nothing, return 0.

# shellcheck source=../core/manifest-extract.sh disable=SC1091
source "$(dirname "${BASH_SOURCE[0]}")/../core/manifest-extract.sh"

cursor_session_id() {
	jq -r '.conversation_id // .session_id // empty' <<<"$1" 2>/dev/null
}

# cursor_extract_touched_paths PAYLOAD -> newline-separated existing image/.d2 paths
cursor_extract_touched_paths() {
	local payload="$1" cwd name p
	cwd="$(jq -r '.cwd // empty' <<<"$payload" 2>/dev/null)"
	name="$(jq -r '.tool_name // empty' <<<"$payload" 2>/dev/null)"

	emit() {
		local q="$1"
		[[ -z $q ]] && return 0
		[[ $q != /* && -n $cwd ]] && q="$cwd/$q"
		[[ ${q,,} =~ \.(png|jpe?g|gif|webp|bmp|d2)$ ]] || return 0
		[[ -f $q ]] || return 0
		printf '%s\n' "$q"
	}

	case "$name" in
	Read | Write)
		emit "$(jq -r '.tool_input.file_path // .tool_input.path // empty' <<<"$payload" 2>/dev/null)"
		;;
	Shell)
		# Prefer structured paths if Cursor ever adds them; always scan output.
		:
		;;
	esac

	local resp
	resp="$(scan_response_image_path "$payload")"
	[[ -n $resp ]] && printf '%s\n' "$resp"
	return 0
}
```

If spike showed Shell commands embedding paths without `tool_output` scan hits, extend Shell branch conservatively (only match obvious absolute paths with image/d2 extensions in `tool_input.command`) — prefer false negatives.

- [ ] **Step 4: Run → pass**

```bash
bats tests/cursor/extract.bats
```

- [ ] **Step 5: Commit**

```bash
git add adapters/cursor/plugin/scripts/lib/shim.sh tests/cursor/extract.bats tests/fixtures/cursor
git commit -m "feat(cursor): shim for session id + Read/Write/Shell path extraction (#157)"
```

---

## Phase 3 — Hook scripts

### Task 3.1: `images.sh` + `diagrams.sh`

**Files:**
- Replace: `adapters/cursor/plugin/scripts/images.sh`
- Replace: `adapters/cursor/plugin/scripts/diagrams.sh`
- Create: `tests/cursor/images.bats`, `tests/cursor/diagrams.bats` (mirror `tests/codex/images.bats` / `diagrams.bats` patterns)

**Interfaces:**
- Consumes: `cursor_session_id`, `cursor_extract_touched_paths`, core lifecycle/render
- Produces: manifest appends; diagrams emit Cursor `additional_context` on markdown-blank failure if Codex/Claude does equivalent

- [ ] **Step 1: Port images.sh from Codex**

Copy `adapters/codex/plugin/scripts/images.sh` → cursor, then replace:
- comments “Codex” → “Cursor”
- `codex_session_id` → `cursor_session_id`
- `codex_extract_touched_paths` → `cursor_extract_touched_paths`
- Keep PLUGIN_ROOT / BASH_SOURCE resolution, TMUX_PANE keying, d2 artifact skip, lock, owner_selfheal, append loop

- [ ] **Step 2: Port diagrams.sh from Codex**

Same renames. Where Codex emits:

```bash
jq -nc --arg ctx "$msg" \
  '{hookSpecificOutput:{hookEventName:"PostToolUse",additionalContext:$ctx}}'
```

emit Cursor shape instead:

```bash
jq -nc --arg ctx "$msg" '{additional_context:$ctx}'
```

- [ ] **Step 3: Add bats that feed synthetic payloads through the scripts**

Follow `tests/codex/images.bats`: set `TMUX_PANE`, `AEYE_DIR` tmp, Write/Read payload on stdin, assert manifest JSONL line. For diagrams, write a minimal `.d2`, stub or require `aeye`+`resvg` on PATH (skip if missing with a clear skip message — match existing diagrams bats style).

```bash
bats tests/cursor/images.bats tests/cursor/diagrams.bats
```

- [ ] **Step 4: Commit**

```bash
git add adapters/cursor/plugin/scripts/images.sh adapters/cursor/plugin/scripts/diagrams.sh tests/cursor
git commit -m "feat(cursor): images + diagrams postToolUse hooks (#157)"
```

### Task 3.2: SessionStart scripts

**Files:**
- Replace: `diagram-guidance.sh`, `session-reset.sh`, `session-backfill.sh`
- Create: `tests/cursor/diagram-guidance.bats`, `tests/cursor/session-reset.bats`

**Interfaces:**
- Consumes: spike findings on sessionStart lifecycle
- Produces: Cursor `additional_context` guidance; reset clears pane on new conversation

**Cursor-specific lifecycle (per docs; confirm in spike):**
- `sessionStart` fires when a **new** composer conversation is created — there is no documented `source: resume`.
- Therefore: `session-reset.sh` always applies **startup** semantics (clear pane + stamp owner) when keyed; ignore missing `source` or treat empty as startup.
- `session-backfill.sh`: if spike shows `transcript_path` null / unusable → ship as **no-op** `exit 0` with a header comment + INSTALL note “backfill deferred”; do not pretend resume parity.

- [ ] **Step 1: Port diagram-guidance.sh**

From Codex: keep host gate (`TMUX` / `KITTY_LISTEN_ON`), PATH preflight for `aeye`/`resvg`, scratch dir creation. Change JSON out to:

```bash
jq -nc --arg ctx "$guidance" '{additional_context:$ctx}'
```

(and the same for the missing-binaries warn).

- [ ] **Step 2: Port session-reset.sh with Cursor semantics**

```bash
# After reading payload / resolving pane:
# If spike confirms no .source field:
case "${source:-startup}" in
startup | clear | "")
  clear_pane "$pane_file"
  [[ -n $session ]] && printf '%s' "$session" >"$owner_file"
  ;;
resume)
  # Only if spike proves resume sessionStart exists
  ;;
esac
```

Use `cursor_session_id`. Keep GC sweep from Codex if it is in core helpers / script body.

- [ ] **Step 3: session-backfill.sh**

If transcript usable: port Codex backfill with Cursor tool-name mapping (`Write`/`Read`/`Shell`) — only after spike proves transcript shape. Else:

```bash
#!/usr/bin/env bash
# Cursor sessionStart is new-conversation-only (see spike). Resume backfill
# deferred until a transcript iterator exists — #157 follow-up.
exit 0
```

- [ ] **Step 4: bats for guidance JSON shape + reset clear**

```bash
bats tests/cursor/diagram-guidance.bats tests/cursor/session-reset.bats
```

Assert guidance stdout is valid JSON with `.additional_context` and does **not** contain `hookSpecificOutput`.

- [ ] **Step 5: Commit**

```bash
git add adapters/cursor/plugin/scripts/diagram-guidance.sh \
  adapters/cursor/plugin/scripts/session-reset.sh \
  adapters/cursor/plugin/scripts/session-backfill.sh tests/cursor
git commit -m "feat(cursor): SessionStart guidance + reset (+ backfill or stub) (#157)"
```

---

## Phase 4 — Skills, docs, smoke, publish checklist

### Task 4.1: Adapt skills

**Files:**
- Create: `adapters/cursor/plugin/skills/diagrams/SKILL.md`
- Create: `adapters/cursor/plugin/skills/image-gallery/SKILL.md`

- [ ] **Step 1: Copy from Codex skills**

```bash
cp -a adapters/codex/plugin/skills/diagrams adapters/cursor/plugin/skills/
cp -a adapters/codex/plugin/skills/image-gallery adapters/cursor/plugin/skills/
```

- [ ] **Step 2: Edit for Cursor**

In both SKILL.md files:
- Replace Codex/Claude tool names with Cursor `Read` / `Write` / `Shell`
- Say `postToolUse` (Cursor) rather than Claude `PostToolUse` if mentioned
- Keep D2 house style + scratch-dir contract unchanged

- [ ] **Step 3: Commit**

```bash
git add adapters/cursor/plugin/skills
git commit -m "feat(cursor): diagrams + image-gallery skills (#157)"
```

### Task 4.2: README + INSTALL + root README

**Files:**
- Create: `adapters/cursor/README.md`
- Modify: `docs/INSTALL.md` (Step 3 Cursor subsection)
- Modify: root `README.md` (adapters list)

- [ ] **Step 1: Write adapters/cursor/README.md** covering:
  - Local install: `ln -sfn …/adapters/cursor/plugin ~/.cursor/plugins/local/aeye` + Reload Window
  - Marketplace submit pointer (after smoke)
  - Deps: `aeye`, `tmux-claude-images`, `resvg` on PATH
  - Smoke steps (Read png → manifest; Write d2 → carousel)
  - Limitations: tmux-primary; no browser/MCP capture; backfill status per spike

- [ ] **Step 2: INSTALL.md Cursor section** (parallel to Codex):

```markdown
### Cursor Agent

Local plugin (dev / nix follow-up):

```bash
ln -sfn /path/to/aeye/adapters/cursor/plugin ~/.cursor/plugins/local/aeye
```

Then **Developer: Reload Window**. Enable the plugin under Customize if prompted.

Marketplace: install **aeye** from the Cursor Marketplace once published.

Open the carousel with `tmux-claude-images` as in Step 4.
```

- [ ] **Step 3: Root README** — under adapters bullet list, add Cursor next to Claude/Codex.

- [ ] **Step 4: Commit**

```bash
git add adapters/cursor/README.md docs/INSTALL.md README.md
git commit -m "docs(cursor): install + adapter README (#157)"
```

### Task 4.3: End-to-end smoke + publish checklist

**Files:** none required beyond notes on #157 / spike doc appendix

- [ ] **Step 1: Install real plugin locally**

```bash
ln -sfn "$(pwd)/adapters/cursor/plugin" ~/.cursor/plugins/local/aeye
# Reload Window
```

- [ ] **Step 2: Smoke in tmux**

1. New Cursor Agent chat
2. Confirm guidance / skills visible (`/diagrams` or skill list)
3. `Read` a png → `ls $AEYE_DIR/images/*.jsonl` shows path
4. `Write` `.d2` under SessionStart scratch dir → PNG appears; `tmux-claude-images` shows it

- [ ] **Step 3: Run full cursor + regression bats**

```bash
bats tests/cursor/*.bats
bats tests/codex/core-sync.bats
```

- [ ] **Step 4: Marketplace checklist** (comment on #157, do not block merge):

- [ ] Valid `.cursor-plugin/plugin.json`
- [ ] Skills frontmatter valid
- [ ] README documents deps
- [ ] Logo present or logo field removed
- [ ] Submit at https://cursor.com/marketplace/publish when ready

- [ ] **Step 5: Final commit if smoke fixed anything; open PR**

```bash
git status
# gh pr create … Closes #157
```

---

## Phase 5 — Follow-ups (issues only, not this PR)

- [ ] **Step 1: File nix-config follow-up** — HM symlink/copy of cursor plugin into `~/.cursor/plugins/local/aeye` (like Codex cache / Claude plugin-dir).
- [ ] **Step 2: File stretch issue** — browser/MCP screenshot capture via `postToolUse` matcher `MCP:*` after probing tool names.
- [ ] **Step 3: If backfill stubbed** — file resume/transcript backfill issue referencing the spike.

---

## Pivot addendum (2026-07-30, post-spike)

Spike (`docs/superpowers/spikes/2026-07-28-cursor-hook-contract.md`) proved
plugin-bundled hooks **never fire on the Cursor CLI** (the primary runtime),
while user/project `hooks.json` hooks fire with the full contract. Distribution
pivots accordingly:

- `adapters/cursor/` ships hook scripts + a `hooks.json` template +
  `install.sh` that merges entries into `~/.cursor/hooks.json` (absolute
  script paths) and links skills into `~/.cursor/skills/`. The
  `.cursor-plugin` / marketplace layout is **dropped** (IDE plugin path
  unproven — follow-up issue, not this PR).
- Field deltas all tasks must honor: `cwd` is an empty string in CLI payloads
  → resolve against `workspace_roots[0]`; Read/Write path =
  `tool_input.file_path`; `tool_output` is a JSON-encoded **string**
  (`fromjson?` before walking it); no `source` on sessionStart → reset always
  applies startup semantics; `transcript_path` is null at sessionStart but set
  on postToolUse → backfill stays a deferred stub in v1.
- Wherever task text below says plugin layout, `hooks/hooks.json` relative
  commands, or `./scripts/…` paths, read it as the hooks.json layout above.

## Spec coverage self-check

| Spec requirement | Task |
|------------------|------|
| Spike gates | 0.1 |
| `adapters/cursor/` layout + marketplace | 1.2 |
| Thin shim + core vendor + sync | 1.2, 2.1, just recipe |
| `postToolUse` images/diagrams | 3.1 |
| SessionStart guidance/reset/backfill | 3.2 |
| Cursor `additional_context` JSON | 3.1–3.2 |
| Skills diagrams + image-gallery | 4.1 |
| INSTALL + README | 4.2 |
| Smoke + Marketplace-ready | 4.3 |
| `tool_output` scan | 1.1 |
| Non-goals (Desktop, MCP, agent-skills, viewer) | excluded |
| nix follow-up | Phase 5 |

## Placeholder scan

No TBD/TODO left in task steps; spike-dependent field names are explicitly “adjust to spike” with default docs-based names in code blocks.
