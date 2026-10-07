#!/usr/bin/env bats

setup() {
	export CLAUDE_STATUS_DIR="$BATS_TEST_TMPDIR/state"
	export TMUX_PANE="%7"
	export TMUX="fake,4242,0"
	unset CLAUDE_CODE_SESSION_ID AEYE_DIR
	MANIFEST="$CLAUDE_STATUS_DIR/images/4242-7.jsonl"
	DIAGRAMS="$CLAUDE_STATUS_DIR/images/diagrams"
	SRC="$DIAGRAMS/src"
	mkdir -p "$SRC"
	printf 'a -> b\n' >"$SRC/flow.d2"
	APP="$(dirname "$BATS_TEST_DIRNAME")/adapters/core/publish-diagram.sh"

	STUB_BIN="$BATS_TEST_TMPDIR/bin"
	mkdir -p "$STUB_BIN"
	cat >"$STUB_BIN/aeye" <<'STUB'
#!/usr/bin/env bash
[[ ${1:-} == render-diagram ]] || exit 0
[[ -n ${AEYE_RENDER_FAIL:-} ]] && {
	echo "d2 boom" >&2
	exit 1
}
printf '<svg/>' >"${3%.png}.svg"
printf 'PNG' >"$3"
STUB
	cat >"$STUB_BIN/tmux-claude-images" <<'STUB'
#!/usr/bin/env bash
echo "$*" >>"$TOGGLE_LOG"
STUB
	chmod +x "$STUB_BIN/aeye" "$STUB_BIN/tmux-claude-images"
	export TOGGLE_LOG="$BATS_TEST_TMPDIR/toggle.log"
	: >"$TOGGLE_LOG"
	export PATH="$STUB_BIN:$PATH"
}

@test "publishes both renders and one manifest entry under the pane key" {
	run bash "$APP" "$SRC/flow.d2"
	[ "$status" -eq 0 ]
	[ -z "$output" ]
	[ "$(wc -l <"$MANIFEST")" -eq 1 ]
	png="$(jq -r .path "$MANIFEST")"
	[ "$(jq -r .name "$MANIFEST")" = flow ]
	[ -f "$png" ]
	[ -f "${png%.png}.svg" ]
	[ -f "${png/-dark/-light}" ]
	[ ! -s "$TOGGLE_LOG" ]
}

@test "--pane overrides TMUX_PANE for the manifest key" {
	run bash "$APP" "$SRC/flow.d2" %9
	[ "$status" -eq 0 ]
	[ -f "$CLAUDE_STATUS_DIR/images/4242-9.jsonl" ]
	[ ! -e "$MANIFEST" ]
}

@test "republishing replaces the entry in place and drops the old render" {
	printf 'x -> y\n' >"$SRC/other.d2"
	bash "$APP" "$SRC/flow.d2"
	bash "$APP" "$SRC/other.d2"
	old="$(jq -r 'select(.name=="flow") | .path' "$MANIFEST")"
	printf 'a -> c\n' >"$SRC/flow.d2"
	run bash "$APP" "$SRC/flow.d2"
	[ "$status" -eq 0 ]
	[ "$(wc -l <"$MANIFEST")" -eq 2 ]
	[ "$(jq -r .name "$MANIFEST" | head -1)" = flow ]
	new="$(jq -r 'select(.name=="flow") | .path' "$MANIFEST")"
	[ "$new" != "$old" ]
	[ -f "$new" ]
	[ ! -e "$old" ]
}

@test "a render failure exits non-zero with the d2 error and keeps the previous entry" {
	bash "$APP" "$SRC/flow.d2"
	before="$(cat "$MANIFEST")"
	printf 'a -> broken\n' >"$SRC/flow.d2"
	AEYE_RENDER_FAIL=1 run bash "$APP" "$SRC/flow.d2"
	[ "$status" -ne 0 ]
	[[ $output == *"d2 boom"* ]]
	[ "$(cat "$MANIFEST")" = "$before" ]
	[ -f "$(jq -r .path "$MANIFEST")" ]
}

@test "refuses a .d2 outside the diagrams src dir" {
	printf 'a -> b\n' >"$BATS_TEST_TMPDIR/stray.d2"
	run bash "$APP" "$BATS_TEST_TMPDIR/stray.d2"
	[ "$status" -ne 0 ]
	[ ! -e "$MANIFEST" ]
}

@test "--open runs the toggle's ensure-open, and only then" {
	run bash "$APP" "$SRC/flow.d2" "" 1
	[ "$status" -eq 0 ]
	[ "$(cat "$TOGGLE_LOG")" = "--ensure-open" ]
}

@test "concurrent publishes of one file leave one valid entry" {
	for _ in 1 2 3 4 5 6; do bash "$APP" "$SRC/flow.d2" & done
	wait
	[ "$(wc -l <"$MANIFEST")" -eq 1 ]
	jq -e . "$MANIFEST" >/dev/null
	[ -f "$(jq -r .path "$MANIFEST")" ]
}

@test "outside tmux, --pane resolves the server pid from the pane" {
	unset TMUX
	printf '#!/usr/bin/env bash\necho 4242\n' >"$STUB_BIN/tmux"
	chmod +x "$STUB_BIN/tmux"
	run bash "$APP" "$SRC/flow.d2" %5
	[ "$status" -eq 0 ]
	[ -f "$CLAUDE_STATUS_DIR/images/4242-5.jsonl" ]
}

@test "outside tmux with an unknown pane fails instead of writing an unreadable key" {
	unset TMUX
	printf '#!/usr/bin/env bash\nexit 1\n' >"$STUB_BIN/tmux"
	chmod +x "$STUB_BIN/tmux"
	run bash "$APP" "$SRC/flow.d2" %5
	[ "$status" -ne 0 ]
	[ -z "$(ls "$CLAUDE_STATUS_DIR"/images/*.jsonl 2>/dev/null)" ]
}

@test "replacing one entry keeps the render a same-content sibling still uses" {
	cp "$SRC/flow.d2" "$SRC/twin.d2"
	bash "$APP" "$SRC/flow.d2"
	bash "$APP" "$SRC/twin.d2"
	shared="$(jq -r 'select(.name=="twin") | .path' "$MANIFEST")"
	printf 'a -> z\n' >"$SRC/flow.d2"
	bash "$APP" "$SRC/flow.d2"
	[ -f "$shared" ]
}
