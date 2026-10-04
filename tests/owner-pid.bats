#!/usr/bin/env bats
# shellcheck disable=SC2030,SC2031  # bats runs each @test in a subshell; export is intentional
# The tmux launch path forwards the agent pid to the viewer so the carousel
# closes when its owner exits (#311).
bats_require_minimum_version 1.5.0

setup() {
	APP="$(dirname "$BATS_TEST_DIRNAME")/scripts/tmux-claude-images.sh"
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
	# ${PANE_PID:-} is what `display-message -p '#{pane_pid}'` reports; empty
	# means the pane shell cannot be resolved.
	cat >"$STUB/tmux" <<'T'
#!/usr/bin/env bash
echo "$*" >>"$TMUX_LOG"
case "${1:-}" in
display-message)
	case "$*" in
	*pane_pid*) printf '%s' "${PANE_PID:-}" ;;
	*window_width*) printf '200 50\n' ;;
	esac
	;;
list-panes) : ;;
split-window | new-pane) echo '%77' ;;
esac
exit 0
T
	chmod +x "$STUB/tmux"
	export PATH="$STUB:$PATH"
}

@test "tmux launch passes AEYE_OWNER_PID via -e when the owner resolves" {
	# Stub the pane shell as this test process's own parent, so the launcher's
	# ancestry walk finds a real, live ancestor of the launcher.
	export PANE_PID="$PPID" AEYE_FLOAT=never
	run env -u AEYE_BRIDGED -u AEYE_OWNER_PID bash "$APP"
	[ "$status" -eq 0 ]
	line="$(grep '^split-window' "$TMUX_LOG")"
	[[ $line == *'-e AEYE_OWNER_PID='* ]]
	pid="$(grep -oE 'AEYE_OWNER_PID=[0-9]+' <<<"$line" | cut -d= -f2)"
	[ -n "$pid" ]
	kill -0 "$pid"
}

@test "tmux launch passes no AEYE_OWNER_PID when the pane shell cannot be resolved" {
	export PANE_PID='' AEYE_FLOAT=never
	run env -u AEYE_BRIDGED -u AEYE_OWNER_PID bash "$APP"
	[ "$status" -eq 0 ]
	run ! grep -q 'AEYE_OWNER_PID' "$TMUX_LOG"
}

@test "remote bridge suppresses AEYE_OWNER_PID" {
	# A bridged viewer may run on a different host, where a local pid is useless.
	export PANE_PID="$PPID" AEYE_FLOAT=never
	run env AEYE_BRIDGED=1 bash "$APP"
	[ "$status" -eq 0 ]
	run ! grep -q 'AEYE_OWNER_PID' "$TMUX_LOG"
}
