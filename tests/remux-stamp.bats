#!/usr/bin/env bats
# shellcheck disable=SC2030,SC2031  # bats runs each @test in a subshell; export is intentional
# The tmux viewer pane stamps tmux-remux's @remux_relaunch (+ _pinned) so a
# restored carousel reattaches to its session's manifest (#402). Real tmux, but
# only on a private socket — never the live server.
bats_require_minimum_version 1.5.0

setup() {
	APP="$(dirname "$BATS_TEST_DIRNAME")/scripts/tmux-claude-images.sh"
	unset TMUX TMUX_PANE
	export TMUX_TMPDIR="$BATS_TEST_TMPDIR/tmx"
	mkdir -p "$TMUX_TMPDIR"
	SOCK="aeye-t-$$-$BATS_TEST_NUMBER"
	T=(tmux -L "$SOCK" -f /dev/null)
	export AEYE_DIR="$BATS_TEST_TMPDIR/state"
	IMAGES="$AEYE_DIR/images"
	mkdir -p "$IMAGES"
	STUB="$BATS_TEST_TMPDIR/bin"
	mkdir -p "$STUB"
	# Viewer stub: records argv/env, then lingers until $STUB/stop appears.
	cat >"$STUB/aeye" <<'V'
#!/usr/bin/env bash
{
	printf 'argv=%s\n' "$*"
	printf 'owner=%s\n' "${AEYE_OWNER:-}"
	printf 'ownerpid=%s\n' "${AEYE_OWNER_PID-unset}"
	printf 'hostpane=%s\n' "${AEYE_HOST_PANE-unset}"
} >"${VIEWER_OUT:-/dev/null}"
for _ in $(seq 1200); do [[ -e ${VIEWER_STOP:-/nonexistent} ]] && break; sleep 0.1; done
V
	chmod +x "$STUB/aeye"
	export VIEWER_OUT="$BATS_TEST_TMPDIR/viewer.out" VIEWER_STOP="$BATS_TEST_TMPDIR/stop"
	"${T[@]}" new-session -d -s main -x 200 -y 50 "sleep 600"
	"${T[@]}" set-option -g remain-on-exit on
	PANE="$("${T[@]}" display-message -p -t main '#{pane_id}')"
	SRV="$("${T[@]}" display-message -p '#{pid}')"
	TMUX="$("${T[@]}" display-message -p '#{socket_path},#{pid},#{session_id}')"
	export TMUX
	export PATH="$STUB:$PATH"
}

teardown() {
	touch "$VIEWER_STOP"
	"${T[@]}" kill-server 2>/dev/null || true
}

seed() { # KEY SESSION
	echo '{"type":"image","path":"/x.png","source":"d2"}' >"$IMAGES/$1.jsonl"
	printf '%s' "$2" >"$IMAGES/$1.owner"
}

# opt PANE NAME -> the pane option's value ("" when unset)
opt() { "${T[@]}" display-message -p -t "$1" "#{$2}"; }

viewer_pane() { "${T[@]}" list-panes -t main -F '#{pane_id} #{@claude_img_src}' | awk -v k="$1" '$2==k{print $1}'; }

