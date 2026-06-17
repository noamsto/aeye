# Ghostty + WezTerm Launch Modes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the carousel toggle (`scripts/tmux-claude-images.sh`) open the viewer when a coding agent runs in **ghostty** or **wezterm** *without* tmux.

**Architecture:** Add two launch modes to the existing detect-then-`launch_$MODE`-dispatch shell script. `ghostty` opens a separate window (`+new-window` on Linux / `open -na` on macOS) and toggles by `pgrep`+`kill` of the viewer process. `wezterm` opens a real split (`wezterm cli split-pane`) and toggles via a stored pane-id + `kill-pane`. No Go changes — ghostty already renders crisp via the kitty protocol; wezterm correctly falls through to chafa.

**Tech Stack:** Bash (script under test), bats (integration tests with stub binaries on `PATH`), shellcheck/shfmt (pre-commit). Indent everything with **tabs** (shfmt enforces this).

**Spec:** `docs/superpowers/specs/2026-06-17-ghostty-wezterm-launch-design.md` · **Issue:** [#58](https://github.com/noamsto/aeye/issues/58)

---

## File Structure

- **Modify** `scripts/tmux-claude-images.sh`
  - top comment block (lines 2-9) and `resolve_target` doc comment (lines 16-19): document the new modes + an "adding a terminal" contract note.
  - `resolve_target` (lines 20-32): two new `elif` branches.
  - new functions `launch_wezterm` and `launch_ghostty`.
  - `main` (lines 92-103): `none` message + group the session-id guard.
- **Create** `tests/toggle-window.bats` — non-tmux launch tests (the existing `tests/toggle.bats` setup is tmux-only).
- **Modify** `README.md` — feature bullet (lines 30-31) + a new "Terminal support" matrix before `## Architecture` (line 137).

---

## Task 1: Detect ghostty and wezterm hosts

**Files:**
- Create: `tests/toggle-window.bats`
- Modify: `scripts/tmux-claude-images.sh:2-32` and `:92-103`

- [ ] **Step 1: Write the failing resolve tests**

Create `tests/toggle-window.bats` with a clean (non-tmux) setup and three resolve-seam tests. Indent with **tabs** to match the repo.

```bash
#!/usr/bin/env bats

setup() {
	export CLAUDE_STATUS_DIR="$BATS_TEST_TMPDIR/state"
	# Clean slate: no host signals unless a test opts in.
	unset TMUX KITTY_LISTEN_ON WEZTERM_PANE GHOSTTY_RESOURCES_DIR TERM
	export CLAUDE_CODE_SESSION_ID="sess-123"
	mkdir -p "$CLAUDE_STATUS_DIR/images"
	# Non-empty manifest so launch tests get past the "no images yet" guard.
	echo '{"type":"image","path":"/x.png","source":"d2"}' \
		>"$CLAUDE_STATUS_DIR/images/$CLAUDE_CODE_SESSION_ID.jsonl"
	APP="$(dirname "$BATS_TEST_DIRNAME")/scripts/tmux-claude-images.sh"

	STUB_BIN="$BATS_TEST_TMPDIR/bin"
	mkdir -p "$STUB_BIN"
	# Viewer stub so VIEWER_BIN resolves on PATH.
	printf '#!/usr/bin/env bash\n:\n' >"$STUB_BIN/aeye"
	chmod +x "$STUB_BIN/aeye"
	export PATH="$STUB_BIN:$PATH"
}

@test "resolve: wezterm when WEZTERM_PANE set" {
	export WEZTERM_PANE=3
	run bash "$APP" --resolve
	[ "$status" -eq 0 ]
	[ "$(echo "$output" | cut -f1)" = wezterm ]
}

@test "resolve: ghostty when TERM=xterm-ghostty" {
	export TERM=xterm-ghostty
	run bash "$APP" --resolve
	[ "$status" -eq 0 ]
	[ "$(echo "$output" | cut -f1)" = ghostty ]
}

@test "resolve: none when no host present" {
	run bash "$APP" --resolve
	[ "$status" -eq 0 ]
	[ "$(echo "$output" | cut -f1)" = none ]
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `bats tests/toggle-window.bats`
Expected: the wezterm and ghostty tests FAIL (current script resolves them to `none`); the "none" test passes.

- [ ] **Step 3: Add the detection branches and contract comment**

In `scripts/tmux-claude-images.sh`, replace the top comment block (lines 2-9) and `resolve_target` doc comment (lines 16-19) and add the two `elif` branches. The full `resolve_target` becomes:

```bash
# resolve_target sets MODE/KEY/MANIFEST from the environment.
#   MODE=tmux    + KEY=<pane id>        inside tmux
#   MODE=kitty   + KEY=<cc session id>  outside tmux, kitty remote control up
#   MODE=wezterm + KEY=<cc session id>  outside tmux, in wezterm
#   MODE=ghostty + KEY=<cc session id>  outside tmux, in ghostty
#   MODE=none                           no host available
#
# Adding a terminal:
#   1. Detect it here by a distinct env var; set MODE/KEY/MANIFEST.
#   2. Add launch_<mode>(): open-or-toggle the viewer as "$VIEWER_BIN" "$KEY".
#   3. Crisp images need the kitty graphics protocol's UNICODE PLACEHOLDERS
#      (U+10EEEE) — add the $TERM prefix to chooseGridBackend in
#      gallery_render.go. Without them the host falls back to chafa.
resolve_target() {
	if [[ -n ${TMUX:-} ]]; then
		MODE=tmux
		KEY="${TMUX_PANE:-$(tmux display-message -p '#{pane_id}')}"
		MANIFEST="$IMAGES_DIR/${KEY#%}.jsonl"
	elif [[ -n ${KITTY_LISTEN_ON:-} ]]; then
		MODE=kitty
		KEY="${CLAUDE_CODE_SESSION_ID:-}"
		MANIFEST="$IMAGES_DIR/$KEY.jsonl"
	elif [[ -n ${WEZTERM_PANE:-} ]]; then
		MODE=wezterm
		KEY="${CLAUDE_CODE_SESSION_ID:-}"
		MANIFEST="$IMAGES_DIR/$KEY.jsonl"
	elif [[ ${TERM:-} == xterm-ghostty* || -n ${GHOSTTY_RESOURCES_DIR:-} ]]; then
		MODE=ghostty
		KEY="${CLAUDE_CODE_SESSION_ID:-}"
		MANIFEST="$IMAGES_DIR/$KEY.jsonl"
	else
		MODE=none
	fi
}
```

Also update the file's top comment block (lines 2-9) to mention the two new modes — add these two lines after the existing kitty bullet:

```bash
#   - Outside tmux, in wezterm: toggle a real split via `wezterm cli split-pane`.
#   - Outside tmux, in ghostty: toggle a separate window via `ghostty +new-window`
#     (Linux) / `open -na ghostty` (macOS). Keyed by $CLAUDE_CODE_SESSION_ID.
```

- [ ] **Step 4: Wire the session-id guard and `none` message in `main`**

In `main` (lines 92-103), change the `none` message and group the new modes under the existing session-id guard:

```bash
	case $MODE in
	none)
		echo "image carousel needs tmux, kitty remote control, wezterm, or ghostty" >&2
		exit 0
		;;
	kitty | wezterm | ghostty)
		[[ -n $KEY ]] || {
			echo "no CLAUDE_CODE_SESSION_ID; cannot locate images" >&2
			exit 0
		}
		;;
	esac
