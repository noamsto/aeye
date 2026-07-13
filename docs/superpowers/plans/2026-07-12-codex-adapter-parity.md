# Codex CLI Adapter Parity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a Codex CLI aeye plugin that captures the images/diagrams a Codex session touches (writes, screenshots, image views) into the carousel, at parity with the Claude adapter.

**Architecture:** Extract the agent-agnostic logic out of the Claude adapter into `adapters/core/` (shared shell libraries), then add `adapters/codex/plugin/` as a thin shim that supplies only the Codex-specific pieces: session-id source, touched-path extraction across Codex's two tool encodings (direct `apply_patch` + JS-`exec`-wrapped `apply_patch`/`view_image`), and a rollout-transcript iterator. The aeye viewer/carousel is unchanged.

**Tech Stack:** Bash + `jq` (hook scripts, matching the Claude adapter), `bats` + Go (`tests/`), the embedded `aeye render-diagram` + `resvg` d2 pipeline, Nix/Home Manager (`home/ai/codex/default.nix`), Codex plugin/marketplace tooling (`codex plugin …`).

## Global Constraints

- Hook scripts are Bash + `jq` only — no new runtime deps (Node/Python) on the capture path. Match the Claude adapter's style (`set -euo pipefail`, `printf`, functions echo-or-nothing, return 0 on "not found").
- Codex hook scripts invoke helpers via `"$PLUGIN_ROOT"/…` (Codex also sets `CLAUDE_PLUGIN_ROOT` for compat; prefer the native name). Commands run with the session `cwd` as working directory.
- Manifest schema, state dir keying, owner/lock sidecar conventions, and the d2 cache layout are FROZEN — the viewer reads them; do not change field names or paths.
- `.codex-plugin/plugin.json` must NOT declare `hooks` (the marketplace validator rejects it); rely on `hooks/hooks.json` auto-discovery. It MUST include a valid `interface` block with strict-semver `version`.
- The Claude adapter's existing test suite MUST pass unchanged after the core refactor (regression gate).
- Do not commit to `main`; all work on `feat/122-codex-adapter` in the worktree `~/Data/git/noamsto/aeye-worktrees/feat-122-codex-adapter`.

---

## Phase 0 — Spikes (GATES; produce knowledge, not shipping code)

These run first. Phase 1+ is unlocked only if Spike 1 passes.

### Task 0.1: Spike — prove the Codex hook runtime fires (BLOCKING GATE)

**Why:** Every downstream task assumes `PostToolUse`/`SessionStart` command hooks execute in `codex-cli 0.144.1` with the documented stdin payload. The only local hook script is headed "Draft hook example for future plugin hook runtimes," so this is unproven. If it fails, the adapter is not buildable now — STOP and report.

**Files:**
- Create (throwaway, outside the repo): `~/.codex-aeye-spike/plugin/.codex-plugin/plugin.json`, `~/.codex-aeye-spike/plugin/hooks/hooks.json`, `~/.codex-aeye-spike/plugin/scripts/probe.sh`
- Create (deliverable): `docs/superpowers/spikes/2026-07-12-codex-hook-contract.md`

- [ ] **Step 1: Scaffold a minimal echo hook plugin**

`probe.sh` (chmod +x):
```bash
#!/usr/bin/env bash
{ printf '=== %s ===\n' "$(date -Is)"
  printf 'ARGV: %s\n' "$*"
  printf 'PLUGIN_ROOT=%s CLAUDE_PLUGIN_ROOT=%s PLUGIN_DATA=%s cwd=%s\n' \
    "${PLUGIN_ROOT:-}" "${CLAUDE_PLUGIN_ROOT:-}" "${PLUGIN_DATA:-}" "$(pwd)"
  printf 'STDIN:\n'; cat
  printf '\n'
} >> /tmp/aeye-hook-probe.log 2>&1
# For SessionStart, also emit an additionalContext to test injection:
if [[ "${1:-}" == sessionstart ]]; then
  jq -nc '{hookSpecificOutput:{hookEventName:"SessionStart",additionalContext:"AEYE_SPIKE_CONTEXT_MARKER"}}'
fi
```
`hooks/hooks.json`:
```json
{ "hooks": {
  "PostToolUse": [ { "hooks": [ { "type": "command", "command": "\"$PLUGIN_ROOT\"/scripts/probe.sh posttooluse" } ] } ],
  "SessionStart": [ { "hooks": [ { "type": "command", "command": "\"$PLUGIN_ROOT\"/scripts/probe.sh sessionstart" } ] } ]
} }
```
`plugin.json`: minimal valid manifest (`name`, `version:"0.0.1"`, `description`, `author.name`, `interface{displayName,shortDescription,longDescription,developerName,category}`), no `hooks` field.

