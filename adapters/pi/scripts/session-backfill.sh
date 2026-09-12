#!/usr/bin/env bash
# session_start(resume|fork) for the aeye pi extension: rebuild this pane/
# session's image manifest from the pi session history, so the carousel is
# populated after resuming (or forking) a session instead of empty. The
# extension collects every historical tool call (read/write/edit/bash, plus the
# paired tool output for screenshots) as normalized payloads, one JSON object
# per line in calls_file, and this script replays them through the same
# extraction the live hook uses.
#
# The history is authoritative: the manifest is rebuilt from scratch, so a prior
# session's images left under a reused tmux pane id cannot bleed through. This
# is the sole writer of a resumed pane's manifest — session-reset defers to it.
set -euo pipefail

PLUGIN_ROOT="${PLUGIN_ROOT:-$(dirname "${BASH_SOURCE[0]}")/..}"
# shellcheck source=lib/shim.sh disable=SC1091
source "$PLUGIN_ROOT/scripts/lib/shim.sh"
# shellcheck source=core/manifest-lifecycle.sh disable=SC1091
source "$PLUGIN_ROOT/scripts/core/manifest-lifecycle.sh"

payload="$(cat)"
[[ -n $payload ]] || exit 0

session="$(pi_session_id "$payload")"
calls_file="$(jq -r '.calls_file // empty' <<<"$payload" 2>/dev/null)"

pane_file="$(resolve_pane_key "$session")"
[[ -n $pane_file ]] || exit 0
valid_pane_file "$pane_file" || exit 0

resolve_state_dirs

manifest_paths "$pane_file"
# shellcheck disable=SC2153 # MANIFEST/OWNER_FILE come from core's manifest_paths
manifest="$MANIFEST"
# shellcheck disable=SC2153
owner_file="$OWNER_FILE"
mkdir -p "$IMAGES_DIR"

# Hold the pane's manifest lock across the whole clear/rebuild — a live images.sh
# or diagrams.sh append fired right after resume must wait, not interleave with
# the authoritative rebuild (which starts by wiping the manifest).
_manifest_lock "$LOCK_FILE"

# A nested agent (a headless run started from inside this session) inherits the
# pane and must not have its parent's carousel wiped (#234); the parent owns the
# pane and will rebuild it.
if owner_live "$pane_file" "$session"; then
	exit 0
fi

# Without a readable call list we can't rebuild. Drop a manifest we can't prove
# belongs to this session (reused-pane-id bleed); keep one this session owns
# (a legit continuation whose history is unreadable) rather than blank it.
if [[ -z $calls_file || ! -r $calls_file ]]; then
	owner=""
	[[ -f $owner_file ]] && owner="$(<"$owner_file")"
	[[ -f $manifest && (-z $owner || $owner != "$session") ]] && rm -f "$manifest" "$owner_file" "$IMAGES_DIR/$pane_file.ownerpid"
	exit 0
fi

# Authoritative rebuild: start from empty rather than merge into whatever the
# pane held — on a reused pane id that's a prior session's images. seen dedups
# within this replay only (a repeated history call).
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

# Replay each historical call. Every jq failure defaults to empty so a single
# malformed line yields nothing rather than aborting the rebuild under set -e.
while IFS= read -r call; do
	[[ -n $call ]] || continue
	source_tool="$(jq -r '.tool_name // "?"' <<<"$call" 2>/dev/null)" || source_tool="?"
	ts="$(jq -r '.ts // empty' <<<"$call" 2>/dev/null)" || ts=""
	[[ -n $ts ]] || printf -v ts '%(%FT%T%z)T' -1

	p="$(extract_image_path "$call")" || p=""
	[[ -n $p ]] && append_image "$p" "$source_tool" "$ts"

	d2="$(extract_d2_path "$call" "$DIAGRAMS_DIR/src")" || d2=""
	if [[ -n $d2 ]]; then
		png="$(d2_render "$d2" "$DIAGRAMS_DIR")" || continue
		append_diagram "$png" "${png%.png}.svg" "$ts" "$(basename "$d2" .d2)"
	fi
done <"$calls_file"

# Claim the rebuilt manifest so the live hooks' owner self-heal does not drop it.
[[ -f $manifest ]] && owner_claim "$pane_file" "$session"
exit 0
