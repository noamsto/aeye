#!/usr/bin/env bats

setup() {
	ROOT="$(dirname "$(dirname "$BATS_TEST_DIRNAME")")"
	export PLUGIN_ROOT="$ROOT/adapters/cursor"
	APP="$PLUGIN_ROOT/scripts/diagrams.sh"

	export AEYE_DIR="$BATS_TEST_TMPDIR/state"
	export TMUX_PANE="%7"
	export TMUX="fake,4242,0" # pane ids are per server; pin one so the key is stable
	MANIFEST="$AEYE_DIR/images/4242-7.jsonl"
	DIAGRAMS="$AEYE_DIR/images/diagrams"
	DOTD2="$BATS_TEST_TMPDIR/flow.d2"
	printf 'a -> b\n' >"$DOTD2"

	# Stub aeye the same way the Codex adapter's diagrams.bats does: the whole
	# d2-compile/fix-fonts/contrast/resvg pipeline lives inside the real binary
	# (covered by Go tests), so fake it here — write a png + sibling svg, log
	# args, honor AEYE_RENDER_FAIL, and emit a <foreignObject> svg for |md
	# sources so the hook's markdown guard can fire.
	STUB_BIN="$BATS_TEST_TMPDIR/bin"
	mkdir -p "$STUB_BIN"
	cat >"$STUB_BIN/aeye" <<'STUB'
#!/usr/bin/env bash
[[ ${1:-} == render-diagram ]] || exit 0
echo "$*" >>"$RENDER_LOG"
[[ -n ${AEYE_RENDER_FAIL:-} ]] && {
	echo "${AEYE_RENDER_FAIL_MSG:-render boom}" >&2
	exit 1
}
in="$2"
out="$3"
if grep -q '|md' "$in" 2>/dev/null; then
	printf '<svg><foreignObject><div>md</div></foreignObject></svg>' >"${out%.png}.svg"
else
	printf '<svg/>' >"${out%.png}.svg"
fi
printf 'PNG' >"$out"
STUB
	cat >"$STUB_BIN/tmux-claude-images" <<'STUB'
#!/usr/bin/env bash
echo "$*" >>"$TOGGLE_LOG"
STUB
	chmod +x "$STUB_BIN/aeye" "$STUB_BIN/tmux-claude-images"
	export TOGGLE_LOG="$BATS_TEST_TMPDIR/toggle.log"
	export RENDER_LOG="$BATS_TEST_TMPDIR/render.log"
	: >"$TOGGLE_LOG"
	: >"$RENDER_LOG"
	# Prefer the stubs even when the ambient shell exports AEYE_BIN / AEYE_TOGGLE
	# (d2_render uses ${AEYE_BIN:-aeye}; diagrams.sh uses ${AEYE_TOGGLE:-tmux-claude-images}).
	export AEYE_BIN="$STUB_BIN/aeye"
	export AEYE_TOGGLE="$STUB_BIN/tmux-claude-images"
	export PATH="$STUB_BIN:$PATH"
}

write_d2_payload() {
	local path="${1:-$DOTD2}"
	jq -nc --arg p "$path" --arg wr "$(dirname "$path")" \
		'{conversation_id:"conv-1",session_id:"conv-1",tool_name:"Write",tool_input:{file_path:$p},tool_output:"{\"success\":true}",cwd:"",workspace_roots:[$wr],hook_event_name:"postToolUse"}'
}

run_app() {
	write_d2_payload "$DOTD2" | bash "$APP"
}

