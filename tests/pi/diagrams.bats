#!/usr/bin/env bats

setup() {
	ROOT="$(dirname "$(dirname "$BATS_TEST_DIRNAME")")"
	export PLUGIN_ROOT="$ROOT/adapters/pi"
	APP="$PLUGIN_ROOT/scripts/diagrams.sh"

	export AEYE_DIR="$BATS_TEST_TMPDIR/state"
	export TMUX_PANE="%7"
	export TMUX="fake,4242,0" # pane ids are per server; pin one so the key is stable
	MANIFEST="$AEYE_DIR/images/4242-7.jsonl"
	DIAGRAMS="$AEYE_DIR/images/diagrams"
	SRC="$DIAGRAMS/src"
	mkdir -p "$SRC"
	DOTD2="$SRC/flow.d2"
	printf 'a -> b\n' >"$DOTD2"

	# Stub aeye: the d2-compile/fix-fonts/contrast/resvg pipeline lives in the
	# real binary (Go-tested), so fake it — write a png + sibling svg, log args,
	# honor AEYE_RENDER_FAIL, and emit a <foreignObject> svg for |md sources so
	# the hook's markdown guard can fire.
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
if grep -q 'BADSYNTAX' "$in" 2>/dev/null; then
	echo "$(basename "$in"):1:1: substitutions must begin on {" >&2
	exit 1
fi
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
if [[ -n ${TOGGLE_FAIL:-} ]]; then
	echo "${TOGGLE_FAIL_MSG:-toggle boom}" >&2
	exit "$TOGGLE_FAIL"
fi
STUB
	chmod +x "$STUB_BIN/aeye" "$STUB_BIN/tmux-claude-images"
	export TOGGLE_LOG="$BATS_TEST_TMPDIR/toggle.log"
	export RENDER_LOG="$BATS_TEST_TMPDIR/render.log"
	: >"$TOGGLE_LOG"
	: >"$RENDER_LOG"
	export PATH="$STUB_BIN:$PATH"
}

run_app() { # $1 = .d2 path
	local payload
	payload="$(jq -nc --arg p "$1" --arg c "$BATS_TEST_TMPDIR" \
		'{tool_name:"write",tool_input:{file_path:$p},tool_response:{content:[""]},cwd:$c,session_id:"sess-A"}')"
	printf '%s' "$payload" | bash "$APP"
}

@test "writing a .d2 renders a png and appends one manifest line" {
	run_app "$DOTD2"
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
	run_app "$DOTD2"
	png="$(jq -r '.path' "$MANIFEST")"
	run grep -cx "render-diagram $DOTD2 ${png%-dark.png}-light.png" "$RENDER_LOG"
	[ "$output" -eq 1 ]
	run grep -cx "render-diagram $DOTD2 $png" "$RENDER_LOG"
	[ "$output" -eq 1 ]
}

@test "duplicate write of identical .d2 -> one manifest line" {
	run_app "$DOTD2"
	run_app "$DOTD2"
	run wc -l <"$MANIFEST"
	[ "$output" -eq 1 ]
}

@test "editing the .d2 supersedes the prior render: one line, old files pruned" {
	run_app "$DOTD2"
	old="$(jq -r '.path' "$MANIFEST")"
	printf 'a -> b -> c\n' >"$DOTD2"
	run_app "$DOTD2"
	new="$(jq -r '.path' "$MANIFEST")"
	run wc -l <"$MANIFEST"
	[ "$output" -eq 1 ]
	[ "$old" != "$new" ]
	[ -f "$new" ]
	[ ! -f "$old" ]
}

@test "editing a .d2 keeps its manifest slot; a new diagram appends (#339)" {
	run_app "$DOTD2"
	OTHER="$SRC/other.d2"
	printf 'x -> y\n' >"$OTHER"
	run_app "$OTHER"
	# the new diagram appended at the end
	run jq -r '.name' "$MANIFEST"
	[ "$output" = $'flow\nother' ]
	# edit flow: it must keep slot 0 rather than moving to the end
	printf 'a -> b -> c\n' >"$DOTD2"
	run_app "$DOTD2"
	run jq -r '.name' "$MANIFEST"
	[ "$output" = $'flow\nother' ]
	run wc -l <"$MANIFEST"
	[ "$output" -eq 2 ]
}

