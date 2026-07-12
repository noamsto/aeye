# Codex CLI adapter at parity with the Claude Code plugin

Date: 2026-07-12
Tracking issue: [#122](https://github.com/noamsto/aeye/issues/122)

## Goal

Bring OpenAI Codex CLI to full parity with the existing `adapters/claude-code/`
plugin: a Codex session captures every image and diagram it touches into the
aeye carousel, with the same SessionStart guidance, resume backfill, GC, and
carousel auto-open the Claude adapter provides today.

## Background

The `2026-07-12-codex-lazytmux-nix-config-audit.md` audit concluded the repo is
Claude-only and listed the deferred Codex work. This spec supersedes that audit
with verified facts: the local `codex-cli 0.144.1` install ships a full plugin +
hook system, and its shape is close enough to Claude's that this is a **port with
one real divergence**, not a redesign.

### Verified Codex facts (from the local install + official docs)

Hook payload (stdin JSON) common fields: `session_id`, `cwd`,
`transcript_path` (nullable), `hook_event_name`, `model`, `permission_mode`,
`turn_id` (turn-scoped hooks only).

- **`hooks/hooks.json`** is auto-discovered at plugin root (a `hooks` entry in
  `plugin.json` can override, but the curated-marketplace validator rejects
  `hooks` in the manifest, so we rely on auto-discovery). Same nesting as Claude:
  `{"hooks":{"PostToolUse":[{"matcher":"…","hooks":[{"type":"command","command":"…"}]}]}}`.
- **`PostToolUse`** fires after `Bash`, `apply_patch` (matchers `Write`/`Edit`
  match it too, but `tool_name` reports `apply_patch`), and `mcp__*` tools.
- **`SessionStart`** carries `source` ∈ {`startup`, `resume`, `clear`,
  `compact`} — identical to Claude — and accepts the identical
  `{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"…"}}`
  output shape.
- **Env vars** for command hooks: `PLUGIN_ROOT`, `PLUGIN_DATA`, plus
  `CLAUDE_PLUGIN_ROOT`/`CLAUDE_PLUGIN_DATA` for compatibility. Commands run with
  the session `cwd` as working directory, so scripts must be invoked via
  `"$PLUGIN_ROOT"/…`.
- **`apply_patch` input** is a patch envelope, e.g.:

  ```
  *** Begin Patch
  *** Add File: docs/foo.d2
  +<content>
  *** End Patch
  ```

  Written paths appear on `*** Add File:` / `*** Update File:` lines, relative to
  `cwd`. This is the one field that has no Claude analogue.
- **Transcript** is a rollout `.jsonl` under `~/.codex/sessions/…`; tool calls
  are records with `payload.type == "custom_tool_call"`, `payload.name`, and
  `payload.input` (the patch text for `apply_patch`).

## Parity map

| Piece | Claude adapter | Codex equivalent | Gap |
|---|---|---|---|
| Manifest | `.claude-plugin/plugin.json` | `.codex-plugin/plugin.json` (+ `interface` block) | dir name + required `interface` |
| Hooks file | `hooks/hooks.json` | `hooks/hooks.json` (auto-discovered) | none |
| Plugin root env | `${CLAUDE_PLUGIN_ROOT}` | `${PLUGIN_ROOT}` | rename |
| `PostToolUse` | Read/Write/Edit/MCP | `Bash`, `apply_patch`, `mcp__*` | write-path extraction ↓ |
| `SessionStart` | startup/resume/clear/compact + `additionalContext` | identical | none |
| Session id | `CLAUDE_CODE_SESSION_ID` (env) | `session_id` (payload) | source of the value |
| Transcript | `message.content[].tool_use` | rollout `payload.custom_tool_call` | replay parser |
| Guidance / reset / GC / auto-open | — | ports unchanged | field-name swaps only |

## Architecture — agent-agnostic core + thin shims

`lib/manifest-extract.sh` is already ~90% agent-agnostic. Split along that seam.

### `adapters/core/` (shared, agent-agnostic)

- **Render + fs helpers**: `_mtime`, `_manifest_lock`, `d2_render`, `d2_png_for`,
  `_d2_render_fail`, `d2_rm_render_set` — unchanged, moved verbatim.
- **`tool_response` image scanner** (current Phase-2 of `extract_image_path`) —
  agent-agnostic; both agents embed screenshot paths in tool output.
- **Manifest lifecycle**: keying + owner self-heal, append, GC sweep, and the
  resume-backfill replay loop — parameterized by two shim-provided callbacks:
  *(a) session-id resolver* and *(b) write-path extractor*.

### Per-agent shim contract

Each adapter provides exactly three things over the core:

1. **`extract_write_paths PAYLOAD`** — Claude: `tool_input.{file_path,path,output_path}`.
   Codex: parse the `apply_patch` envelope for `*** Add/Update File:` paths.
2. **`session_id`** — Claude: `$CLAUDE_CODE_SESSION_ID`. Codex: `.session_id` from payload.
3. **transcript record iterator** — Claude: `message.content[].tool_use`/`tool_result`.
   Codex: rollout `payload.custom_tool_call` (+ tool-output records for screenshots).

### Directory layout after the change

```
adapters/
  core/
    manifest-extract.sh        # render/fs helpers + tool_response scanner
    manifest-lifecycle.sh      # keying, append, GC, backfill (callback-driven)
  claude-code/plugin/          # existing, now sources ../../core
    .claude-plugin/plugin.json
    hooks/hooks.json
    scripts/{images,diagrams,session-reset,session-backfill,diagram-guidance}.sh
    scripts/lib/shim.sh        # Claude's 3 shim functions
    skills/{diagrams,image-gallery}/
  codex/plugin/                # NEW, sources ../../core
    .codex-plugin/plugin.json
    marketplace.json
    hooks/hooks.json
    scripts/{images,diagrams,session-reset,session-backfill,diagram-guidance}.sh
    scripts/lib/shim.sh        # Codex's 3 shim functions
    skills/{diagrams,image-gallery}/
```

The per-agent `scripts/*.sh` become thin: source core + shim, then run the shared
lifecycle. The prose in the two skills and `diagram-guidance` is identical except
for host-detection env names.

## Capture sources for Codex

| Source | Mechanism |
|---|---|
| Agent writes `.d2` / image file | `apply_patch` PostToolUse → envelope parser |
| Screenshot from Bash / MCP (e.g. playwright) | `tool_response` scan (core, ported) |
| Resume backfill | replay rollout `.jsonl` `custom_tool_call` records through the same extractors |

## Packaging (nix-config)

`home/ai/codex/default.nix` today installs `pkgs.codex` + a worker MCP profile
only. Two gaps:

1. **Missing `AEYE_D2_*` env.** The Claude wrapper exports `AEYE_D2_FONT`,
   `AEYE_D2_FONT_DIR`, `AEYE_D2_THEME`, and puts `aeye`/`resvg` on PATH. Codex is
   launched directly (no wrapper), so its session env lacks these — diagrams would
   silently fail to render (see the resvg font-dir + diagram-hook-PATH history).
   Fix: export them for Codex sessions (session vars or a thin wrapper).
2. **No plugin install path.** Codex plugin install is stateful (cache under
   `~/.codex/plugins` + persisted **hook-trust**), unlike `claude --plugin-dir`.

### Known risk — resolved in the plan (not here)

The declarative install + hook-trust story is the one genuinely open question.
**The first step of the implementation plan is a spike** to settle it: a
Nix-generated `marketplace.json` pointing at the aeye flake input's
`adapters/codex/plugin`, installed and trusted non-interactively, vs. a launch
flag (`--dangerously-bypass-hook-trust` is explicitly rejected for anything but
vetted automation). The plan will record the chosen mechanism and a fallback.

## Testing

- Unit/bats: port `tests/` for the Codex shim — `apply_patch` envelope
  extraction (add/update/delete, relative paths, non-image files ignored),
  session keying via `session_id`, and backfill replay against a **real rollout
  `.jsonl` fixture** captured from `~/.codex/sessions`.
- Regression: re-run the full Claude adapter test suite after the core refactor —
  it must pass unchanged, proving the extraction moved without behavior change.
- Manual end-to-end: run a Codex session, have it `apply_patch` a `.d2` file and a
  screenshot via an MCP/Bash path, confirm both land in the carousel and the
  SessionStart guidance appears.

## Out of scope

- Other agent adapters (Gemini CLI, aider). The `adapters/core/` split is
  designed to make them cheap later, but none are built here.
- Any change to the aeye viewer/carousel itself — it consumes the manifest
  unchanged.
