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

## Completed

Task 0.1: spike gate PASSED (hooks fire in 0.144.1; contract captured)
Task 1.1: complete (commits fa1e413..a535f1c, review clean — core manifest-extract.sh extracted, 117/117 bats + go green)
