# Progress ledger — Codex adapter parity (#122)

Plan: docs/superpowers/plans/2026-07-12-codex-adapter-parity.md
Branch: feat/122-codex-adapter

Task 3.1: complete (commits ec7d536..aef3c31, review clean opus — session-backfill.sh rollout replay w/ JS-unwrap normalization; 2 TDD bugs fixed (cwd grep-widen, set-e unwrap guards); all 7 named risks clean incl set-e no-abort-mid-rebuild; 178/178 bats green). PHASE 3 DONE. In-repo adapter COMPLETE.

## Status

- Task 0.1 (spike: hook runtime fires) — COMPLETE, GATE PASSED. Contract in docs/superpowers/spikes/2026-07-12-codex-hook-contract.md. Hook payload normalized (clean tool_name + structured tool_input); JS-unwrap is backfill-only. Plan/spec updated to match.
- Task 0.2 (spike: install + trust) — PARTIAL: install copies to versioned cache; runtime hook-trust gate exists (needed --dangerously-bypass-hook-trust). Trust-persistence for Nix = remaining Phase 4 unknown; fallback documented. Resolve before Task 4.1.
- Tasks 1.1+ — UNLOCKED (0.1 passed). Next: Task 1.1 (core refactor).

## Minor findings (for final whole-branch review triage)

- Task 1.1: `resolve`/`is_ext` duplicated across Claude lib + core `scan_response_image_path` (spec-mandated; harmless). If a 3rd adapter needs Phase-1-style resolution, dedupe.
- Task 1.1: image-extension list maintained in 3 spots (fast-bail regex, `is_ext`, jq capture) — pre-existing, not worsened; Codex core will also depend on the core copy.
- Task 1.2: `diagrams.bats` has no owner-self-heal case (images.sh path IS covered via adapter.bats; owner_selfheal is now shared so the fn is tested). Consider adding a diagrams.sh owner-drop case.
- Task 1.2 ⚠️ RESOLVED (not a defect): reviewer flagged possible divergence of two manifest-extract.sh copies — there is only ONE (core/); Claude lib + lifecycle lib both source it, idempotent double-source, single source of truth.
- Task 2.3: diagrams.sh:97 — `_manifest_lock` inside the per-candidate loop holds the lock across candidate 2..N's slow `d2_render` (fd 9 persists). Contention regression only (no corruption/deadlock); comment "rendering never holds the lock" now false for multi-.d2. Fix: render all first, then lock once for prune/append.
- Task 2.3: diagrams.sh:89-91 — two markdown-broken .d2 in one apply_patch emit two hookSpecificOutput JSON objects on stdout (PostToolUse expects ≤1); worst case lost/mangled warning, no corruption. (PostToolUse additionalContext honoring itself unconfirmed.)
- Task 2.3: test gap — no bats case exercises a single apply_patch with 2+ .d2/images (the loop generalization, the whole point of the task, is untested on the multi-path branch). A 2-.d2 fixture asserting 2 manifest lines would cover it.

## FOLLOW-UP GAP (parity, beyond current plan tasks)
- `scripts/tmux-claude-images.sh` keys its OUTSIDE-tmux/kitty launch off `${TMUX_PANE:-${CLAUDE_CODE_SESSION_ID:-}}`. Codex never sets CLAUDE_CODE_SESSION_ID (session id is payload-only), so bare-terminal/kitty carousel launch is UNWIRED for Codex. In-tmux (TMUX_PANE) works. Fix = wire the toggle to a Codex-supplied key (new follow-up task; shared viewer infra, also used by lazytmux — cross-repo consideration). SKILL.md wording hedged in 0cb52c8 so it doesn't over-claim.

## Completed

Task 0.1: spike gate PASSED (hooks fire in 0.144.1; contract captured)
Task 1.1: complete (commits fa1e413..a535f1c, review clean — core manifest-extract.sh extracted, 117/117 bats + go green)
Task 1.2: complete (commits 2ceabc6..a78a9e0, review clean opus — manifest-lifecycle.sh extracted, session-id parameterized, all 5 fragility risks byte-identical, 117/117 + go green). PHASE 1 DONE.
Task 2.1: complete (commits cade0fa..1b585a5 — Codex plugin.json + hooks.json + provisional marketplace.json; review found missing interface.defaultPrompt (Important), fixed in 1b585a5; validate_plugin.py PASSES under nix python w/ pyyaml). marketplace.json path:"./" provisional → finalize in Task 4.1.
Task 2.4: complete (commits ab0f51a..0cb52c8 — session-reset.sh + diagram-guidance.sh (verbatim) + both skills + plugin.json skills field; validate_plugin.py PASSES; 163/163 bats green. Review found Important doc oversell in image-gallery SKILL.md (outside-tmux launch broken for Codex), fixed in 0cb52c8. Toggle gap → FOLLOW-UP above.) PHASE 2 DONE.
Task 2.3: complete (commits d6720a4..b79d4d1, review clean opus — images.sh + diagrams.sh capture hooks ported, 137/137 bats green, shellcheck clean. 3 Minors on multi-.d2 branch logged above for final review.)
Task 2.2: complete (commits 8df0278..0a5b55a — shim.sh: codex_session_id, codex_extract_touched_paths, _codex_apply_patch_paths; tests/codex/extract.bats + fixtures. Review found Important newline-loss defect (scan output dropped by while-read consumers), fixed in 0a5b55a w/ hardened read-loop test. ALSO: changed justfile + ci.yml to `bats --recursive tests/` (bats didn't recurse into tests/codex/) — verify still correct at final review. 123/123 bats green.
