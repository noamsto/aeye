#!/usr/bin/env bash
# SessionStart hook: rebuild this pane/session's image manifest from the
# Cursor agent-transcript file, so the carousel is populated after a
# `cursor-agent --resume` instead of empty. Mirrors the Codex adapter's
# session-backfill.sh; the transcript is authoritative, so the manifest is
# rebuilt from scratch and a prior session's images left under a reused tmux
# pane id cannot bleed through. This is the sole writer of a resumed pane's
# manifest — session-reset.sh defers to it on a detected resume, since
# SessionStart hooks run in parallel with no ordered turn.
#
# Two load-bearing assumptions, both unprovable in this environment (see
# docs/superpowers/plans/2026-09-07-cursor-resume-backfill.md for the full
# reasoning):
#
# 1. `cursor-agent --resume` reuses the original conversation_id. If false,
#    the glob below simply finds zero matches and this hook exits 0 —
#    identical to the prior shipped no-op, not a regression.
# 2. Per-conversation transcript lines are already shaped like the proven
#    postToolUse payload (tool_name, tool_input, tool_output,
#    cwd/workspace_roots), so each line is replayed directly through
#    cursor_extract_touched_paths (no separate "unwrap" step, unlike Codex's
#    raw rollout transport). If the real schema differs, every line fails the
#    tool_name/tool_input.file_path lookup and the backfill degrades to an
#    empty manifest — again, today's no-op, not a crash.
#
# Direction check on assumption 1: the false-positive direction (a brand-new
# conversation's sessionStart firing while a non-empty transcript already
# exists under its fresh conversation_id) isn't reachable — sessionStart
# fires before any message is exchanged, so a genuinely new conversation has
# nothing to have written yet. If it somehow happened anyway (id reuse,
# clock/filesystem weirdness), this script's unconditional rm -f + rebuild
# below still replaces whatever was there, and owner_selfheal (fired on the
# first real tool call) re-stamps ownership.
set -euo pipefail

payload="$(cat)"
[[ -n $payload ]] || exit 0

PLUGIN_ROOT="${PLUGIN_ROOT:-$(dirname "${BASH_SOURCE[0]}")/..}"
# shellcheck source=lib/shim.sh disable=SC1091
source "$PLUGIN_ROOT/scripts/lib/shim.sh"
# shellcheck source=core/manifest-lifecycle.sh disable=SC1091
source "$PLUGIN_ROOT/scripts/core/manifest-lifecycle.sh"

resolve_state_dirs

session="$(cursor_session_id "$payload")"
pane_file="$(resolve_pane_key "$session")"
[[ -n $pane_file ]] || exit 0
valid_pane_file "$pane_file" || exit 0

manifest_paths "$pane_file"
# shellcheck disable=SC2153 # MANIFEST/OWNER_FILE come from core's manifest_paths
manifest="$MANIFEST"
# shellcheck disable=SC2153
owner_file="$OWNER_FILE"
mkdir -p "$IMAGES_DIR"

# A nested agent resolves this same manifest key (it inherits $TMUX_PANE) and
# reports its own resume, whose rebuild below starts by wiping the manifest —
# which would be the spawning session's carousel, rebuilt from the wrong
# transcript (#234). Leave a live owner's pane untouched.
if owner_live "$pane_file" "$session"; then
	exit 0
fi

transcript="$(cursor_resume_transcript "$session")"

# This script runs on every sessionStart (Cursor has no .source field), so an
# empty transcript here is the ordinary new-conversation path, running
# concurrently with session-reset.sh's clear+stamp and possibly a live
# images.sh append. manifest_paths sets LOCK_FILE to the same path those two
# lock, so the drop below serializes against both regardless of which hook's
# SessionStart fires first.
_manifest_lock "$LOCK_FILE"

# No transcript, or one that's unreadable (permission bits, or removed since
# the glob): drop a manifest this session can't prove it owns, keep one it
# does.
if [[ -z $transcript || ! -r $transcript ]]; then
	owner=""
	[[ -f $owner_file ]] && owner="$(<"$owner_file")"
	[[ -f $manifest && (-z $owner || $owner != "$session") ]] && rm -f "$manifest" "$owner_file"
	exit 0
fi

# Authoritative rebuild: the transcript is the record of what this session
# touched, so start from empty rather than merge into whatever the pane held —
# on a reused pane id that's a prior session's images. seen dedups within this
# replay only (a repeated transcript path).
rm -f "$manifest"
declare -A seen=()

append_image() { # $1 path  $2 source  $3 ts
	# A diagram inspection is already represented by its canonical d2 entry.
	is_d2_render_artifact "$1" "$DIAGRAMS_DIR" && return 0
	[[ -n ${seen["$1"]:-} ]] && return 0
	seen["$1"]=1
	append_image_line "$manifest" "$1" "$2" "$3"
}

append_diagram() { # $1 png  $2 svg  $3 ts  $4 name
	[[ -n ${seen["$1"]:-} ]] && return 0
	seen["$1"]=1
	append_diagram_line "$manifest" "$1" "$2" "$4" "$3"
}

# Only lines that could plausibly carry an image/.d2 reach jq (raw grep
# fast-bail, like images.sh). A crashed prior session can leave a truncated,
# non-JSON final line; jq exits nonzero on it and the bare assignment would
# abort the whole rebuild under set -e, so every jq call below defaults to
# empty on failure and lets that line yield nothing.
while IFS= read -r line; do
	[[ -n $line ]] || continue
	ts="$(jq -r '.timestamp // empty' <<<"$line" 2>/dev/null)" || ts=""
	tool_name="$(jq -r '.tool_name // "?"' <<<"$line" 2>/dev/null)" || tool_name="?"
	while IFS= read -r p; do
		[[ -n $p ]] || continue
		if [[ ${p,,} == *.d2 ]]; then
			png="$(d2_render "$p" "$DIAGRAMS_DIR")" || continue
			append_diagram "$png" "${png%.png}.svg" "$ts" "$(basename "$p" .d2)"
		else
			append_image "$p" "$tool_name" "$ts"
		fi
	done < <(cursor_extract_touched_paths "$line")
done < <(grep -E '\.(png|jpe?g|gif|webp|bmp|d2)' "$transcript")

# Claim the rebuilt manifest so the live hooks' owner self-heal does not drop it.
[[ -f $manifest && -n $session ]] && printf '%s' "$session" >"$owner_file"
