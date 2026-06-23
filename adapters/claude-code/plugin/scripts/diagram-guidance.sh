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
This session has an image carousel. When a diagram would clarify your
explanation — architecture, data flow, state machines, pipelines, entity
relationships — Write a D2 diagram as a .d2 file and it renders into the
carousel automatically. Write it to: $SRC_DIR/<name>.d2 (an absolute path
outside any repo; never write .d2 files into the working project).
Do NOT diagram trivial or linear one-step things. One diagram per concept.
Escape a literal \$ in labels as \\\$ — a bare \$ starts a D2 substitution and
the diagram silently fails to compile.
Use plain quoted labels (\\n for line breaks); do NOT use |md / |markdown block
bodies anywhere — including title: — the carousel rasterizer can't paint them,
so they render blank and the whole diagram is suppressed (it won't appear at all)
until you rewrite them as plain quoted labels.
Prose stays primary — a diagram supplements, never replaces, the explanation.
EOF

jq -nc --arg ctx "$guidance" \
	'{hookSpecificOutput:{hookEventName:"SessionStart",additionalContext:$ctx}}'