@test "a .png write is ignored (that's images.sh's job)" {
	PNG="$BATS_TEST_TMPDIR/pic.png"
	printf 'x' >"$PNG"
	run_app "$PNG"
	[ ! -f "$MANIFEST" ]
}

@test "render-diagram failure -> skip, log to render-errors.log, no manifest line" {
	# shellcheck disable=SC2030,SC2031
	export AEYE_RENDER_FAIL=1
	run run_app "$DOTD2"
	[ "$status" -eq 0 ]
	[ ! -f "$MANIFEST" ]
	[ -f "$DIAGRAMS/render-errors.log" ]
}

@test "a compile failure warns the agent with the d2 error, opens no carousel" {
	# shellcheck disable=SC2030,SC2031
	export AEYE_RENDER_FAIL=1
	# shellcheck disable=SC2030,SC2031
	export AEYE_RENDER_FAIL_MSG="flow.d2:3:9: missing value after colon"
	run run_app "$DOTD2"
	[ "$status" -eq 0 ]
	ctx="$(jq -r '.hookSpecificOutput.additionalContext' <<<"$output")"
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
	run run_app "$DOTD2"
	ctx="$(jq -r '.hookSpecificOutput.additionalContext' <<<"$output")"
	[[ $ctx == *'ONE backslash'* ]]
	[[ $ctx == *'\$'* ]]
}

@test "a markdown node (<foreignObject>) is suppressed: warned, logged, not shown" {
	printf 'a: "ok"\nb: |md\n  blank\n|\na -> b\n' >"$DOTD2"
	run run_app "$DOTD2"
	[ "$status" -eq 0 ]
	ctx="$(jq -r '.hookSpecificOutput.additionalContext' <<<"$output")"
	[[ $ctx == *"|md"* ]]
	[[ $ctx == *BLANK* ]]
	run grep -c 'WARN markdown' "$DIAGRAMS/render-errors.log"
	[ "$output" -ge 1 ]
	[ ! -f "$MANIFEST" ]
	[ ! -s "$TOGGLE_LOG" ]
	[ -z "$(ls "$DIAGRAMS"/*.png 2>/dev/null)" ]
}

@test "a new diagram opens the carousel via AEYE_TOGGLE" {
	run_app "$DOTD2"
	run grep -c -- '--ensure-open' "$TOGGLE_LOG"
	[ "$output" -eq 1 ]
}

@test "an unchanged diagram does not re-open the carousel" {
	run_app "$DOTD2"
	run_app "$DOTD2"
	run grep -c -- '--ensure-open' "$TOGGLE_LOG"
	[ "$output" -eq 1 ]
}

@test "a .d2 outside the source dir renders but is not appended (#271)" {
	OUTSIDE="$BATS_TEST_TMPDIR/scratch.d2"
	printf 'a -> b\n' >"$OUTSIDE"
	run run_app "$OUTSIDE"
	[ "$status" -eq 0 ]
	# it still renders — only adoption is contained
	run grep -c "render-diagram $OUTSIDE" "$RENDER_LOG"
	[ "$output" -eq 2 ]
	[ ! -f "$MANIFEST" ]
	[ ! -s "$TOGGLE_LOG" ]
}

@test "a .d2 escaping src through a '..' component renders but is not appended (#271)" {
	ESCAPE="$DIAGRAMS/scratch.d2"
	printf 'a -> b\n' >"$ESCAPE"
	run run_app "$SRC/../scratch.d2"
	[ "$status" -eq 0 ]
	run grep -c "render-diagram $SRC/../scratch.d2" "$RENDER_LOG"
	[ "$output" -eq 2 ]
	[ ! -f "$MANIFEST" ]
}

@test "a content-differing *-check.d2 copy renders but leaves one entry for the real source (#271)" {
	run_app "$DOTD2"
	printf 'direction: right\na -> b: one\n' >"$SRC/flow-check.d2"
	run_app "$SRC/flow-check.d2"
	run grep -c "render-diagram $SRC/flow-check.d2" "$RENDER_LOG"
	[ "$output" -eq 2 ]
	run wc -l <"$MANIFEST"
	[ "$output" -eq 1 ]
	run jq -r '.name' "$MANIFEST"
	[ "$output" = "flow" ]
}

@test "a byte-identical *-check.d2 copy leaves one entry for the real source (#271)" {
	run_app "$DOTD2"
	cp "$DOTD2" "$SRC/flow-check.d2"
	run_app "$SRC/flow-check.d2"
	run wc -l <"$MANIFEST"
	[ "$output" -eq 1 ]
	run jq -r '.name' "$MANIFEST"
	[ "$output" = "flow" ]
}

