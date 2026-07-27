# Cursor Agent adapter at parity with Claude Code / Codex

Date: 2026-07-27
Tracking issue: [#157](https://github.com/noamsto/aeye/issues/157)
Status: Approved design (scope A — full capture + diagrams + skills; Marketplace-ready)

## Goal

Ship a first-class **Cursor Agent** capture adapter under `adapters/cursor/` so a
Cursor session in tmux gets the same carousel loop Claude Code and Codex already
have: auto-capture images the agent reads/writes, render D2 diagrams into the
carousel, SessionStart guidance + reset + resume backfill, and discoverable
`diagrams` / `image-gallery` skills.

The viewer and `docs/MANIFEST.md` contract stay unchanged. New agents = new
adapters; this is the Cursor one.

## Decisions (locked)

| Decision | Choice |
|----------|--------|
| Success bar | **Full parity** with Claude/Codex (not skills-only) |
| Primary runtime | **Cursor Agent / CLI inside tmux** (`TMUX_PANE` keying) |
| Distribution | **Marketplace-ready** from day one + local/`plugins/local` path |
| Architecture | **Thin Cursor shim** over `adapters/core` (Codex pattern), not a Claude fork |
| Browser/MCP screenshots | **Stretch / out of v1** |
| nix-config HM wiring | **Follow-up** after the aeye adapter lands (not a merge gate) |

## Background

Cursor already has Agent Skills discovery and a hook system that is close to
Claude Code:

- Skills load from plugin `skills/` dirs and from flat `~/.cursor/skills/` etc.
- Hooks: `postToolUse`, `sessionStart`, … with JSON stdin/stdout; matchers on
  `Read` / `Write` / `Shell` / `MCP:<tool>`.
- Plugins: `.cursor-plugin/plugin.json` + auto-discovered `hooks/`, `skills/`,
  `scripts/`. Local test: `~/.cursor/plugins/local/<name>/` + Reload Window.
- Marketplace: public Git repo + [publish form](https://cursor.com/marketplace/publish).

What Cursor does **not** do: load Claude Code `--plugin-dir` / marketplace cache
trees. So aeye's Claude plugin never appears in Cursor today — by missing
adapter, not user misconfig. nix-config `programs.agent-skills` is the wrong
seam (third-party flat catalogs, not plugin-owned capture).

### Verified from docs vs empirical gates

**From Cursor docs (hooks + plugins reference):**

- `postToolUse` input includes `tool_name`, `tool_input`, `tool_output`, `cwd`,
  plus common fields `conversation_id` / `session_id`.
- `postToolUse` / `sessionStart` output may include `additional_context`
  (snake_case — not Claude's `hookSpecificOutput.additionalContext`).
- Plugin hooks use relative commands like `./scripts/….sh` from the plugin root.
- Matchers for tool hooks: `Shell`, `Read`, `Write`, `Grep`, `Delete`, `Task`,
  `MCP:<name>`.

**Must prove in a spike before treating backfill / guidance output as done
(same honesty bar as the Codex adapter spec):**

1. Plugin-bundled `hooks/hooks.json` actually fires for Cursor Agent in tmux.
2. `sessionStart` honors returned `additional_context` (diagram guidance depends
   on it).
3. `postToolUse` for `Write` of a `.d2` / `Read` of a `.png` carries a usable
   absolute (or cwd-resolvable) path in `tool_input`.
4. Whether `transcript_path` (or equivalent) exists for resume backfill; if not,
   backfill is best-effort / deferred with an explicit INSTALL note.
5. Env var for plugin root if any (`PLUGIN_ROOT` vs none — scripts must
   `BASH_SOURCE`-resolve either way).

## Architecture

### Layout

Mirror Codex packaging (marketplace under the adapter dir, not Claude's repo-root
`.claude-plugin/`):

```text
adapters/cursor/
  README.md
  .cursor-plugin/
    marketplace.json          # Marketplace root for this adapter
  plugin/
    .cursor-plugin/
      plugin.json             # name: aeye
    hooks/
      hooks.json
    skills/
      diagrams/SKILL.md
      image-gallery/SKILL.md
    scripts/
      core/                   # vendored copy of adapters/core (like Codex)
      lib/shim.sh             # Cursor payload → paths + session id
      images.sh
      diagrams.sh
      diagram-guidance.sh
      session-reset.sh
      session-backfill.sh
    assets/                   # logo for Marketplace (optional but preferred)
```

### Data flow

```text
Cursor Agent tool call
        │
        ▼
postToolUse (matcher Read|Write|Shell)
        │
        ├─ images.sh ──► shim extracts image paths
        │                     │
        │                     ▼
        │              adapters/core append_image_line
        │                     │
        │                     ▼
        │         $AEYE_DIR/images/<pane>.jsonl
        │
        └─ diagrams.sh ─► shim extracts .d2 paths
                              │
                              ▼
                       aeye render-diagram (d2→svg→png)
                              │
                              ▼
                       append with optional vector= SVG
                              │
                              ▼
                       existing aeye / tmux-claude-images viewer
```

SessionStart runs `diagram-guidance.sh` (host-gated), `session-reset.sh`, and
`session-backfill.sh` in parallel — same lifecycle contract as Claude/Codex.

### Keying

1. Prefer `TMUX_PANE` (primary runtime).
2. Else `conversation_id` or `session_id` from the hook payload (same role as
   `CLAUDE_CODE_SESSION_ID` / Codex session id).
3. No pane and no session id → no-op (exit 0).
4. `valid_pane_file` + `owner_selfheal` from core unchanged.

Cursor Desktop (no tmux) is not the v1 target; the session-id fallback is only
so a bare-terminal Cursor Agent still works when a payload id exists.

### Hook table

| Event | Matcher | Script | Role |
|-------|---------|--------|------|
| `postToolUse` | `Read\|Write\|Shell` | `images.sh` | Append image paths |
| `postToolUse` | `Write\|Shell` | `diagrams.sh` | Render `.d2`, append PNG (+ vector) |
| `sessionStart` | — | `diagram-guidance.sh` | Scratch-dir + PATH preflight via `additional_context` |
| `sessionStart` | — | `session-reset.sh` | Drop stale pane manifest on fresh session |
| `sessionStart` | — | `session-backfill.sh` | Rebuild from transcript on resume (gated by spike) |

`hooks.json` uses Cursor **camelCase** event names and relative
`./scripts/….sh` commands. Fail-open: hooks never block the agent.

### Payload shim (`lib/shim.sh`)

Codex-shaped helper module:

- `cursor_session_id PAYLOAD` → `conversation_id` // `session_id` // empty
- `cursor_extract_touched_paths PAYLOAD` → existing image / `.d2` paths:
  - `Read` / `Write`: `tool_input.file_path` or `.path`, resolve against `cwd`
  - `Shell`: scrape absolute/relative image or `.d2` paths from `command` /
    `tool_output` when clearly present (conservative; prefer false negatives)
- Filter to `png|jpe?g|gif|webp|bmp|d2`; require `-f`
- `images.sh` skips `.d2` and dual-theme diagram render artifacts
- `diagrams.sh` only consumes `.d2`

Diagram render and markdown-blank warnings reuse the same `aeye render-diagram`
/ dual-theme behavior as other adapters; adapt any agent-facing warning to
Cursor's `additional_context` response shape (spike confirms exact JSON).

### Skills

Ship `diagrams` and `image-gallery` adapted from the Codex copies:

- Same D2 house style, scratch-dir contract, carousel semantics
- Drop Claude/Codex-only tool names; describe Cursor `Read` / `Write` / `Shell`
- Do not invent a Cursor-only diagram path that bypasses the hook

### Core vendoring

Copy `adapters/core/{manifest-extract,manifest-lifecycle}.sh` into
`plugin/scripts/core/` the way Codex does (not a live symlink into the monorepo
layout that Marketplace zips would break). When core changes, update all three
adapter copies in the same PR (or add a sync check in CI later — not required
for v1).

## Distribution

### Local (dev + nix follow-up)

```bash
ln -sfn /path/to/aeye/adapters/cursor/plugin ~/.cursor/plugins/local/aeye
# Developer: Reload Window
```

nix-config later: HM activation analogous to Codex cache copy / Claude
`--plugin-dir`, pointing at the flake-input plugin dir. Out of scope for the
aeye merge.

### Marketplace

- Adapter-local `.cursor-plugin/marketplace.json` listing plugin `aeye` with
  `source` → `./plugin` (same idea as Codex's `.agents/plugins/marketplace.json`).
- `plugin.json`: `name`, `version`, `description`, `author`, `repository`,
  `license`, `keywords`, `logo`.
- Submit `https://github.com/noamsto/aeye` (or the adapter path the publish form
  accepts) via cursor.com/marketplace/publish after local smoke passes.
- README must state required PATH binaries: `aeye`, `tmux-claude-images`;
  diagrams need `resvg` (and `aeye` which embeds/uses d2 per existing pipeline).

Do **not** put a Cursor marketplace manifest at the repo root next to
`.claude-plugin/` — keep agent marketplaces namespaced under their adapter dirs
(Claude root marketplace already points at `./adapters/claude-code/plugin`).

## Docs touchpoints

- `docs/INSTALL.md` — new Step 3 subsection **Cursor Agent**
- Root README — list `adapters/cursor/` next to Claude/Codex
- `adapters/cursor/README.md` — install, trust/reload, smoke test, limitations
- Close/link [#157](https://github.com/noamsto/aeye/issues/157)

## Testing

1. **bats** for `lib/shim.sh` with Cursor-shaped JSON fixtures (Read/Write/Shell,
   relative path + cwd, non-image no-op, `.d2` vs image split).
2. **Smoke (manual):** tmux + Cursor Agent + local plugin — `Read` a PNG →
   manifest line; `Write` a `.d2` under the SessionStart scratch dir → PNG in
   carousel via `tmux-claude-images`.
3. **Fail-open:** no key → exit 0; missing `aeye`/`resvg` → SessionStart warns
   and does not nudge diagram drawing.
4. **Spike log** under `docs/superpowers/spikes/` for the five empirical gates
   above (same practice as Codex hook-contract spike).

## Error handling

- All hooks fail-open (non-zero/crash must not block the agent).
- stderr-only diagnostics; no interactive prompts.
- Append-only manifests; viewer dedups `(path, mtime)` on read.
- Owner self-heal + lock file via core, parameterized with Cursor session id.

## Non-goals (v1)

- Cursor Desktop as primary runtime (no `TMUX_PANE`)
- Browser / MCP screenshot auto-capture
- Viewer or manifest schema changes
- Teaching nix-config `agent-skills` to vend aeye
- Claiming fake parity if a spike kills SessionStart context or backfill —
  document the subset and ship

## Open risks (gate the plan)

| Risk | Mitigation |
|------|------------|
| Plugin hooks don't fire until Marketplace install / trust UI | Spike with `plugins/local`; document Reload + any trust prompt |
| `additional_context` shape differs from docs | Spike; fall back to skill-only guidance if SessionStart injection fails |
| Resume backfill has no transcript | Ship capture + guidance first; mark backfill deferred in INSTALL |
| Shell path scraping is noisy | Keep conservative; rely on Write for diagrams |
| Hook cwd ≠ agent cwd for path resolve | Prefer absolute paths; resolve with payload `cwd` |

## Implementation sketch (for the plan, not this PR)

1. Spike Cursor hook contract → write spike note.
2. Scaffold `adapters/cursor/` from Codex layout; vendor core; write `lib/shim.sh` + bats.
3. Port `images.sh` / `diagrams.sh` / SessionStart scripts; Cursor JSON out for guidance warnings.
4. Adapt skills; marketplace + plugin manifests + README.
5. INSTALL + root README; smoke; publish checklist.
6. Follow-up issues: nix-config local plugin wiring; browser/MCP capture stretch.