```

- [ ] **Step 5: Run the resolve tests to verify they pass**

Run: `bats tests/toggle-window.bats`
Expected: all three resolve tests PASS.

- [ ] **Step 6: Lint**

Run: `shellcheck scripts/tmux-claude-images.sh`
Expected: no output (clean).

- [ ] **Step 7: Commit**

```bash
git add scripts/tmux-claude-images.sh tests/toggle-window.bats
git commit -m "feat(toggle): detect ghostty and wezterm hosts (#58)"
```

---

## Task 2: WezTerm split-pane launch mode

**Files:**
- Modify: `scripts/tmux-claude-images.sh` (add `launch_wezterm` after `launch_kitty`)
- Modify: `tests/toggle-window.bats` (add a `wezterm` stub + tests)

- [ ] **Step 1: Add the wezterm stub to the bats setup**

Append to `setup()` in `tests/toggle-window.bats`, before the `export PATH=` line:

```bash
	# wezterm stub: logs args; `cli list` reports a live pane only when
	# $STUB_PANE_ALIVE is set; `cli split-pane` echoes the new pane id.
	export WEZTERM_LOG="$BATS_TEST_TMPDIR/wezterm.log"
	: >"$WEZTERM_LOG"
	cat >"$STUB_BIN/wezterm" <<'STUB'
