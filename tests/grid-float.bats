#!/usr/bin/env bats
# shellcheck disable=SC2030,SC2031  # bats runs each @test in a subshell; export is intentional
bats_require_minimum_version 1.5.0

setup() {
	APP="$(dirname "$BATS_TEST_DIRNAME")/scripts/tmux-claude-images.sh"
}

# next-3.9 `list-commands new-pane`: has -A (z-order) and -B (border-lines), no -d.
CAPS39='new-pane (newp) [-AbCDefhIkKLMOPvWZ] [-B border-lines] [-c start-directory] [-e environment] [-F format] [-l size] [-m message] [-p percentage] [-s style] [-S active-border-style] [-R inactive-border-style] [-T title] [-x width] [-y height] [-X x-position] [-Y y-position] [-t target-pane] [shell-command [argument ...]]'
# 3.7c `list-commands new-pane`: has -d, no -A, no -B.
CAPS37='new-pane (newp) [-bdefhIklPvZ] [-c start-directory] [-e environment] [-F format] [-l size] [-m message] [-p percentage] [-s style] [-S active-border-style] [-R inactive-border-style] [-t target-pane] [shell-command [argument ...]]'

tmux_stub_setup() {
	export CLAUDE_STATUS_DIR="$BATS_TEST_TMPDIR/state"
	mkdir -p "$CLAUDE_STATUS_DIR/images"
	export TMUX_PANE='%9' TMUX='/tmp/fake-tmux,123,0'
	echo '{"type":"image","path":"/x.png","source":"d2"}' >"$CLAUDE_STATUS_DIR/images/123-9.jsonl"
	STUB="$BATS_TEST_TMPDIR/bin"
	mkdir -p "$STUB"
	printf '#!/usr/bin/env bash\n:\n' >"$STUB/aeye"
	chmod +x "$STUB/aeye"
	export TMUX_LOG="$BATS_TEST_TMPDIR/tmux.log"
	: >"$TMUX_LOG"
	export WIN_DIMS="$BATS_TEST_TMPDIR/dims"
	printf '200 50\n' >"$WIN_DIMS"
	# Routes by first arg; `${GRID:-}`/`${NEWPANE_CAPS:-}`/`${NEWPANE_RC:-}`/
	# `${STUB_EXISTING:-}` are read from the test's own exported env.
	cat >"$STUB/tmux" <<'T'
#!/usr/bin/env bash
echo "$*" >>"$TMUX_LOG"
case "${1:-}" in
list-commands)
	[[ -n "${NEWPANE_CAPS:-}" ]] && printf '%s\n' "$NEWPANE_CAPS"
	exit "${NEWPANE_RC:-0}"
	;;
display-message)
	case "$*" in
	*@crew_grid*) printf '%s\n' "${GRID:-}" ;;
	*window_width*) cat "$WIN_DIMS" ;;
	esac
	;;
list-panes)
	case "$*" in
	*@claude_img_src*) [[ -n "${STUB_EXISTING:-}" ]] && echo '%50 123-9' ;;
	*pane_active*) printf '0 %%9\n1 %%3\n' ;;
	esac
	;;
new-pane | split-window) echo '%77' ;;
esac
exit 0
T
	chmod +x "$STUB/tmux"
	export PATH="$STUB:$PATH"
}

@test "grid float: side geometry, border+z-order flags, no split-window" {
	tmux_stub_setup
	export GRID=1 NEWPANE_CAPS="$CAPS39"
	printf '200 50\n' >"$WIN_DIMS"
	run env -u AEYE_SPLIT -u AEYE_FLOAT bash "$APP"
	[ "$status" -eq 0 ]
	grep -qE '^new-pane -x 50% -y 90% -X 48% -Y 5% -B heavy -A .*-e AEYE_HOST_PANE=%9.*-t %9 -P -F #\{pane_id\}' "$TMUX_LOG"
	grep -q 'set-option .*-t %77 @float_geom 50% 90% 48% 5%' "$TMUX_LOG"
	grep -q 'set-option .*-t %77 remain-on-exit off' "$TMUX_LOG"
	grep -q '@claude_img_src 123-9' "$TMUX_LOG"
	grep -q '@claude_img_axis side' "$TMUX_LOG"
	run ! grep -q '^split-window' "$TMUX_LOG"
}

