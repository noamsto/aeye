#!/usr/bin/env bash
# SessionStart hook: when a carousel host is present, nudge the agent to draw
# diagrams as .d2 files (rendered into the carousel by diagrams.sh). Host-gated
# so the guidance only loads where a diagram can actually be displayed.
set -euo pipefail

[[ -n ${TMUX:-} || -n ${KITTY_LISTEN_ON:-} ]] || exit 0

STATE_DIR="${AGENT_CAROUSEL_DIR:-${CLAUDE_STATUS_DIR:-/tmp/claude-status}}"
SRC_DIR="$STATE_DIR/images/diagrams/src"
mkdir -p "$SRC_DIR"

read -r -d '' guidance <<EOF || true
This session has an image carousel. When a diagram would clarify your
explanation — architecture, data flow, state machines, pipelines, entity
relationships — Write a D2 diagram as a .d2 file and it renders into the
carousel automatically. Write it to: $SRC_DIR/<name>.d2 (an absolute path
outside any repo; never write .d2 files into the working project).
Do NOT diagram trivial or linear one-step things. One diagram per concept.
Prose stays primary — a diagram supplements, never replaces, the explanation.
EOF

jq -nc --arg ctx "$guidance" \
	'{hookSpecificOutput:{hookEventName:"SessionStart",additionalContext:$ctx}}'
