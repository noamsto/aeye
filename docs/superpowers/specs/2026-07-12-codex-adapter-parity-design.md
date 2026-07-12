# Codex CLI adapter at parity with the Claude Code plugin

Date: 2026-07-12
Tracking issue: [#122](https://github.com/noamsto/aeye/issues/122)
Revision: rev2 — corrected after an adversarial spec review against the local
`codex-cli 0.144.1` rollouts (the "one divergence" framing in rev1 was wrong;
see [Codex tool encodings](#codex-tool-encodings-the-real-work) and
[Open risks](#open-risks-gate-the-plan)).

## Goal

Bring OpenAI Codex CLI to parity with the existing `adapters/claude-code/`
plugin: a Codex session captures the images and diagrams it touches — writes,
screenshots, **and image views** — into the aeye carousel, with the same
SessionStart guidance, resume backfill, GC, and carousel auto-open the Claude
adapter provides today.

"Parity" is bounded by what the Codex hook runtime actually exposes in the
installed version; see [Open risks](#open-risks-gate-the-plan) for the two
unknowns that can shrink it, each gated by a spike before build.

## Background

The `2026-07-12-codex-lazytmux-nix-config-audit.md` audit concluded the repo is
Claude-only. This spec plans the fix, grounded in the local `codex-cli 0.144.1`
plugin/hook system and its real session rollouts.

### What is verified locally vs. documented

Kept honest because the adapter is worthless if the hook runtime doesn't fire.

**Verified against the local install (files cited):**

- Plugin system is live: `codex plugin {add,list,marketplace,remove}` subcommands
  exist; `.codex-plugin/plugin.json` + `hooks/hooks.json` are the layout
  (`~/.codex/skills/.system/plugin-creator/`, and real manifests under
  `~/.codex/.tmp/plugins/plugins/*/`).
- A hook-trust model exists: `codex --dangerously-bypass-hook-trust`
  ("Run enabled hooks without requiring persisted hook trust").
- `hooks.json` shape (real examples, `~/.codex/.tmp/plugins/plugins/{figma,replayio}/hooks.json`):
  `{"hooks":{"PostToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./scripts/…"}]}]}}`,
  plus a `Stop` event. Matchers seen: `Bash`, `Write|Edit`.
- Tool call encodings and the `apply_patch`/`view_image`/`exec_command` payloads
  (cited in [Codex tool encodings](#codex-tool-encodings-the-real-work)).

**Documented (official Codex hooks docs, `learn.chatgpt.com/docs/hooks`) but NOT
yet empirically confirmed in 0.144.1 — first spike proves or disproves:**

- That `PostToolUse` and `SessionStart` command hooks actually fire, and the
  stdin payload carries `session_id`, `cwd`, `transcript_path`, `hook_event_name`,
  `permission_mode`, `turn_id`.
- SessionStart `source` ∈ {`startup`,`resume`,`clear`,`compact`}.
- Env vars `PLUGIN_ROOT`/`PLUGIN_DATA` (+ `CLAUDE_PLUGIN_ROOT` compat); command
  cwd = session cwd.
- That `PostToolUse` honors `hookSpecificOutput.additionalContext` (the Claude
  `diagrams.sh` markdown-blank warning depends on this).

The one local hook *script* example is explicitly headed "Draft hook example for
**future** plugin hook runtimes" — hence the empirical gate before any build.

## Codex tool encodings (the real work)

Codex 0.144.1 emits tool calls in **two encodings**, and the adapter must handle
both (which the model uses depends on mode/model):

1. **Legacy direct call** — `payload.type == "custom_tool_call"`,
   `payload.name == "apply_patch"`, `payload.input` == a raw patch envelope:
   ```
   *** Begin Patch
   *** Add File: docs/foo.d2
   +<content>
   *** End Patch
   ```
   (`~/.codex/sessions/2026/07/12/…019f5683….jsonl`)

2. **Unified `exec` tool** — `payload.name == "exec"`, `payload.input` == a **JS
   program** that calls tool primitives, with arguments as escaped JS string
   literals:
   - write: `text(await tools.apply_patch("*** Begin Patch\n*** Update File: /abs/path\n…*** End Patch"));`
   - view/read: `const r = await tools.view_image({"path":"/tmp/foo.png","detail":"original"}); image(r.image_url);`
     (`~/.codex/sessions/2026/07/12/…019f5675….jsonl`)
   - shell: `await tools.exec_command({"cmd":"…","workdir":"/abs"});`
     (`~/.codex/sessions/2026/07/12/…019f5682….jsonl`)

Consequences that rev1 got wrong:

- A `matcher` of `apply_patch`/`Write|Edit` will **not** match encoding #2 (tool
  name is `exec`). The PostToolUse matcher must include `exec`, and the extractor
  must detect the JS wrapper, locate the embedded `tools.apply_patch(...)` /
  `tools.view_image(...)` call, and unescape its string/JSON argument before
  parsing.
- Observed written/viewed paths are **absolute**, not cwd-relative (rev1 assumed
  cwd-relative). Resolve-against-cwd still applies as a fallback for relative
  paths.
- **`view_image` is a first-class capture source** — the parity-equivalent of
  Claude reading a PNG. Omitting it is a direct Goal miss.

## Parity map

| Piece | Claude adapter | Codex equivalent | Gap |
|---|---|---|---|
| Manifest | `.claude-plugin/plugin.json` | `.codex-plugin/plugin.json` (+ required `interface`) | dir + interface |
| Hooks file | `hooks/hooks.json` | `hooks/hooks.json` (auto-discovered) | none |
| Plugin root env | `${CLAUDE_PLUGIN_ROOT}` | `${PLUGIN_ROOT}` (+ compat) | rename |
| Write capture | `tool_input.file_path` | `apply_patch` (direct **and** JS-exec-wrapped) | parse envelope + unwrap JS |
| View/read capture | Read of a PNG (`tool_input.file_path`) | `view_image({path})` (JS-exec-wrapped) | new extractor |
| Screenshot capture | `tool_response` scan | `tool_response` / exec output scan | ports; confirm output shape |
| Tool-name matcher | Read/Write/Edit/MCP | `exec`, `apply_patch`, `Bash`, `mcp__*` | must include `exec` |
| `SessionStart` | source + `additionalContext` | documented identical | confirm empirically |
| Session id | `CLAUDE_CODE_SESSION_ID` (env) | `session_id` (payload only) | thread payload→sites |
| Transcript replay | `message.content[].{tool_use,tool_result}` | 3+ rollout record shapes ↓ | broader iterator |

## Architecture — agent-agnostic core + per-agent shim

`lib/manifest-extract.sh` is already largely agent-agnostic. Split along that seam.

### `adapters/core/` (shared)

- **Render + fs helpers**: `_mtime`, `_manifest_lock`, `d2_render`, `d2_png_for`,
  `_d2_render_fail`, `d2_rm_render_set` — moved verbatim.
- **`tool_response`/output image scanner** (current Phase-2) — agent-agnostic.
- **Manifest lifecycle**: keying + owner self-heal, append, GC sweep, resume
  backfill loop — parameterized by shim callbacks.

### Per-agent shim contract

The shim is wider than rev1's "3 functions." It provides:

1. **`session_id`** — Claude: `$CLAUDE_CODE_SESSION_ID`. Codex: `.session_id`
   from the payload, threaded to the owner-stamp/self-heal call sites in
   `images.sh`/`diagrams.sh`/`session-reset.sh` (these read the env directly
   today — this is a real refactor, not a rename).
2. **`extract_touched_paths PAYLOAD`** — returns image/`.d2` paths this tool call
   wrote **or viewed**. Claude: `tool_input.{file_path,path,output_path}`. Codex:
   - `apply_patch` direct → parse `*** Add/Update File:` lines;
   - `exec` JS → unwrap embedded `tools.apply_patch(...)` (→ same parse) and
     `tools.view_image({path})` (→ the path arg);
   - fall through to the shared output scanner for screenshots.
3. **transcript record iterator** — yields synthetic payloads from the agent's
   transcript. Codex rollout shapes the iterator MUST cover:
   - `custom_tool_call` / `name:apply_patch` / raw `input`;
   - `custom_tool_call` / `name:exec` / JS `input` (embeds apply_patch/view_image);
   - `function_call` / `name:exec_command` / `arguments` (JSON string, older shape);
   - tool-output records (`function_call_output` / exec output) for screenshot
     paths — the Codex analogue of Claude's `tool_result` branch.

### Directory layout after the change

```
adapters/
  core/
    manifest-extract.sh        # render/fs helpers + output scanner
    manifest-lifecycle.sh      # keying, append, GC, backfill (callback-driven)
  claude-code/plugin/          # existing, now sources ../../core
    .claude-plugin/plugin.json
    hooks/hooks.json
    scripts/{images,diagrams,session-reset,session-backfill,diagram-guidance}.sh
    scripts/lib/shim.sh        # Claude shim
    skills/{diagrams,image-gallery}/
  codex/plugin/                # NEW, sources ../../core
    .codex-plugin/plugin.json
    marketplace.json
    hooks/hooks.json           # matchers: exec|apply_patch|Bash|mcp__* ; SessionStart ; Stop
    scripts/{images,diagrams,session-reset,session-backfill,diagram-guidance}.sh
    scripts/lib/shim.sh        # Codex shim (JS-exec unwrap, view_image, rollout iterator)
    skills/{diagrams,image-gallery}/
```

## Capture sources for Codex

| Source | Mechanism |
|---|---|
| Agent writes `.d2` / image | `apply_patch` (direct or JS-exec-wrapped) → envelope parser |
| Agent views an existing image | `view_image({path})` (JS-exec-wrapped) → path arg |
| Screenshot from Bash / MCP | tool output scan (ported; confirm output record shape) |
| Resume backfill | rollout `.jsonl` replay across all record shapes above |

## Packaging (nix-config)

`home/ai/codex/default.nix` today installs `pkgs.codex` + a worker MCP profile
only. Two gaps:

1. **Missing `AEYE_D2_*` env.** The Claude wrapper exports `AEYE_D2_FONT`,
   `AEYE_D2_FONT_DIR`, `AEYE_D2_THEME`, and puts `aeye`/`resvg` on PATH. Codex is
   launched directly, so its session env lacks these and diagrams silently fail
   to render. Fix: export them for Codex sessions.
2. **No plugin install path** — see the install risk below.

## Open risks (gate the plan)

Two unknowns are resolved by spikes **before** committing to build, because each
can shrink the Goal or the packaging deliverable:

1. **Hook runtime actually fires (blocking, spike #1).** Install a trivial
   echo-to-file `PostToolUse` + `SessionStart` hook plugin, run a real Codex
   turn, and confirm (a) both fire, (b) the stdin payload carries the documented
   fields, (c) `exec`/`apply_patch` matchers behave as expected, (d) PostToolUse
   `additionalContext` is honored. If hooks don't fire in 0.144.1, the adapter is
   **not buildable now** — stop and report, don't build against docs.
2. **Declarative install + hook-trust (spike #2).** Codex plugin install is
   stateful (copy into `~/.codex/plugins/cache/…` keyed by a cachebuster +
   persisted hook-trust), unlike `claude --plugin-dir`. A read-only `/nix/store`
   plugin source vs. the mutate-in-place cachebuster reinstall loop may conflict.
   `--dangerously-bypass-hook-trust` is rejected (it's for vetted automation
   only, and defeats the trust model). **The packaging deliverable is conditional
   on this spike:** if no non-interactive trust mechanism exists, the auto-install
   deliverable degrades to a documented manual `codex plugin marketplace add` +
   `codex plugin add` + one-time trust prompt, and nix only provides the
   marketplace file + `AEYE_D2_*` env.

## Testing

- Unit/bats: port `tests/` for the Codex shim — extraction for **both** tool
  encodings (direct `apply_patch`, JS-exec-wrapped `apply_patch`/`view_image`),
  absolute + relative paths, non-image files ignored, session keying via
  `session_id`, and backfill replay against **real rollout `.jsonl` fixtures**
  captured from `~/.codex/sessions` covering all record shapes.
- Regression: re-run the full Claude adapter suite after the core refactor — must
  pass unchanged.
- Manual end-to-end: a Codex session that (a) `apply_patch`es a `.d2`, (b)
  `view_image`s an existing PNG, (c) takes a screenshot via Bash/MCP — confirm
  all three land in the carousel, plus SessionStart guidance and the
  markdown-blank warning appear.

## Out of scope

- Other agent adapters (Gemini CLI, aider). The `adapters/core/` split makes them
  cheaper later, but none are built here.
- Any change to the aeye viewer/carousel — it consumes the manifest unchanged.