@test "Write of a .d2 renders a png and appends one manifest line" {
	run_app
	[ -f "$MANIFEST" ]
	run wc -l <"$MANIFEST"
	[ "$output" -eq 1 ]
	run jq -r '.source' "$MANIFEST"
	[ "$output" = "d2" ]
	png="$(jq -r '.path' "$MANIFEST")"
	[ -f "$png" ]
	[[ $png == "$DIAGRAMS"/*.png ]]
}

@test "renders both theme variants" {
	run_app
	png="$(jq -r '.path' "$MANIFEST")"
	run grep -cx "render-diagram $DOTD2 ${png%-dark.png}-light.png" "$RENDER_LOG"
	[ "$output" -eq 1 ]
	run grep -cx "render-diagram $DOTD2 $png" "$RENDER_LOG"
	[ "$output" -eq 1 ]
}

@test "duplicate Write of identical .d2 -> one manifest line" {
	run_app
	run_app
	run wc -l <"$MANIFEST"
	[ "$output" -eq 1 ]
}

@test "editing the .d2 supersedes the prior render: one line, old files pruned" {
	run_app
	old="$(jq -r '.path' "$MANIFEST")"
	printf 'a -> b -> c\n' >"$DOTD2"
	run_app
	new="$(jq -r '.path' "$MANIFEST")"
	run wc -l <"$MANIFEST"
	[ "$output" -eq 1 ]
	[ "$old" != "$new" ]
	[ -f "$new" ]
	[ ! -f "$old" ]
}

@test "a Write of a .png is ignored (that's images.sh's job)" {
	PNG="$BATS_TEST_TMPDIR/pic.png"
	printf 'x' >"$PNG"
	write_d2_payload "$PNG" | bash "$APP"
	[ ! -f "$MANIFEST" ]
}

@test "render-diagram failure -> skip, log to render-errors.log, no manifest line" {
	# shellcheck disable=SC2030,SC2031
	export AEYE_RENDER_FAIL=1
	run run_app
	[ "$status" -eq 0 ]
	[ ! -f "$MANIFEST" ]
	[ -f "$DIAGRAMS/render-errors.log" ]
}

@test "a compile failure warns the agent with the d2 error, opens no carousel" {
	# shellcheck disable=SC2030,SC2031
	export AEYE_RENDER_FAIL=1
	# shellcheck disable=SC2030,SC2031
	export AEYE_RENDER_FAIL_MSG="flow.d2:3:9: missing value after colon"
	run run_app
	[ "$status" -eq 0 ]
	# Cursor's hook contract is a bare additional_context, not Claude's wrapper
	ctx="$(jq -r '.additional_context' <<<"$output")"
	[[ $ctx == *flow.d2* ]]
	[[ $ctx == *"FAILED to compile"* ]]
	[[ $ctx == *"missing value after colon"* ]]
	[ ! -f "$MANIFEST" ]
	[ ! -s "$TOGGLE_LOG" ]
}

@test 'a $-substitution failure adds the one-backslash hint' {
	# shellcheck disable=SC2030,SC2031
	export AEYE_RENDER_FAIL=1
	# shellcheck disable=SC2030,SC2031
	export AEYE_RENDER_FAIL_MSG="flow.d2:142:61: substitutions must begin on {"
	run run_app
	ctx="$(jq -r '.additional_context' <<<"$output")"
	[[ $ctx == *'ONE backslash'* ]]
	[[ $ctx == *'\$'* ]]
}

@test "a markdown node (<foreignObject>) is suppressed: warned, logged, not shown" {
	printf 'a: "ok"\nb: |md\n  blank\n|\na -> b\n' >"$DOTD2"
	run run_app
	[ "$status" -eq 0 ]
	# Cursor shape — never Claude/Codex hookSpecificOutput.
	ctx="$(jq -r '.additional_context' <<<"$output")"
	[[ $ctx == *"|md"* ]]
	[[ $ctx == *BLANK* ]]
	run jq -e 'has("hookSpecificOutput") | not' <<<"$output"
	[ "$status" -eq 0 ]
	run grep -c 'WARN markdown' "$DIAGRAMS/render-errors.log"
	[ "$output" -ge 1 ]
	[ ! -f "$MANIFEST" ]
	[ ! -s "$TOGGLE_LOG" ]
	[ -z "$(ls "$DIAGRAMS"/*.png 2>/dev/null)" ]
}

@test "a new diagram opens the carousel via AEYE_TOGGLE" {
	run_app
	run grep -c -- '--ensure-open' "$TOGGLE_LOG"
	[ "$output" -eq 1 ]
}

@test "an unchanged diagram does not re-open the carousel" {
	run_app
	run_app
	run grep -c -- '--ensure-open' "$TOGGLE_LOG"
	[ "$output" -eq 1 ]
}
