# Progress ledger — Codex adapter parity (#122)

Plan: docs/superpowers/plans/2026-07-12-codex-adapter-parity.md
Branch: feat/122-codex-adapter

## Status

- Task 0.1 (spike: hook runtime fires) — COMPLETE, GATE PASSED. Contract in docs/superpowers/spikes/2026-07-12-codex-hook-contract.md. Hook payload normalized (clean tool_name + structured tool_input); JS-unwrap is backfill-only. Plan/spec updated to match.
- Task 0.2 (spike: install + trust) — PARTIAL: install copies to versioned cache; runtime hook-trust gate exists (needed --dangerously-bypass-hook-trust). Trust-persistence for Nix = remaining Phase 4 unknown; fallback documented. Resolve before Task 4.1.
- Tasks 1.1+ — UNLOCKED (0.1 passed). Next: Task 1.1 (core refactor).

## Completed

Task 0.1: spike gate PASSED (hooks fire in 0.144.1; contract captured)