#!/usr/bin/env bash
echo "$*" >>"$WEZTERM_LOG"
case "$2" in
list) [[ -n ${STUB_PANE_ALIVE:-} ]] && printf 'WINID TABID PANEID\n0 0 %s\n' "$STUB_PANE_ALIVE" || printf 'WINID TABID PANEID\n' ;;
split-pane) echo "${STUB_NEW_PANE:-42}" ;;
*) : ;;
esac
STUB
	chmod +x "$STUB_BIN/wezterm"
```

- [ ] **Step 2: Write the failing wezterm launch tests**

Add to `tests/toggle-window.bats`:

```bash
@test "wezterm: split-pane opens a viewer and records the pane id" {
	export WEZTERM_PANE=3
	unset STUB_PANE_ALIVE
	export STUB_NEW_PANE=77
	run bash "$APP"
	[ "$status" -eq 0 ]
	grep -q split-pane "$WEZTERM_LOG"
	[ "$(cat "$CLAUDE_STATUS_DIR/images/$CLAUDE_CODE_SESSION_ID.wezterm-pane")" = 77 ]
}

@test "wezterm: bare toggle kills the live viewer pane" {
	export WEZTERM_PANE=3
	printf '42\n' >"$CLAUDE_STATUS_DIR/images/$CLAUDE_CODE_SESSION_ID.wezterm-pane"
	export STUB_PANE_ALIVE=42
	run bash "$APP"
	[ "$status" -eq 0 ]
	grep -q "kill-pane --pane-id 42" "$WEZTERM_LOG"
	[ ! -f "$CLAUDE_STATUS_DIR/images/$CLAUDE_CODE_SESSION_ID.wezterm-pane" ]
}

