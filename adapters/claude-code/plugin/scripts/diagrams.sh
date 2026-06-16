#!/usr/bin/env bash
# Render a .d2 file the agent wrote into a PNG and append it to the per-pane
# image manifest. PostToolUse hook: reads the hook JSON payload on stdin.
# Mirrors images.sh — self-contained, keyed by $TMUX_PANE or $CLAUDE_CODE_SESSION_ID.
set -euo pipefail

STATE_DIR="${AEYE_DIR:-${CLAUDE_STATUS_DIR:-/tmp/claude-status}}"
IMAGES_DIR="$STATE_DIR/images"
DIAGRAMS_DIR="$IMAGES_DIR/diagrams"

pane_id="${TMUX_PANE:-${CLAUDE_CODE_SESSION_ID:-}}"
[[ -n $pane_id ]] || exit 0
pane_file="${pane_id#%}"
[[ $pane_file =~ ^[A-Za-z0-9_@:.-]+$ ]] || exit 0

payload="$(cat)"
[[ -n $payload ]] || exit 0

# shellcheck source=lib/manifest-extract.sh disable=SC1091
source "$(dirname "${BASH_SOURCE[0]}")/lib/manifest-extract.sh"

candidate="$(extract_d2_path "$payload")"
[[ -n $candidate ]] || exit 0

mkdir -p "$DIAGRAMS_DIR"
png="$(d2_png_for "$candidate" "$DIAGRAMS_DIR")"
svg="${png%.png}.svg"
manifest="$IMAGES_DIR/$pane_file.jsonl"

# A fresh render (PNG absent beforehand) is the only time the markdown check
# applies — a cached PNG was already vetted on its first render.
was_missing=1
[[ -f $png ]] && was_missing=0
d2_render "$candidate" "$DIAGRAMS_DIR" >/dev/null || exit 0

# d2 emits |md / |markdown bodies as an HTML <foreignObject>, which resvg can't
# paint — those nodes rasterize blank while d2 exits 0, a silent failure that
# looks like a missing entity. Detect it on the rendered SVG (exact: no
# source-grep false positives, catches every markdown syntax) and warn the agent.
# Non-blocking — the rest of the diagram still renders.
if [[ $was_missing -eq 1 ]] && grep -q '<foreignObject' "$svg"; then
	printf -v now '%(%FT%T%z)T' -1
	printf '%s\t%s\tWARN markdown block(s) render blank in resvg (<foreignObject>)\n' \
		"$now" "$(basename "$candidate")" >>"$DIAGRAMS_DIR/render-errors.log"
	warn="$(basename "$candidate") contains markdown (|md / |markdown) block(s) that render BLANK in the carousel: resvg can't paint the HTML <foreignObject> that D2 emits for markdown. Rewrite those node bodies as plain quoted labels (use \\n for line breaks)."
	jq -nc --arg ctx "$warn" \
		'{hookSpecificOutput:{hookEventName:"PostToolUse",additionalContext:$ctx}}'
fi

# Self-heal against tmux pane-id reuse: a manifest last written by a different
# Claude session belongs to a pane that's since been recycled — drop it so this
# session's carousel never blends in a prior session's images. (The SessionStart
# reset already covers fresh starts; this also guards a start the reset missed.)
owner="$IMAGES_DIR/$pane_file.owner"
if [[ -n ${CLAUDE_CODE_SESSION_ID:-} ]]; then
	if [[ -f $owner && $(<"$owner") != "$CLAUDE_CODE_SESSION_ID" ]]; then
		rm -f "$manifest"
	fi
	printf '%s' "$CLAUDE_CODE_SESSION_ID" >"$owner"
fi

# Append guarded by a path-dedup check (independent of the render step, so a
# diagram missing from the manifest is re-added even when its PNG is cached).
if [[ -f $manifest ]] &&
	jq -e --arg p "$png" 'select(.path == $p)' "$manifest" >/dev/null 2>&1; then
	exit 0
fi

mtime="$(stat -c %Y "$png" 2>/dev/null || echo 0)"
printf -v now '%(%FT%T%z)T' -1
jq -nc --arg path "$png" --arg vector "$svg" --arg source "d2" --arg ts "$now" --argjson mtime "$mtime" \
	'{type:"image", path:$path, vector:$vector, source:$source, ts:$ts, mtime:$mtime}' >>"$manifest"

# Proactively surface the carousel on every new diagram. Reached only when a
# genuinely new diagram was appended above (the dedup guard exits early for ones
# already in the manifest), so this re-opens after a manual close but never
# re-fires for an unchanged redraw. --ensure-open is idempotent and never kills.
"${AEYE_TOGGLE:-tmux-claude-images}" --ensure-open >/dev/null 2>&1 || true
