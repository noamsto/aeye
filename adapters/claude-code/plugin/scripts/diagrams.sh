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
svg="$DIAGRAMS_DIR/$hash.svg"
manifest="$IMAGES_DIR/$pane_file.jsonl"

# Render only when the PNG is absent (identical source is a no-op; an edited
# diagram hashes differently). Renderers absent -> silent no-op.
if [[ ! -f $png ]]; then
	d2_bin="${AEYE_D2:-d2}"
	resvg_bin="${AEYE_RESVG:-resvg}"
	command -v "$d2_bin" >/dev/null 2>&1 || exit 0
	command -v "$resvg_bin" >/dev/null 2>&1 || exit 0
	err="$DIAGRAMS_DIR/$hash.err"

	d2_args=(-t "${AEYE_D2_THEME:-105}")
	[[ ${AEYE_D2_SKETCH:-1} != 0 ]] && d2_args+=(--sketch)
	if ! "$d2_bin" "${d2_args[@]}" "$candidate" "$svg" 2>"$err"; then
		printf -v now '%(%FT%T%z)T' -1
		printf '%s\t%s\t%s\n' "$now" "$hash" "$(tr '\n' ' ' <"$err")" \
			>>"$DIAGRAMS_DIR/render-errors.log"
		rm -f "$svg" "$err"
		exit 0
	fi

	# resvg can't use d2's embedded @font-face fonts; rewrite to an installed family.
	if ! bash "$(dirname "${BASH_SOURCE[0]}")/d2-fix-fonts.sh" "$svg" 2>>"$err"; then
		printf -v now '%(%FT%T%z)T' -1
		printf '%s\t%s\t%s\n' "$now" "$hash" "$(tr '\n' ' ' <"$err")" \
			>>"$DIAGRAMS_DIR/render-errors.log"
		rm -f "$svg" "$err"
		exit 0
	fi

	# Recolor labels to contrast their node's fill — d2 colors labels from the
	# theme, not the fill, so a light fill under a dark theme (or vice versa)
	# renders illegible text. Best-effort: an absent or older binary just renders
	# without the pass.
	contrast_bin="$(command -v "${AEYE_BIN:-aeye}" 2>/dev/null || true)"
	if [[ -n $contrast_bin ]]; then
		"$contrast_bin" svg-contrast "$svg" 2>>"$err" || true
	fi

	resvg_args=()
	if [[ -n ${AEYE_D2_FONT_DIR:-} ]]; then
		resvg_args+=(--skip-system-fonts --use-fonts-dir "$AEYE_D2_FONT_DIR")
	fi
	if ! "$resvg_bin" "${resvg_args[@]}" "$svg" "$png" 2>>"$err"; then
		printf -v now '%(%FT%T%z)T' -1
		printf '%s\t%s\t%s\n' "$now" "$hash" "$(tr '\n' ' ' <"$err")" \
			>>"$DIAGRAMS_DIR/render-errors.log"
		rm -f "$svg" "$err" "$png"
		exit 0
	fi
	rm -f "$err"

	# d2 emits |md / |markdown bodies as an HTML <foreignObject>, which resvg
	# can't paint — those nodes rasterize blank while d2 exits 0, a silent
	# failure that looks like a missing entity. Detect it on the rendered SVG
	# (exact: no source-grep false positives, catches every markdown syntax) and
	# warn the agent. Non-blocking — the rest of the diagram still renders.
	if grep -q '<foreignObject' "$svg"; then
		printf -v now '%(%FT%T%z)T' -1
		printf '%s\t%s\tWARN markdown block(s) render blank in resvg (<foreignObject>)\n' \
			"$now" "$(basename "$candidate")" >>"$DIAGRAMS_DIR/render-errors.log"
		warn="$(basename "$candidate") contains markdown (|md / |markdown) block(s) that render BLANK in the carousel: resvg can't paint the HTML <foreignObject> that D2 emits for markdown. Rewrite those node bodies as plain quoted labels (use \\n for line breaks)."
		jq -nc --arg ctx "$warn" \
			'{hookSpecificOutput:{hookEventName:"PostToolUse",additionalContext:$ctx}}'
	fi
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