@test "launch stamps @remux_relaunch and the pin; the viewer's exit clears them" {
	seed "$SRV-${PANE#%}" sess-A
	TMUX_PANE="$PANE" CLAUDE_CODE_SESSION_ID=sess-A AEYE_FLOAT=never run bash "$APP"
	[ "$status" -eq 0 ]
	v="$(viewer_pane "$SRV-${PANE#%}")"
	[ -n "$v" ]
	# The pane command stamps itself, so wait for it to run.
	for _ in $(seq 50); do
		[ -n "$(opt "$v" @remux_relaunch)" ] && break
		sleep 0.1
	done
	stamp="$(opt "$v" @remux_relaunch)"
	[[ $stamp == *"--attach sess-A"* ]]
	[[ $stamp == *"--bin $STUB/aeye"* ]]
	[[ $stamp != "env "* ]]
	[ "$(opt "$v" @remux_relaunch_pinned)" = 1 ]

	touch "$VIEWER_STOP"
	for _ in $(seq 50); do
		[ "$(opt "$v" pane_dead)" = 1 ] && break
		sleep 0.1
	done
	[ "$(opt "$v" @remux_relaunch)" = "" ]
	[ "$(opt "$v" @remux_relaunch_pinned)" = "" ]
}

@test "launch without a session id stamps nothing" {
	seed "$SRV-${PANE#%}" sess-A
	TMUX_PANE="$PANE" AEYE_FLOAT=never run env -u CLAUDE_CODE_SESSION_ID -u AEYE_SESSION_ID bash "$APP"
	[ "$status" -eq 0 ]
	v="$(viewer_pane "$SRV-${PANE#%}")"
	[ -n "$v" ]
	[ "$(opt "$v" @remux_relaunch)" = "" ]
	[ "$(opt "$v" @remux_relaunch_pinned)" = "" ]
}

@test "outside tmux nothing is stamped" {
	seed sess-A sess-A
	AEYE_HOST=none run env -u TMUX -u TMUX_PANE CLAUDE_CODE_SESSION_ID=sess-A bash "$APP"
	[ "$status" -eq 0 ]
	[ "$("${T[@]}" show-options -p -t "$PANE" @remux_relaunch)" = "" ]
}

@test "--attach resolves the new server's key by session, re-stamps, runs the viewer, then clears" {
	true 3>&- &
	dead=$!
	wait "$dead" 2>/dev/null || true
	seed 111-5 sess-A
	seed "$SRV-${PANE#%}" sess-A
	printf '%s' "$$" >"$IMAGES/$SRV-${PANE#%}.ownerpid"
	touch -d '1 hour ago' "$IMAGES/111-5.jsonl"

	TMUX_PANE="$PANE" AEYE_ATTACH_WAIT=0 bash "$APP" --attach sess-A --bin "$STUB/aeye" --dir "$AEYE_DIR" 3>&- &
	pid=$!
	for _ in $(seq 50); do
		[ -s "$VIEWER_OUT" ] && break
		sleep 0.1
	done
	grep -qx "argv=$SRV-${PANE#%}" "$VIEWER_OUT"
	grep -qx "owner=sess-A" "$VIEWER_OUT"
	grep -qx "ownerpid=$$" "$VIEWER_OUT"
	grep -qx "hostpane=$PANE" "$VIEWER_OUT"
	[ "$(opt "$PANE" @claude_img_src)" = "$SRV-${PANE#%}" ]
	stamp="$(opt "$PANE" @remux_relaunch)"
	[[ $stamp == *"--attach sess-A"* && $stamp != "env "* ]]
	[ "$(opt "$PANE" @remux_relaunch_pinned)" = 1 ]

	touch "$VIEWER_STOP"
	wait "$pid"
	[ "$(opt "$PANE" @remux_relaunch)" = "" ]
	[ "$(opt "$PANE" @remux_relaunch_pinned)" = "" ]
}

@test "--attach falling back to a dead-server key does not export a dead owner pid" {
	true 3>&- &
	dead=$!
	wait "$dead" 2>/dev/null || true
	seed 111-5 sess-A
	printf '%s' "$dead" >"$IMAGES/111-5.ownerpid"
	touch "$VIEWER_STOP"

	TMUX_PANE="$PANE" AEYE_ATTACH_WAIT=0 AEYE_OWNER_PID=999999 AEYE_HOST_PANE=%77 \
		run bash "$APP" --attach sess-A --bin "$STUB/aeye" --dir "$AEYE_DIR"
	[ "$status" -eq 0 ]
	grep -qx "argv=111-5" "$VIEWER_OUT"
	grep -qx "ownerpid=unset" "$VIEWER_OUT"
	grep -qx "hostpane=unset" "$VIEWER_OUT"
}

@test "--attach with no manifest for the session clears the stamp and exits 0" {
	"${T[@]}" set-option -p -t "$PANE" @remux_relaunch "stale"
	"${T[@]}" set-option -p -t "$PANE" @remux_relaunch_pinned 1
	TMUX_PANE="$PANE" AEYE_ATTACH_WAIT=0 run bash "$APP" --attach sess-none --bin "$STUB/aeye" --dir "$AEYE_DIR"
	[ "$status" -eq 0 ]
	[ "$(opt "$PANE" @remux_relaunch)" = "" ]
	[ "$(opt "$PANE" @remux_relaunch_pinned)" = "" ]
}

@test "--attach waits for the new server's key and runs the viewer on it, not the old one" {
	seed 111-5 sess-A
	TMUX_PANE="$PANE" AEYE_ATTACH_WAIT=5 bash "$APP" --attach sess-A --bin "$STUB/aeye" --dir "$AEYE_DIR" 3>&- &
	pid=$!
	sleep 1
	[ ! -s "$VIEWER_OUT" ]
	seed "$SRV-${PANE#%}" sess-A
	for _ in $(seq 50); do
		[ -s "$VIEWER_OUT" ] && break
		sleep 0.1
	done
	grep -qx "argv=$SRV-${PANE#%}" "$VIEWER_OUT"
	touch "$VIEWER_STOP"
	wait "$pid"
}
