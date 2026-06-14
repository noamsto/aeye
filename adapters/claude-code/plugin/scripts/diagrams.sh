#!/usr/bin/env bash
# Render a .d2 file the agent wrote into a PNG and append it to the per-pane
# image manifest. PostToolUse hook: reads the hook JSON payload on stdin.
# Mirrors images.sh — self-contained, keyed by $TMUX_PANE or $CLAUDE_CODE_SESSION_ID.
set -euo pipefail

STATE_DIR="${AGENT_CAROUSEL_DIR:-${CLAUDE_STATUS_DIR:-/tmp/claude-status}}"
IMAGES_DIR="$STATE_DIR/images"
DIAGRAMS_DIR="$IMAGES_DIR/diagrams"

pane_id="${TMUX_PANE:-${CLAUDE_CODE_SESSION_ID:-}}"
[[ -n $pane_id ]] || exit 0
pane_file="${pane_id#%}"
[[ $pane_file =~ ^[A-Za-z0-9_@:.-]+$ ]] || exit 0

payload="$(cat)"
[[ -n $payload ]] || exit 0

cwd="$(jq -r '.cwd // empty' <<<"$payload" 2>/dev/null)"

# The agent's .d2 file path comes from tool_input.file_path (Write/Edit/MultiEdit).
candidate="$(jq -r '.tool_input.file_path // empty' <<<"$payload" 2>/dev/null)"
[[ -n $candidate ]] || exit 0
if [[ $candidate != /* ]] && [[ -n $cwd ]]; then
	candidate="$cwd/$candidate"
fi
# Fast-bail: only .d2 files that exist.
[[ ${candidate,,} == *.d2 ]] || exit 0
[[ -f $candidate ]] || exit 0

mkdir -p "$DIAGRAMS_DIR"
hash="$(sha256sum "$candidate" | cut -c1-16)"
png="$DIAGRAMS_DIR/$hash.png"
manifest="$IMAGES_DIR/$pane_file.jsonl"

# Render only when the PNG is absent (identical source is a no-op; an edited
# diagram hashes differently). Renderers absent -> silent no-op.
if [[ ! -f $png ]]; then
	d2_bin="${AGENT_CAROUSEL_D2:-d2}"
	resvg_bin="${AGENT_CAROUSEL_RESVG:-resvg}"
	command -v "$d2_bin" >/dev/null 2>&1 || exit 0
	command -v "$resvg_bin" >/dev/null 2>&1 || exit 0
	svg="$DIAGRAMS_DIR/$hash.svg"
	err="$DIAGRAMS_DIR/$hash.err"
	if ! "$d2_bin" "$candidate" "$svg" 2>"$err"; then
		printf -v now '%(%FT%T%z)T' -1
		printf '%s\t%s\t%s\n' "$now" "$hash" "$(tr '\n' ' ' <"$err")" \
			>>"$DIAGRAMS_DIR/render-errors.log"
		rm -f "$svg" "$err"
		exit 0
	fi
	if ! "$resvg_bin" "$svg" "$png" 2>>"$err"; then
		printf -v now '%(%FT%T%z)T' -1
		printf '%s\t%s\t%s\n' "$now" "$hash" "$(tr '\n' ' ' <"$err")" \
			>>"$DIAGRAMS_DIR/render-errors.log"
		rm -f "$svg" "$err" "$png"
		exit 0
	fi
	rm -f "$svg" "$err"
fi

# Append guarded by a path-dedup check (independent of the render step, so a
# diagram missing from the manifest is re-added even when its PNG is cached).
if [[ -f $manifest ]] &&
	jq -e --arg p "$png" 'select(.path == $p)' "$manifest" >/dev/null 2>&1; then
	exit 0
fi

mtime="$(stat -c %Y "$png" 2>/dev/null || echo 0)"
printf -v now '%(%FT%T%z)T' -1
jq -nc --arg path "$png" --arg source "d2" --arg ts "$now" --argjson mtime "$mtime" \
	'{type:"image", path:$path, source:$source, ts:$ts, mtime:$mtime}' >>"$manifest"

# Auto-open once per session: the first new diagram surfaces the carousel; after
# that, leave open/closed state under user control (--ensure-open never kills).
marker="$IMAGES_DIR/$pane_file.opened"
if [[ ! -f $marker ]]; then
	"${AGENT_CAROUSEL_TOGGLE:-tmux-claude-images}" --ensure-open >/dev/null 2>&1 || true
	: >"$marker"
fi
