#!/usr/bin/env bash
# Usage: backfill-calls.sh <session_file_path> <cwd> <session_id>
#
# Rebuilds the normalized calls.jsonl that scripts/session-backfill.sh expects
# from pi's on-disk JSONL session transcript, and writes it to stdout.
#
# The transcript is a parent-linked tree, not a flat log: every line is a JSON
# object with `id`, `parentId`, and other fields, and only some lines have
# `type == "message"` — but any line type (session, model_change,
# thinking_level_change, custom_message, ...) can be the parent of a message
# line. So the active branch is found by starting at the last message line (by
# file order) and walking `parentId` back to the root, then keeping only the
# message-typed entries in that chain. Anything not on that chain (an
# abandoned branch from an earlier fork) is dropped.
#
# toolResult/assistant/bashExecution role handling mirrors pi's own message
# shapes in the on-disk transcript; no bashExecution role has been observed on
# this machine, so that branch is exercised only by the bats fixture below.
set -euo pipefail

session_file="${1:-}"
cwd="${2:-}"
session_id="${3:-}"

[[ -n $session_file && -s $session_file ]] || exit 0

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT

# --- Every line into an id -> line map, skipping blank/unparsable lines. ---
valid_lines="$scratch/valid.jsonl"
: >"$valid_lines"
while IFS= read -r line || [[ -n $line ]]; do
	[[ -n $line ]] || continue
	jq -e . >/dev/null 2>&1 <<<"$line" || continue
	printf '%s\n' "$line" >>"$valid_lines"
done <"$session_file"

[[ -s $valid_lines ]] || exit 0

id_map="$scratch/by-id.json"
jq -s 'map(select(.id != null)) | INDEX(.id)' "$valid_lines" >"$id_map"

# --- Head of the active branch: the last message-typed line, by file order. ---
last_id="$(jq -rs '[.[] | select(.type == "message")] | last | .id // empty' "$valid_lines")"
[[ -n $last_id ]] || exit 0

# --- Walk parentId back to the root, collecting ids tail-to-root. ---
chain_ids="$scratch/chain-ids.txt"
: >"$chain_ids"
cur="$last_id"
max_steps="$(wc -l <"$valid_lines")"
steps=0
while [[ -n $cur ]]; do
	jq -e --arg id "$cur" 'has($id)' "$id_map" >/dev/null 2>&1 || break
	printf '%s\n' "$cur" >>"$chain_ids"
	cur="$(jq -r --arg id "$cur" '.[$id].parentId // empty' "$id_map")"
	steps=$((steps + 1))
	# A parentId cycle would otherwise loop forever; the chain can't legitimately
	# revisit more ids than exist in the transcript.
	((steps <= max_steps)) || break
done

# --- Reverse to chronological (root-to-tail) order, keep only message entries. ---
chain_messages="$scratch/chain-messages.jsonl"
jq -c --slurpfile idmap "$id_map" -R -s '
	split("\n") | map(select(length > 0)) | reverse
	| map($idmap[0][.])
	| map(select(.type == "message"))
	| .[]
' "$chain_ids" >"$chain_messages"

# --- Replay the chain: toolResult records feed the following assistant toolCall's
# tool_response; bashExecution emits its own call directly. ---
jq -c --slurp --arg cwd "$cwd" --arg sid "$session_id" '
	def text_blocks(content):
		if (content | type) == "array" then
			[content[] | select(type == "object" and .type == "text") | (.text // "")] | join("\n")
		elif (content | type) == "string" then content
		else "" end;

	# Two passes, mirroring historicalCalls(): a toolResult can only be looked up
	# by the assistant toolCall it answers once the whole chain has been scanned,
	# since the result always comes *after* the call it responds to.
	. as $entries
	| (reduce $entries[] as $entry (
		{};
		($entry.message // {}) as $m
		| if $m.role == "toolResult" then
			. + {($m.toolCallId // "_"): text_blocks($m.content)}
		else . end
	)) as $results
	| $entries[]
	| (.message // {}) as $m
	| if $m.role == "assistant" then
		($m.content // [])[]
		| select(type == "object" and .type == "toolCall" and .name != null)
		| {
			tool_name: .name,
			tool_input: (.arguments // {}),
			tool_response: {content: [($results[(.id // "_")] // "")]},
			cwd: $cwd,
			session_id: $sid
		}
	elif $m.role == "bashExecution" then
		{
			tool_name: "bash",
			tool_input: {command: ($m.command // "")},
			tool_response: {content: [($m.output // "")]},
			cwd: $cwd,
			session_id: $sid
		}
	else empty end
	| select((.tool_name // "" | ascii_downcase) as $t | ["read", "write", "edit", "bash", "shell"] | index($t) != null)
' "$chain_messages"
