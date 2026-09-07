#!/usr/bin/env bash
# SessionStart hook: when a carousel host is present, nudge the agent to draw
# diagrams as .d2 files (rendered into the carousel by diagrams.sh). Host-gated
# so the guidance only loads where a diagram can actually be displayed.
set -euo pipefail

[[ -n ${TMUX:-} || -n ${KITTY_LISTEN_ON:-} ]] || exit 0

# Render-pipeline preflight. diagrams.sh runs `aeye render-diagram` (d2 is
# embedded; it shells out only to resvg), and silently no-ops when either is
# unreachable on the PATH this hook's env shares with the PostToolUse hooks.
# Detect that here — once, at SessionStart — and warn instead of nudging the
# agent to draw diagrams that will never render. Resolution mirrors d2_render
# (AEYE_BIN / AEYE_RESVG overrides).
missing=()
command -v "${AEYE_BIN:-aeye}" >/dev/null 2>&1 || missing+=("${AEYE_BIN:-aeye}")
command -v "${AEYE_RESVG:-resvg}" >/dev/null 2>&1 || missing+=("${AEYE_RESVG:-resvg}")
if ((${#missing[@]})); then
	warn="Diagram rendering is unavailable: ${missing[*]} not found on PATH for this hook, so any .d2 you write will silently NOT render into the carousel. Don't draw diagrams this session; tell the user the diagram hook is missing ${missing[*]} on PATH (e.g. add the aeye package to home.packages)."
	jq -nc --arg ctx "$warn" \
		'{hookSpecificOutput:{hookEventName:"SessionStart",additionalContext:$ctx}}'
	exit 0
fi

STATE_DIR="${AEYE_DIR:-${CLAUDE_STATUS_DIR:-/tmp/claude-status}}"
SRC_DIR="$STATE_DIR/images/diagrams/src"
mkdir -p "$SRC_DIR"

read -r -d '' guidance <<EOF || true
This session has an image carousel next to the terminal. Any D2 diagram you
write to $SRC_DIR/<name>.d2 renders there automatically — draw one whenever a
picture would carry part of your explanation: architecture, data flow, state
machines, pipelines, entity relationships. Not for linear or trivial things,
and the prose still does the explaining. Never write .d2 files inside the
working project. The aeye diagrams skill has the few syntax rules that keep a
render clean; load it when you draw.
EOF

jq -nc --arg ctx "$guidance" \
	'{hookSpecificOutput:{hookEventName:"SessionStart",additionalContext:$ctx}}'