# Replays a hookyard-shaped envelope through the real handler. pi names the
# target `path` (not `file_path`) for write and edit alike.
run_handler() { # $1 = tool_name  $2 = tool_input JSON
	local payload
	payload="$(jq -nc --arg t "$1" --argjson i "$2" --arg c "$BATS_TEST_TMPDIR" \
		'{tool_name:$t,tool_input:$i,native:{tool_response:{content:[{type:"text",text:"ok"}]}},cwd:$c,session_id:"sess-A"}')"
	printf '%s' "$payload" | bash "$PLUGIN_ROOT/handlers/tool-result.sh"
}

pi_write() { run_handler Write "$(jq -nc --arg p "$1" --arg c "${2:-a -> b}" '{path:$p,content:$c}')"; }
pi_edit() { run_handler edit "$(jq -nc --arg p "$1" '{path:$p,edits:[{oldText:"a",newText:"z"}]}')"; }

@test "pi write (tool_input.path) of a new .d2 renders, adopts and opens the carousel" {
	run pi_write "$DOTD2"
	[ "$status" -eq 0 ]
	run wc -l <"$MANIFEST"
	[ "$output" -eq 1 ]
	[ -f "$(jq -r '.path' "$MANIFEST")" ]
	run grep -c -- '--ensure-open' "$TOGGLE_LOG"
	[ "$output" -eq 1 ]
}

@test "pi overwrite of an adopted .d2 replaces its entry and prunes the old png" {
	run_app "$DOTD2"
	old="$(jq -r '.path' "$MANIFEST")"
	printf 'a -> b -> c\n' >"$DOTD2"
	run pi_write "$DOTD2" "a -> b -> c"
	[ "$status" -eq 0 ]
	new="$(jq -r '.path' "$MANIFEST")"
	run wc -l <"$MANIFEST"
	[ "$output" -eq 1 ]
	[ "$old" != "$new" ]
	[ -f "$new" ]
	[ ! -f "$old" ]
}

@test "pi edit (tool_input.path + edits) of an adopted .d2 re-renders it" {
	run_app "$DOTD2"
	old="$(jq -r '.path' "$MANIFEST")"
	printf 'z -> b\n' >"$DOTD2"
	run pi_edit "$DOTD2"
	[ "$status" -eq 0 ]
	new="$(jq -r '.path' "$MANIFEST")"
	run wc -l <"$MANIFEST"
	[ "$output" -eq 1 ]
	[ "$old" != "$new" ]
	[ -f "$new" ]
	[ ! -f "$old" ]
}

@test "a failing ensure-open is logged with its exit status and stderr, hook still exits 0" {
	# shellcheck disable=SC2030,SC2031
	export TOGGLE_FAIL=3
	# shellcheck disable=SC2030,SC2031
	export TOGGLE_FAIL_MSG="no tmux client attached"
	run pi_write "$DOTD2"
	[ "$status" -eq 0 ]
	run wc -l <"$MANIFEST"
	[ "$output" -eq 1 ]
	run grep -F 'ensure-open' "$DIAGRAMS/render-errors.log"
	[ "$status" -eq 0 ]
	[[ $output == *flow* ]]
	[[ $output == *"exit=3"* ]]
	[[ $output == *"no tmux client attached"* ]]
}

@test "an ensure-open that exits 0 but writes stderr is still logged" {
	# shellcheck disable=SC2030,SC2031
	export TOGGLE_FAIL=0
	# shellcheck disable=SC2030,SC2031
	export TOGGLE_FAIL_MSG="image carousel needs tmux"
	run pi_write "$DOTD2"
	[ "$status" -eq 0 ]
	run grep -F 'ensure-open' "$DIAGRAMS/render-errors.log"
	[ "$status" -eq 0 ]
	[[ $output == *"exit=0"* ]]
	[[ $output == *"image carousel needs tmux"* ]]
}

@test "a succeeding ensure-open writes nothing to render-errors.log" {
	run pi_write "$DOTD2"
	[ "$status" -eq 0 ]
	run grep -c -- '--ensure-open' "$TOGGLE_LOG"
	[ "$output" -eq 1 ]
	run wc -l <"$MANIFEST"
	[ "$output" -eq 1 ]
	[ ! -s "$DIAGRAMS/render-errors.log" ]
}
