#!/usr/bin/env bash
# AEYE_SESSION_ID ends up concatenated into a manifest file path
# (adapters/pi/scripts/core/manifest-lifecycle.sh's manifest_paths) and used as
# a bare identity string, so a malformed value here degrades to "no session
# id" rather than reaching that path-building code unsanitized — defense in
# depth, since pi's own session ids are UUID-shaped today.
sid="${HOOKYARD_SESSION_ID:-}"
[[ $sid =~ ^[A-Za-z0-9_-]*$ ]] || sid=""
export AEYE_SESSION_ID="$sid"
exec "${AEYE_TOGGLE:-tmux-claude-images}"