- [ ] **Step 2: Install + trust the spike plugin**

Run (record exact commands + prompts in the deliverable):
```bash
codex plugin marketplace add ~/.codex-aeye-spike   # or the marketplace.json flow the tooling requires
codex plugin add aeye-spike
```
Note whether a trust prompt appears and what it asks.

- [ ] **Step 3: Drive a real Codex turn that touches tools**

In a throwaway dir, run a Codex session that (a) writes a file via `apply_patch`, (b) views an image via `view_image` (point it at an existing PNG), (c) runs a `Bash`/`exec_command`. Then inspect:
```bash
cat /tmp/aeye-hook-probe.log
```

- [ ] **Step 4: Record the confirmed contract (the deliverable)**

In `docs/superpowers/spikes/2026-07-12-codex-hook-contract.md`, document verbatim from the probe log:
- Whether PostToolUse and SessionStart fired at all (PASS/FAIL).
- The exact stdin JSON keys present (confirm/deny `session_id`, `cwd`, `transcript_path`, `hook_event_name`, `tool_name`, `tool_input`, `tool_response`, `permission_mode`, `turn_id`).
- **Critical:** for an `exec`-encoded call that ran `tools.apply_patch(...)` / `tools.view_image(...)`, what does `tool_name` report and what is the shape of `tool_input`? (Raw JS program string? A parsed sub-call object? This decides extraction difficulty — see Task 2.3.)
- The env vars actually set (`PLUGIN_ROOT` etc.) and the command working directory.
- Whether the `SessionStart` `additionalContext` marker reached the model, and whether `PostToolUse` `additionalContext` is honored.
- SessionStart `source` values observed (startup/resume/…).

- [ ] **Step 5: GATE decision**

If PostToolUse did NOT fire → **STOP**. Update issue #122 + the spec with the finding; the adapter is not buildable against 0.144.1. Do not proceed to Phase 1.
If it fired → record the confirmed contract; Phase 1+ tasks consume it. Note any field-name deltas from the spec's assumptions so extraction tasks can adjust their payload-access lines.

- [ ] **Step 6: Clean up**

```bash
codex plugin remove aeye-spike
codex plugin marketplace remove aeye-spike || true
gtrash put ~/.codex-aeye-spike
git add docs/superpowers/spikes/2026-07-12-codex-hook-contract.md
git commit -m "docs(spike): confirm Codex hook runtime contract (#122)"
```

### Task 0.2: Spike — declarative install + hook-trust for Nix

**Why:** Codex plugin install is stateful (copy into `~/.codex/plugins/cache/…` keyed by a cachebuster + persisted hook-trust), unlike `claude --plugin-dir`. This decides the packaging deliverable (Phase 4). Not a hard build gate, but resolve before Phase 4.

**Files:** Append findings to `docs/superpowers/spikes/2026-07-12-codex-hook-contract.md` (a `## Install & trust` section).

- [ ] **Step 1: Map the install mechanics**

Inspect `~/.codex/skills/.system/plugin-creator/references/installing-and-updating.md` and run `codex plugin marketplace --help`, `codex plugin add --help`. Record: where the plugin is copied, the cachebuster role, and whether a `/nix/store` (read-only) source path is usable directly or must be copied.

- [ ] **Step 2: Determine non-interactive trust**

Establish whether hook-trust can be granted without an interactive prompt (a config key in `~/.codex/config.toml`? a `codex plugin add` flag? a trust-store file Nix can write?). `--dangerously-bypass-hook-trust` is NOT an acceptable production answer — it defeats the trust model.

- [ ] **Step 3: Record the packaging decision**

Document the chosen approach OR the degraded fallback: Nix writes the marketplace file + `AEYE_D2_*` env, and the user runs a one-time `codex plugin add` + trust prompt (documented in the plugin README). Commit:
```bash
git commit -am "docs(spike): Codex plugin install + hook-trust for Nix (#122)"
```

