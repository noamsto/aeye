#!/usr/bin/env bash
# Cursor sessionStart fires for new conversations only — no resume event and
# transcript_path is null at sessionStart (see
# docs/superpowers/spikes/2026-07-28-cursor-hook-contract.md). Resume backfill
# is deferred until a transcript iterator exists — #157 follow-up.
exit 0