@test "wezterm: --ensure-open with a live pane does not split again" {
	export WEZTERM_PANE=3
	printf '42\n' >"$CLAUDE_STATUS_DIR/images/$CLAUDE_CODE_SESSION_ID.wezterm-pane"
	export STUB_PANE_ALIVE=42
	run bash "$APP" --ensure-open
	[ "$status" -eq 0 ]
	run grep -c split-pane "$WEZTERM_LOG"
	[ "$output" -eq 0 ]
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `bats tests/toggle-window.bats`
Expected: the three wezterm tests FAIL — `launch_wezterm` does not exist, so `"launch_$MODE"` errors (`command not found`).

- [ ] **Step 4: Implement `launch_wezterm`**

In `scripts/tmux-claude-images.sh`, add after `launch_kitty` (after line 83):

```bash
launch_wezterm() {
	# wezterm has a real mux CLI: split-pane returns the new pane id, kill-pane
	# removes it. We persist the id (keyed by session id) for the toggle.
	local panefile="$IMAGES_DIR/$KEY.wezterm-pane" pane=""
	[[ -f $panefile ]] && pane="$(<"$panefile")"
	# Liveness: `wezterm cli list` prints a PANEID column (3rd field; row 1 is the
	# header). A stale id (pane already gone) falls through to a fresh split.
	if [[ -n $pane ]] &&
		wezterm cli list 2>/dev/null | awk -v p="$pane" 'NR>1 && $3==p{f=1} END{exit !f}'; then
		[[ -n $ENSURE_OPEN ]] && return # already open; ensure-open is a no-op
		wezterm cli kill-pane --pane-id "$pane" >/dev/null 2>&1 || true
		rm -f "$panefile"
		return
	fi
	# split-pane defaults its target to $WEZTERM_PANE, so it lands next to the
	# agent. env forwards the state dir — the mux server never saw our env.
	pane="$(wezterm cli split-pane --right --percent 40 --cwd "$STATE_DIR" -- \
		env AEYE_DIR="$STATE_DIR" CLAUDE_STATUS_DIR="$STATE_DIR" "$VIEWER_BIN" "$KEY")"
	printf '%s\n' "$pane" >"$panefile"
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `bats tests/toggle-window.bats`
Expected: all wezterm tests PASS (resolve tests still pass).

- [ ] **Step 6: Lint**

Run: `shellcheck scripts/tmux-claude-images.sh`
Expected: no output.

- [ ] **Step 7: Commit**

```bash
git add scripts/tmux-claude-images.sh tests/toggle-window.bats
git commit -m "feat(toggle): wezterm split-pane launch mode (#58)"
```

---

## Task 3: Ghostty window launch mode

**Files:**
- Modify: `scripts/tmux-claude-images.sh` (add `launch_ghostty` after `launch_wezterm`)
- Modify: `tests/toggle-window.bats` (add `ghostty`/`uname`/`pgrep`/`open` stubs + tests)

- [ ] **Step 1: Add the ghostty/uname/pgrep/open stubs to the bats setup**

Append to `setup()` in `tests/toggle-window.bats`, before the `export PATH=` line:

```bash
	# ghostty + helpers stubs. ghostty/open log args; uname is pinned by
	# $STUB_UNAME; pgrep reports a viewer pid only when $STUB_PGREP_PID is set.
	export GHOSTTY_LOG="$BATS_TEST_TMPDIR/ghostty.log"
	export OPEN_LOG="$BATS_TEST_TMPDIR/open.log"
	: >"$GHOSTTY_LOG"
	: >"$OPEN_LOG"
	printf '#!/usr/bin/env bash\necho "$*" >>"%s"\n' "$GHOSTTY_LOG" >"$STUB_BIN/ghostty"
	printf '#!/usr/bin/env bash\necho "$*" >>"%s"\n' "$OPEN_LOG" >"$STUB_BIN/open"
	printf '#!/usr/bin/env bash\necho "${STUB_UNAME:-Linux}"\n' >"$STUB_BIN/uname"
	cat >"$STUB_BIN/pgrep" <<'STUB'
#!/usr/bin/env bash
[[ -n ${STUB_PGREP_PID:-} ]] && { echo "$STUB_PGREP_PID"; exit 0; }
exit 1
STUB
	chmod +x "$STUB_BIN/ghostty" "$STUB_BIN/open" "$STUB_BIN/uname" "$STUB_BIN/pgrep"
```

- [ ] **Step 2: Write the failing ghostty launch tests**

Add to `tests/toggle-window.bats`:

```bash
@test "ghostty: Linux opens a viewer window via +new-window" {
	export TERM=xterm-ghostty
	unset STUB_PGREP_PID
	export STUB_UNAME=Linux
	run bash "$APP"
	[ "$status" -eq 0 ]
	grep -q -- "+new-window" "$GHOSTTY_LOG"
	grep -q -- "$CLAUDE_CODE_SESSION_ID" "$GHOSTTY_LOG"
}

@test "ghostty: macOS opens a viewer window via open -na ghostty" {
	export TERM=xterm-ghostty
	unset STUB_PGREP_PID
	export STUB_UNAME=Darwin
	run bash "$APP"
	[ "$status" -eq 0 ]
	grep -q -- "-na ghostty" "$OPEN_LOG"
	grep -q -- "$CLAUDE_CODE_SESSION_ID" "$OPEN_LOG"
}

@test "ghostty: --ensure-open with a live viewer does not spawn" {
	export TERM=xterm-ghostty
	export STUB_PGREP_PID=4242
	run bash "$APP" --ensure-open
	[ "$status" -eq 0 ]
	[ ! -s "$GHOSTTY_LOG" ]
}

@test "ghostty: bare toggle kills the live viewer process" {
	export TERM=xterm-ghostty
	sleep 300 &
	pid=$!
	export STUB_PGREP_PID=$pid
	run bash "$APP"
	[ "$status" -eq 0 ]
	wait "$pid" 2>/dev/null || true
	run kill -0 "$pid"
	[ "$status" -ne 0 ]
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `bats tests/toggle-window.bats`
Expected: the four ghostty tests FAIL — `launch_ghostty` does not exist.

- [ ] **Step 4: Implement `launch_ghostty`**

In `scripts/tmux-claude-images.sh`, add after `launch_wezterm`:

```bash
launch_ghostty() {
	# ghostty has no window query/close IPC (+close is unshipped), so toggle on
	# the viewer process itself: it runs as `"$VIEWER_BIN" "$KEY"` and $KEY is the
	# unique CC session id, so pgrep matches exactly our viewer.
	local pids
	pids="$(pgrep -f "$VIEWER_BIN $KEY" 2>/dev/null || true)"
	if [[ -n $pids ]]; then
		[[ -n $ENSURE_OPEN ]] && return # already open; ensure-open is a no-op
		# shellcheck disable=SC2086 # pgrep may return several pids; split intentionally
		kill $pids 2>/dev/null || true # viewer exit closes its ghostty window
		return
	fi
	# env forwards the state dir (D-Bus/new instance never saw our env);
	# --working-directory is explicit to dodge the 1.3.0 -e working-dir bug.
	local cmd=(env AEYE_DIR="$STATE_DIR" CLAUDE_STATUS_DIR="$STATE_DIR" "$VIEWER_BIN" "$KEY")
	case "$(uname -s)" in
	Darwin) open -na ghostty --args --working-directory="$STATE_DIR" -e "${cmd[@]}" ;;
	*) ghostty +new-window --working-directory="$STATE_DIR" -e "${cmd[@]}" ;;
	esac
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `bats tests/toggle-window.bats`
Expected: all tests PASS (resolve + wezterm + ghostty).

- [ ] **Step 6: Lint**

Run: `shellcheck scripts/tmux-claude-images.sh`
Expected: no output.

- [ ] **Step 7: Commit**

```bash
git add scripts/tmux-claude-images.sh tests/toggle-window.bats
git commit -m "feat(toggle): ghostty window launch mode (#58)"
```

---

## Task 4: README terminal-support matrix

**Files:**
- Modify: `README.md:30-31` (feature bullet) and before `:137` (`## Architecture`)

- [ ] **Step 1: Broaden the dual-mode feature bullet**

Replace lines 30-31:

```markdown
- 🔭 **Dual-mode rendering** — a tmux split or a kitty window, auto-detected from
  the host. Opens beside your agent, not wherever you happened to navigate.
```

with:

```markdown
- 🔭 **Multi-host rendering** — a tmux split, a kitty/wezterm split, or a ghostty
  window, auto-detected from the host. Opens beside your agent, not wherever you
  happened to navigate. See [Terminal support](#terminal-support).
```

- [ ] **Step 2: Add the Terminal support section**

Insert immediately before `## Architecture` (line 137):

```markdown
## Terminal support

Two things vary by host: **where** the viewer opens, and **how sharp** the images
are. Crisp rendering needs the kitty graphics protocol's *unicode placeholders*
(U+10EEEE); everything else uses the [`chafa`](https://hpjansson.org/chafa/)
block-art fallback.

| Host (no tmux) | Window placement | Image quality |
|----------------|------------------|---------------|
| **kitty** | split beside the agent (`kitty @ launch`) | crisp |
| **ghostty** | separate window (`ghostty +new-window` / `open -na`) | crisp |
| **wezterm** | split beside the agent (`wezterm cli split-pane`) | chafa\* |
| **Alacritty / Warp / other** | — (use tmux) | chafa |

Inside **tmux** the viewer always opens as a split, on any host. WezTerm speaks
the kitty protocol but not its unicode placeholders ([wezterm#986](https://github.com/wezterm/wezterm/issues/986)),
so it uses chafa today and would upgrade automatically if that lands.

\* Crisp real-pixel rendering on wezterm/iTerm2 (sixel / OSC 1337) is tracked
separately.
```

- [ ] **Step 3: Verify the markdown links/anchors and typos hook**

Run: `git add README.md && pre-commit run --files README.md`
Expected: `typos` and whitespace hooks PASS.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: terminal support matrix for ghostty/wezterm (#58)"
```

---

## Task 5: Full verification

- [ ] **Step 1: Run the whole bats suite**

Run: `bats tests/`
Expected: all files pass, including the existing `tests/toggle.bats` (unchanged tmux behaviour) and the new `tests/toggle-window.bats`.

- [ ] **Step 2: Run Go tests (sanity — no Go changed)**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 3: Final lint**

Run: `shellcheck scripts/tmux-claude-images.sh`
Expected: no output.

- [ ] **Step 4: Manual verification note (cannot be automated here)**

No ghostty/wezterm binary exists in this environment or CI, so the launch paths
are stub-tested only. Record in the PR body that real-host verification (open +
toggle in ghostty-no-tmux and wezterm-no-tmux, Linux and macOS) is pending a
manual check.

- [ ] **Step 5: Open the PR**

```bash
git push -u origin feat/58-ghostty-wezterm
gh pr create --assignee @me --title "feat: open the carousel under ghostty and wezterm (no tmux)" \
	--body "Closes #58. Adds wezterm (split) and ghostty (window) launch modes to the toggle script; no Go changes. Stub-tested; real-host manual verification pending."
```
