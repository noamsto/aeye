# Progress ledger — Codex adapter parity (#122)

Plan: docs/superpowers/plans/2026-07-12-codex-adapter-parity.md
Branch: feat/122-codex-adapter

## Status

- Task 0.1 (spike: hook runtime fires) — COMPLETE, GATE PASSED. Contract in docs/superpowers/spikes/2026-07-12-codex-hook-contract.md. Hook payload normalized (clean tool_name + structured tool_input); JS-unwrap is backfill-only. Plan/spec updated to match.
- Task 0.2 (spike: install + trust) — PARTIAL: install copies to versioned cache; runtime hook-trust gate exists (needed --dangerously-bypass-hook-trust). Trust-persistence for Nix = remaining Phase 4 unknown; fallback documented. Resolve before Task 4.1.
- Tasks 1.1+ — UNLOCKED (0.1 passed). Next: Task 1.1 (core refactor).

## Minor findings (for final whole-branch review triage)

- Task 1.1: `resolve`/`is_ext` duplicated across Claude lib + core `scan_response_image_path` (spec-mandated; harmless). If a 3rd adapter needs Phase-1-style resolution, dedupe.
- Task 1.1: image-extension list maintained in 3 spots (fast-bail regex, `is_ext`, jq capture) — pre-existing, not worsened; Codex core will also depend on the core copy.
- Task 1.2: `diagrams.bats` has no owner-self-heal case (images.sh path IS covered via adapter.bats; owner_selfheal is now shared so the fn is tested). Consider adding a diagrams.sh owner-drop case.
- Task 1.2 ⚠️ RESOLVED (not a defect): reviewer flagged possible divergence of two manifest-extract.sh copies — there is only ONE (core/); Claude lib + lifecycle lib both source it, idempotent double-source, single source of truth.

## Completed

Task 0.1: spike gate PASSED (hooks fire in 0.144.1; contract captured)
Task 1.1: complete (commits fa1e413..a535f1c, review clean — core manifest-extract.sh extracted, 117/117 bats + go green)
Task 1.2: complete (commits 2ceabc6..a78a9e0, review clean opus — manifest-lifecycle.sh extracted, session-id parameterized, all 5 fragility risks byte-identical, 117/117 + go green). PHASE 1 DONE.
Task 2.1: complete (commits cade0fa..1b585a5 — Codex plugin.json + hooks.json + provisional marketplace.json; review found missing interface.defaultPrompt (Important), fixed in 1b585a5; validate_plugin.py PASSES under nix python w/ pyyaml). marketplace.json path:"./" provisional → finalize in Task 4.1.