---

## Phase 1 — Agent-agnostic core refactor (Claude stays green)

Pure refactor of working code; the Claude test suite is the safety net. Unlocked by Spike 1 PASS (no point refactoring for a second adapter that can't exist).

### Task 1.1: Extract render/fs helpers + output scanner into `adapters/core/manifest-extract.sh`

**Files:**
- Create: `adapters/core/manifest-extract.sh`
- Modify: `adapters/claude-code/plugin/scripts/lib/manifest-extract.sh`
- Test: `tests/` (existing bats/go covering extraction — identify with `ls tests/`)

**Interfaces:**
- Produces (sourced by both adapters): `_mtime`, `_manifest_lock`, `d2_png_for`, `_d2_render_fail`, `d2_rm_render_set`, `d2_render`, and `scan_response_image_path PAYLOAD` (the agent-agnostic Phase-2 `tool_response` scan, renamed from the back half of `extract_image_path`).
- Consumes: nothing (leaf library).

- [ ] **Step 1: Read the current lib and existing tests**

Run: `cat adapters/claude-code/plugin/scripts/lib/manifest-extract.sh` and `ls tests/ && sed -n '1,60p' tests/*.bats 2>/dev/null`. Identify which tests exercise `extract_image_path` Phase-2 and `d2_render`.

- [ ] **Step 2: Move the agent-agnostic functions verbatim**

Create `adapters/core/manifest-extract.sh` containing (moved unchanged): `_mtime`, `_manifest_lock`, `d2_png_for`, `_d2_render_fail`, `d2_rm_render_set`, `d2_render`. Add a new `scan_response_image_path PAYLOAD` holding the current Phase-2 jq scan of `.tool_response` (lines 62-76 of the current lib) plus the `resolve`/`is_ext` helpers it needs.

- [ ] **Step 3: Repoint the Claude lib to source core + keep Claude's Phase-1**

Rewrite `adapters/claude-code/plugin/scripts/lib/manifest-extract.sh` to `source "$(dirname "${BASH_SOURCE[0]}")/../../../../core/manifest-extract.sh"` (verify the relative depth) and define only Claude's `extract_image_path` (Phase-1 `tool_input.file_path/path/output_path`, then delegate to `scan_response_image_path`) and `extract_d2_path`.

- [ ] **Step 4: Run the Claude adapter test suite**

Run the repo's test command (check `justfile`: likely `just test` or `bats tests/`). Expected: PASS, unchanged.

- [ ] **Step 5: Commit**

```bash
git add adapters/core/manifest-extract.sh adapters/claude-code/plugin/scripts/lib/manifest-extract.sh
git commit -m "refactor(adapters): extract agent-agnostic core from manifest-extract (#122)"
```

### Task 1.2: Extract the manifest lifecycle into `adapters/core/manifest-lifecycle.sh`

**Files:**
- Create: `adapters/core/manifest-lifecycle.sh`
- Modify: `adapters/claude-code/plugin/scripts/{images,diagrams,session-reset,session-backfill}.sh`
- Test: existing `tests/` for reset/GC/backfill

**Interfaces:**
- Produces: `resolve_state_dirs` (sets `STATE_DIR`/`IMAGES_DIR`/`DIAGRAMS_DIR`), `manifest_paths PANE_FILE`, `owner_selfheal PANE_FILE SESSION_ID`, `append_image_line MANIFEST PATH SOURCE TS`, `append_diagram_line MANIFEST PNG SVG NAME TS`, `gc_sweep PANE_FILE LIVE_PANES`, and the pane-keying validator `valid_pane_file PANE`.
- Consumes: `_mtime`, `_manifest_lock` from `manifest-extract.sh`.

- [ ] **Step 1: Identify the duplicated lifecycle blocks**

Read `images.sh`, `diagrams.sh`, `session-reset.sh`, `session-backfill.sh`. Mark the identical blocks: state-dir resolution, pane-file validation regex `^[A-Za-z0-9_@:.-]+$`, owner self-heal (images.sh:43-49, diagrams.sh:65-71), the `jq -nc … {type:"image",…}` append lines, and the GC sweep (session-reset.sh:66-99).

- [ ] **Step 2: Write core lifecycle functions**

Create `adapters/core/manifest-lifecycle.sh` with the functions above, taking the session id as a **parameter** (not reading `$CLAUDE_CODE_SESSION_ID` directly — that becomes the shim's job). `owner_selfheal` drops a manifest whose `.owner` != the passed session id, then stamps it.

- [ ] **Step 3: Repoint the Claude scripts**

Edit the four Claude scripts to source core + pass `"$CLAUDE_CODE_SESSION_ID"` into the lifecycle functions. Behavior identical.

- [ ] **Step 4: Run the full Claude suite**

Run the test command. Expected: PASS, unchanged. Manually spot-check reset GC with a stale manifest fixture if the suite doesn't cover it.

- [ ] **Step 5: Commit**

```bash
git add adapters/core/manifest-lifecycle.sh adapters/claude-code/plugin/scripts/
git commit -m "refactor(adapters): extract manifest lifecycle into core (#122)"
```

---

## Phase 2 — Codex adapter (Spike 0.1 PASSED — contract confirmed)

> **Contract confirmed** by `docs/superpowers/spikes/2026-07-12-codex-hook-contract.md`: the hook payload is NORMALIZED. `tool_name` ∈ {`apply_patch`,`view_image`,`Bash`,`mcp__*`}; `apply_patch` → `tool_input.command` (clean patch envelope), `view_image` → `tool_input.path`, `Bash` → `tool_input.command`. **No JS-unwrap on the hook path** — that is backfill-only (Phase 3). Session id/cwd/transcript from the payload (`session_id`,`cwd`,`transcript_path`).

### Task 2.1: Codex plugin manifest, marketplace entry, hooks.json

**Files:**
- Create: `adapters/codex/plugin/.codex-plugin/plugin.json`, `adapters/codex/plugin/marketplace.json`, `adapters/codex/plugin/hooks/hooks.json`

**Interfaces:**
- Produces: the plugin skeleton the hook scripts (Task 2.2+) attach to.

- [ ] **Step 1: Write `plugin.json`** — `name:"aeye"`, `version:"0.9.0"`, `description`, `author.name:"Noam Stolero"`, and an `interface` block (`displayName:"aeye"`, `shortDescription`, `longDescription`, `developerName`, `category:"Productivity"`, `capabilities:["Read","Write"]`). No `hooks` field.

- [ ] **Step 2: Write `marketplace.json`** — root `{name, interface.displayName, plugins:[…]}` with one entry: `name:"aeye"`, `source:{source:"local", path:"./"}`, `policy:{installation:"AVAILABLE", authentication:"ON_INSTALL"}`, `category:"Productivity"`. (Confirm the `path` convention against the Task 0.2 findings.)

- [ ] **Step 3: Write `hooks/hooks.json`** — mirror the Claude `hooks.json` structure with the confirmed clean Codex matchers (NOT `exec` — the hook payload is normalized):
```json
{ "hooks": {
  "PostToolUse": [
    { "matcher": "apply_patch|view_image|Bash", "hooks": [ { "type": "command", "command": "\"$PLUGIN_ROOT\"/scripts/images.sh" } ] },
    { "matcher": "apply_patch", "hooks": [ { "type": "command", "command": "\"$PLUGIN_ROOT\"/scripts/diagrams.sh" } ] }
  ],
  "SessionStart": [
    { "hooks": [ { "type": "command", "command": "\"$PLUGIN_ROOT\"/scripts/diagram-guidance.sh" } ] },
    { "hooks": [ { "type": "command", "command": "\"$PLUGIN_ROOT\"/scripts/session-reset.sh" } ] },
    { "hooks": [ { "type": "command", "command": "\"$PLUGIN_ROOT\"/scripts/session-backfill.sh" } ] }
  ]
} }
```

- [ ] **Step 4: Validate + commit**

Run `python3 ~/.codex/skills/.system/plugin-creator/scripts/validate_plugin.py adapters/codex/plugin` — expect PASS.
```bash
git add adapters/codex/plugin/.codex-plugin adapters/codex/plugin/marketplace.json adapters/codex/plugin/hooks
git commit -m "feat(codex): plugin manifest, marketplace entry, hooks.json (#122)"
```

### Task 2.2: Codex shim — session id + touched-path extraction

**Files:**
- Create: `adapters/codex/plugin/scripts/lib/shim.sh`
- Test: `tests/codex/extract.bats` (new), `tests/fixtures/codex/*.json` (new)

**Interfaces:**
- Consumes: `scan_response_image_path`, `_mtime` from core; the confirmed hook contract (normalized `tool_name`/`tool_input`).
- Produces: `codex_session_id PAYLOAD` (echoes `.session_id`), `codex_extract_touched_paths PAYLOAD` (echoes newline-separated existing image/`.d2` paths this call wrote or viewed), and internal `_codex_apply_patch_paths ENVELOPE`.

- [ ] **Step 1: Write failing tests**

`tests/codex/extract.bats` — three synthetic hook payloads (real shapes from the spike):
- `{"tool_name":"apply_patch","tool_input":{"command":"*** Begin Patch\n*** Add File: docs/foo.d2\n+x\n*** Update File: /abs/bar.png\n*** End Patch"},"cwd":"/repo"}` → echoes `/repo/docs/foo.d2` and `/abs/bar.png` (for the ones that exist as fixtures);
- `{"tool_name":"view_image","tool_input":{"path":"/abs/shot.png"},"cwd":"/repo"}` → echoes `/abs/shot.png`;
- an `apply_patch` touching `README.md` → NOT echoed (non-image).
Fixtures under `tests/fixtures/codex/`.

- [ ] **Step 2: Run → fail** (`shim.sh` functions undefined).

- [ ] **Step 3: Implement the envelope parser**

```bash
# _codex_apply_patch_paths ENVELOPE -> echo Add/Update File paths (one per line)
_codex_apply_patch_paths() {
	sed -n 's/^\*\*\* \(Add\|Update\) File: //p' <<<"$1"
}
```

- [ ] **Step 4: Implement `codex_extract_touched_paths`**

The hook payload is normalized (confirmed by the spike), so this is a clean
branch on `tool_name` — no JS unwrap:

```bash
codex_extract_touched_paths() {
	local payload="$1" cwd name p resolved
	cwd="$(jq -r '.cwd // empty' <<<"$payload" 2>/dev/null)"
	name="$(jq -r '.tool_name // empty' <<<"$payload" 2>/dev/null)"

	emit() { # $1 raw path -> resolve, filter, existence-check, print
		local q="$1"
		[[ -z $q ]] && return 0
		[[ $q != /* && -n $cwd ]] && q="$cwd/$q"
		[[ ${q,,} =~ \.(png|jpe?g|gif|webp|bmp|d2)$ ]] || return 0
		[[ -f $q ]] || return 0
		printf '%s\n' "$q"
	}

	case "$name" in
	apply_patch)
		local env; env="$(jq -r '.tool_input.command // empty' <<<"$payload" 2>/dev/null)"
		while IFS= read -r p; do emit "$p"; done < <(_codex_apply_patch_paths "$env")
		;;
	view_image)
		emit "$(jq -r '.tool_input.path // empty' <<<"$payload" 2>/dev/null)"
		;;
	esac

	# screenshots embedded in tool output (Bash/MCP) — shared scanner.
	scan_response_image_path "$payload"
}
codex_session_id() { jq -r '.session_id // empty' <<<"$1" 2>/dev/null; }
```

- [ ] **Step 5: Run → pass.** Add a case with a `Bash` tool whose `tool_response` embeds a screenshot path → captured via `scan_response_image_path`.

- [ ] **Step 6: Commit**

```bash
git add adapters/codex/plugin/scripts/lib/shim.sh tests/codex tests/fixtures/codex
git commit -m "feat(codex): shim for session id + normalized-payload path extraction (#122)"
```

### Task 2.3: Codex `images.sh` + `diagrams.sh` capture hooks

**Files:**
- Create: `adapters/codex/plugin/scripts/images.sh`, `adapters/codex/plugin/scripts/diagrams.sh`
- Test: `tests/codex/images.bats`

**Interfaces:**
- Consumes: core lifecycle + `codex_session_id`, `codex_extract_touched_paths`; for diagrams also `extract_d2_path`-equivalent (a `.d2` from `codex_extract_touched_paths` filtered to `.d2`).

- [ ] **Step 1: Write failing test** — pipe a synthetic `apply_patch`-of-a-PNG payload into `images.sh` with `TMUX_PANE=%1` set; assert one `{type:"image",path:…}` line appended to the manifest.

- [ ] **Step 2: Run → fail.**

- [ ] **Step 3: Implement `images.sh`** — port the Claude `images.sh` structure: resolve state dirs, key by `${TMUX_PANE:-$(codex_session_id "$payload")}`, `owner_selfheal`, then for each path from `codex_extract_touched_paths` that is NOT `.d2`, `append_image_line`. Source core + shim via `"$PLUGIN_ROOT"`.

- [ ] **Step 4: Implement `diagrams.sh`** — for each `.d2` path from `codex_extract_touched_paths`: `d2_render` both themes, the `<foreignObject>` markdown-blank suppression + `additionalContext` warning (ported verbatim — gated on the 0.1 finding that PostToolUse honors it), prune superseded renders, `append_diagram_line`, and `--ensure-open` the toggle.

- [ ] **Step 5: Run → pass.**

- [ ] **Step 6: Commit** `feat(codex): images + diagrams PostToolUse capture hooks (#122)`.

### Task 2.4: Codex `session-reset.sh` + `diagram-guidance.sh` + skills

**Files:**
- Create: `adapters/codex/plugin/scripts/session-reset.sh`, `adapters/codex/plugin/scripts/diagram-guidance.sh`, `adapters/codex/plugin/skills/{diagrams,image-gallery}/SKILL.md`
- Test: `tests/codex/session-reset.bats`

- [ ] **Step 1: Write failing test** — SessionStart `source:"startup"` payload with a foreign `.owner` → manifest cleared + owner restamped with `codex_session_id`.

- [ ] **Step 2: Run → fail.**

- [ ] **Step 3: Port `session-reset.sh`** — identical logic to Claude's, but session id from `codex_session_id "$payload"` (payload, not env) and `source` from `.source`. GC sweep reused from core.

- [ ] **Step 4: Port `diagram-guidance.sh`** — host-gate (`$TMUX`/`$KITTY_LISTEN_ON`), the `aeye`/`resvg` PATH preflight, and the guidance text via `hookSpecificOutput.additionalContext`. Copy the two `SKILL.md` files from the Claude adapter (they're agent-neutral prose; adjust only host-detection references).

- [ ] **Step 5: Run → pass.**

- [ ] **Step 6: Commit** `feat(codex): session-reset, diagram-guidance, skills (#122)`.

---

## Phase 3 — Codex resume backfill (unlocked by Spike 1 PASS)

### Task 3.1: Rollout transcript iterator + `session-backfill.sh`

**Files:**
- Create: `adapters/codex/plugin/scripts/session-backfill.sh`
- Test: `tests/codex/backfill.bats`, `tests/fixtures/codex/rollout-*.jsonl` (real fixtures trimmed from `~/.codex/sessions`)

**Interfaces:**
- Consumes: `codex_extract_touched_paths` (fed synthetic payloads), core `append_*`, `_manifest_lock`.

- [ ] **Step 1: Capture real rollout fixtures** — trim 3 fixtures from `~/.codex/sessions` covering: `custom_tool_call`/`apply_patch` raw; `custom_tool_call`/`exec` JS embedding `apply_patch` + `view_image`; `function_call`/`exec_command` with `arguments`.

- [ ] **Step 2: Write failing test** — replay each fixture; assert the manifest contains the expected image/diagram entries and nothing from unrelated lines.

- [ ] **Step 3: Run → fail.**

- [ ] **Step 4: Implement the iterator** — port Claude's `session-backfill.sh` skeleton (resume-only, authoritative rebuild under lock, `seen` dedup). The transcript stores the RAW `exec`/JS transport (unlike the normalized hook payload), so the reader must normalize each record into the clean `{tool_name, tool_input}` shape `codex_extract_touched_paths` expects, then feed it through. Per `.jsonl` line, from `.payload`:
  - `custom_tool_call` / `name=="exec"` / JS `input` → **unwrap** (see helper below): a `tools.apply_patch("…")` call → `{tool_name:"apply_patch", tool_input:{command:<decoded envelope>}}`; a `tools.view_image({path:…})` call → `{tool_name:"view_image", tool_input:{path:<path>}}`.
  - `custom_tool_call` / `name=="apply_patch"` / raw envelope `input` (older) → `{tool_name:"apply_patch", tool_input:{command:.input}}`.
  - `function_call` / `name=="exec_command"` / `arguments` (older) → shell; scan its paired output record for screenshot paths.
  - tool-output records (e.g. `function_call_output`) → `{tool_response:<output>}` for the shared scanner.
  `cwd` from the `turn_context`/`session_meta` record (confirm the field name in the fixtures).

  Unwrap helper (handles the JS transport — note `apply_patch` arg is a JSON-escaped string, `view_image` uses an **unquoted** JS key `path:`):
```bash
# _codex_unwrap_apply_patch JS -> decoded patch envelope (empty if none)
_codex_unwrap_apply_patch() {
	local s; s="$(grep -oE 'tools\.apply_patch\("([^"\\]|\\.)*"' <<<"$1" | sed -E 's/^tools\.apply_patch\(//')"
	[[ -n $s ]] && jq -r . <<<"$s" 2>/dev/null
}
# _codex_unwrap_view_image JS -> path arg (quoted or unquoted key)
_codex_unwrap_view_image() {
	grep -oE 'tools\.view_image\(\{[[:space:]]*"?path"?[[:space:]]*:[[:space:]]*"[^"]+"' <<<"$1" \
		| grep -oE '"[^"]+"$' | tr -d '"'
}
```

- [ ] **Step 5: Run → pass.**

- [ ] **Step 6: Commit** `feat(codex): resume backfill from rollout transcript (#122)`.

---

## Phase 4 — Packaging (unlocked by Spike 0.2)

### Task 4.1: nix-config — `AEYE_D2_*` env + Codex plugin install

**Files (in the nix-config repo, separate worktree):**
- Modify: `home/ai/codex/default.nix`

- [ ] **Step 1: Export the diagram env for Codex sessions** — add `AEYE_D2_FONT`, `AEYE_D2_FONT_DIR` (`${pkgs.source-sans}/share/fonts/truetype`), and ensure `aeye`/`resvg` are on the Codex session PATH, mirroring the Claude wrapper in `home/ai/claude-code/default.nix:166-176`. Set `AEYE_D2_THEME` if the Codex launch path has a light/dark signal.

- [ ] **Step 2: Wire the plugin install** — implement the Task 0.2 decision: generate the marketplace file pointing at `inputs.…aeye/adapters/codex/plugin` and install+trust it declaratively, OR (fallback) write the marketplace file + document the one-time `codex plugin add` in the plugin README.

- [ ] **Step 3: Rebuild + verify** — `nh home switch` (or the repo's justfile wrapper), then start a Codex session and confirm `env | grep AEYE_D2` and `codex plugin list` shows aeye.

- [ ] **Step 4: Commit** in nix-config on its own branch; PR separately.

### Task 4.2: End-to-end verification + docs

- [ ] **Step 1: Manual E2E** — a Codex session that (a) `apply_patch`es a `.d2`, (b) `view_image`s an existing PNG, (c) screenshots via Bash/MCP. Confirm all three appear in the carousel; SessionStart guidance shows; markdown-blank warning fires on a bad `.d2`.
- [ ] **Step 2: Update README** — add Codex to the supported agents / install section.
- [ ] **Step 3: Update `CHANGELOG.md`** per the repo's release-please conventions.
- [ ] **Step 4: Commit + open PR** `--assignee @me`, linking #122.

---

## Self-Review

**Spec coverage:** manifest/marketplace/hooks → 2.1; dual-encoding + view_image extraction → 2.2; capture hooks → 2.3; reset/guidance/skills → 2.4; backfill across record shapes → 3.1; core refactor + Claude regression → 1.1/1.2; nix env + install → 4.1; hook-runtime + trust risks → 0.1/0.2; marketplace/nix-store conflict → 0.2; PostToolUse `additionalContext` → 0.1 + 2.3. All spec sections mapped.

**Placeholders:** Extraction/parse steps carry real code; the one deliberately-deferred seam (outer hook-payload keys) is explicitly flagged as resolved by Task 0.1, not left vague.

**Type consistency:** `codex_extract_touched_paths` / `codex_session_id` / `scan_response_image_path` / `owner_selfheal` / `append_image_line` / `append_diagram_line` used consistently across Tasks 1.1–3.1.