@test "grid float: bottom geometry on a portrait window" {
	tmux_stub_setup
	export GRID=1 NEWPANE_CAPS="$CAPS39"
	printf '90 50\n' >"$WIN_DIMS"
	run env -u AEYE_SPLIT -u AEYE_FLOAT bash "$APP"
	[ "$status" -eq 0 ]
	grep -qE '^new-pane -x 90% -y 50% -X 5% -Y 48%' "$TMUX_LOG"
	grep -q '@float_geom 90% 50% 5% 48%' "$TMUX_LOG"
	grep -q '@claude_img_axis bottom' "$TMUX_LOG"
}

@test "grid float: caps lacking -A/-B omit both flags but keep sizing" {
	tmux_stub_setup
	export GRID=1 NEWPANE_CAPS="$CAPS37"
	printf '200 50\n' >"$WIN_DIMS"
	run env -u AEYE_SPLIT -u AEYE_FLOAT bash "$APP"
	[ "$status" -eq 0 ]
	line="$(grep '^new-pane' "$TMUX_LOG")"
	[[ $line != *' -A '* && $line != *' -A' ]]
	[[ $line != *' -B '* ]]
	[[ $line == *'-x 50% -y 90%'* ]]
}

@test "grid float: ensure-open without -d support restores prior focus" {
	tmux_stub_setup
	export GRID=1 NEWPANE_CAPS="$CAPS39"
	printf '200 50\n' >"$WIN_DIMS"
	run env -u AEYE_SPLIT -u AEYE_FLOAT bash "$APP" --ensure-open
	[ "$status" -eq 0 ]
	line="$(grep '^new-pane' "$TMUX_LOG")"
	[[ $line != *' -d '* && $line != *' -d' ]]
	awk '/^new-pane/{f=1} f && /^select-pane -t %3/{found=1} END{exit !found}' "$TMUX_LOG"
}

@test "grid float: ensure-open with -d support detaches, no select-pane" {
	tmux_stub_setup
	export GRID=1 NEWPANE_CAPS="$CAPS37"
	printf '200 50\n' >"$WIN_DIMS"
	run env -u AEYE_SPLIT -u AEYE_FLOAT bash "$APP" --ensure-open
	[ "$status" -eq 0 ]
	line="$(grep '^new-pane' "$TMUX_LOG")"
	[[ $line == *' -d '* ]]
	run ! grep -q '^select-pane' "$TMUX_LOG"
}

@test "grid float: no @crew_grid falls back to split-window, no list-commands" {
	tmux_stub_setup
	export GRID='' NEWPANE_CAPS="$CAPS39"
	printf '200 50\n' >"$WIN_DIMS"
	run env -u AEYE_SPLIT -u AEYE_FLOAT bash "$APP"
	[ "$status" -eq 0 ]
	grep -q '^split-window -h ' "$TMUX_LOG"
	run ! grep -q '^new-pane' "$TMUX_LOG"
	run ! grep -q '^list-commands' "$TMUX_LOG"
}

@test "grid float: list-commands failure falls back to split-window" {
	tmux_stub_setup
	export GRID=1 NEWPANE_RC=1
	unset NEWPANE_CAPS
	printf '200 50\n' >"$WIN_DIMS"
	run env -u AEYE_SPLIT -u AEYE_FLOAT bash "$APP"
	[ "$status" -eq 0 ]
	grep -q '^split-window -h ' "$TMUX_LOG"
	run ! grep -q '^new-pane' "$TMUX_LOG"
}

@test "grid float: missing -e support falls back to split-window" {
	tmux_stub_setup
	local_caps="${CAPS39//\[-e environment\] /}"
	export GRID=1 NEWPANE_CAPS="$local_caps"
	printf '200 50\n' >"$WIN_DIMS"
	run env -u AEYE_SPLIT -u AEYE_FLOAT bash "$APP"
	[ "$status" -eq 0 ]
	grep -q '^split-window -h ' "$TMUX_LOG"
	run ! grep -q '^new-pane' "$TMUX_LOG"
}

@test "grid float: an already-open float is killed on toggle-off" {
	tmux_stub_setup
	export GRID=1 NEWPANE_CAPS="$CAPS39" STUB_EXISTING=1
	printf '200 50\n' >"$WIN_DIMS"
	run env -u AEYE_SPLIT -u AEYE_FLOAT bash "$APP"
	[ "$status" -eq 0 ]
	grep -q '^kill-pane -t %50' "$TMUX_LOG"
	run ! grep -q '^new-pane' "$TMUX_LOG"
}

@test "AEYE_FLOAT=always floats outside a crew grid window, no @crew_grid query" {
	tmux_stub_setup
	export GRID='' NEWPANE_CAPS="$CAPS39"
	printf '200 50\n' >"$WIN_DIMS"
	run env -u AEYE_SPLIT AEYE_FLOAT=always bash "$APP"
	[ "$status" -eq 0 ]
	grep -q '^new-pane' "$TMUX_LOG"
	run ! grep -q '^split-window' "$TMUX_LOG"
	run ! grep -q '@crew_grid' "$TMUX_LOG"
}

@test "AEYE_FLOAT=never always splits, no list-commands or @crew_grid query" {
	tmux_stub_setup
	export GRID=1 NEWPANE_CAPS="$CAPS39"
	printf '200 50\n' >"$WIN_DIMS"
	run env -u AEYE_SPLIT AEYE_FLOAT=never bash "$APP"
	[ "$status" -eq 0 ]
	grep -q '^split-window -h ' "$TMUX_LOG"
	run ! grep -q '^new-pane' "$TMUX_LOG"
	run ! grep -q '^list-commands' "$TMUX_LOG"
}

@test "AEYE_FLOAT=garbage behaves like the grid default" {
	tmux_stub_setup
	export GRID='' NEWPANE_CAPS="$CAPS39"
	printf '200 50\n' >"$WIN_DIMS"
	run env -u AEYE_SPLIT AEYE_FLOAT=garbage bash "$APP"
	[ "$status" -eq 0 ]
	grep -q '^split-window -h ' "$TMUX_LOG"
	run ! grep -q '^new-pane' "$TMUX_LOG"

	tmux_stub_setup
	export GRID=1 NEWPANE_CAPS="$CAPS39"
	printf '200 50\n' >"$WIN_DIMS"
	run env -u AEYE_SPLIT AEYE_FLOAT=garbage bash "$APP"
	[ "$status" -eq 0 ]
	grep -q '^new-pane' "$TMUX_LOG"
	run ! grep -q '^split-window' "$TMUX_LOG"
}

@test "AEYE_FLOAT=always still falls back to split-window on an old server" {
	tmux_stub_setup
	export GRID='' NEWPANE_RC=1
	unset NEWPANE_CAPS
	printf '200 50\n' >"$WIN_DIMS"
	run env -u AEYE_SPLIT AEYE_FLOAT=always bash "$APP"
	[ "$status" -eq 0 ]
	grep -q '^split-window -h ' "$TMUX_LOG"
	run ! grep -q '^new-pane' "$TMUX_LOG"
}

@test "float-geom seam: side 200x50" {
	run bash "$APP" --float-geom side 200 50
	[ "$status" -eq 0 ]
	[ "$output" = '50% 90% 48% 5%' ]
}

@test "float-geom seam: side 120x50" {
	run bash "$APP" --float-geom side 120 50
	[ "$output" = '67% 90% 31% 5%' ]
}

@test "float-geom seam: side 60x50 caps at 90" {
	run bash "$APP" --float-geom side 60 50
	[ "$output" = '90% 90% 8% 5%' ]
}

@test "float-geom seam: bottom 90x50" {
	run bash "$APP" --float-geom bottom 90 50
	[ "$output" = '90% 50% 5% 48%' ]
}

@test "float-geom seam: bottom 90x30" {
	run bash "$APP" --float-geom bottom 90 30
	[ "$output" = '90% 80% 5% 18%' ]
}

@test "float-geom seam: unreadable dims fall back to side defaults" {
	run bash "$APP" --float-geom side "" ""
	[ "$output" = '50% 90% 48% 5%' ]
}
