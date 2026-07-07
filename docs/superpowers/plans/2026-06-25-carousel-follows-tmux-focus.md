# Carousel Follows tmux Focus — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** In kitty-pane mode, the carousel tracks the visible tmux window — shown beside the tmux host when its owning pane's window is on screen, stashed (state preserved) otherwise.

**Architecture:** A self-gating `--reconcile` action in aeye's launcher diffs `kitty @ ls` carousel windows against the visible tmux window's panes, stashing/unstashing via kitty remote control. tmux `set-hook`s in lazytmux drive it on visible-window changes. kitty RC is enabled in nix-config. Stash mechanism is decided by a spike (A: live detach-window to a hidden tab; B fallback: kill + persist/restore viewer state).

**Tech Stack:** bash (POSIX-ish, `set -euo pipefail`), kitty remote control (`kitty @`), tmux hooks, `jq`, `bats` (stubbed `kitty`/`tmux`), Nix (kitty + tmux config), Go (only if fallback B).

**Spec:** `docs/superpowers/specs/2026-06-25-carousel-follows-tmux-focus-design.md` · Issue #100

---

## File Structure

- `nix-config: home/.../kitty config` — enable `allow_remote_control` + `listen_on` (prereq).
- `nix-config: lazytmux tmux config consumer / kitty` — `update-environment KITTY_LISTEN_ON`.
- `aeye: scripts/tmux-claude-images.sh` — refactor placement into a helper; add `--reconcile` action + stash/unstash + lock + gate.
- `aeye: tests/carousel-reconcile.bats` — reconcile unit tests with stubbed `kitty`/`tmux`.
- `lazytmux: config/tmux.conf.nix` — `set-hook`s calling the reconcile action.
- `aeye: gallery_zoom.go / new gallery_state.go` — per-pane view-state persist/restore (**only if spike picks B**).

---

## Task 1: Spike — decide the stash mechanism (manual, gates Task 4)

**Files:** none (throwaway shell exploration). Record the outcome in the spec's §4.

This needs a live kitty with RC reachable from tmux. If RC isn't enabled yet, enable it ad-hoc for the spike: add `allow_remote_control yes` and `listen_on unix:/tmp/aeye-kitty-spike` to a scratch kitty launched as `kitty -o allow_remote_control=yes --listen-on unix:/tmp/aeye-kitty-spike`, run tmux inside it, and `export KITTY_LISTEN_ON=unix:/tmp/aeye-kitty-spike` in the tmux pane.

- [ ] **Step 1: Launch a tagged window beside the tmux host**

```bash
kitty @ launch --type=window --location=vsplit --keep-focus --var claude_img_src=spike -- sh -c 'echo spike; sleep 600'
kitty @ ls | jq '[.. | objects | select(.user_vars.claude_img_src=="spike") | {id, tab: input_line_number}]'
```
Expected: one window with `user_vars.claude_img_src=spike` in the active tab.

- [ ] **Step 2: Create a stash tab and move the window into it**

```bash
kitty @ launch --type=tab --var aeye_stash=1 --keep-focus -- sh -c 'sleep 600'   # lazily create stash tab
kitty @ detach-window --match var:claude_img_src=spike --target-tab var:aeye_stash=1
kitty @ ls | jq '.. | objects | select(.user_vars.claude_img_src=="spike") | .id'   # still alive?
```
Expected: window still exists (process alive), now in the stash tab; the active tab no longer shows it.

- [ ] **Step 3: Bring it back and re-place it as a vsplit beside the host**

```bash
# back to the active (host) tab:
kitty @ detach-window --match var:claude_img_src=spike --target-tab id:<HOST_TAB_ID>
# can we re-vsplit it beside the host window, or does it only append?
kitty @ resize-window ... ; kitty @ goto-layout splits ; # experiment
```
Expected (PASS = approach A): the window returns to the host tab and can be positioned as a vsplit beside the tmux host (process state intact). FAIL = it only appends without controllable placement, or detach loses split positioning.

- [ ] **Step 4: Record the decision**

Edit the spec §4: note **A** (with the exact working `detach-window`/placement commands) or **B** (repositioning not controllable → fall back). Commit:

```bash
git add docs/superpowers/specs/2026-06-25-carousel-follows-tmux-focus-design.md
git commit -m "docs(specs): record carousel-stash spike outcome (approach A|B)"
```

> **Gate:** Task 4 implements the recorded approach. Tasks 2, 3, 5 are independent of the outcome.

---

## Task 2: kitty RC prerequisite (nix-config)

**Files:**
- Modify: nix-config kitty config module (the file with `programs.kitty.settings`)
- Modify: kitty/tmux glue so tmux panes inherit `KITTY_LISTEN_ON`

- [ ] **Step 1: Enable kitty remote control with a stable socket**

In the kitty settings module, add:
```nix
programs.kitty.settings = {
  allow_remote_control = "yes";
  listen_on = "unix:/tmp/kitty-\${USER}";   # stable per-user socket
};
```
(Escape `${USER}` per the surrounding Nix string style; if the module uses an attrset of strings, use `"unix:/tmp/kitty"` with no interpolation rather than guessing.)

- [ ] **Step 2: Propagate the socket into tmux**

Ensure tmux carries `KITTY_LISTEN_ON` to new panes. In the tmux config (lazytmux consumer or nix-config tmux extraConfig):
```tmux
set-option -ga update-environment " KITTY_LISTEN_ON"
```

- [ ] **Step 3: Stage, build, verify**

```bash
git add <kitty-module> <tmux-config>
just check-staged
nh home build .
```
Expected: build succeeds.

- [ ] **Step 4: Switch and verify RC reachable from tmux** (manual)

After `! nh home switch ~/nix-config`, open kitty → tmux pane:
```bash
echo $KITTY_LISTEN_ON          # non-empty
kitty @ ls >/dev/null && echo "RC OK from tmux"
```
Expected: `RC OK from tmux`.

- [ ] **Step 5: Commit** (on a nix-config branch, per repo rules)

```bash
git commit -m "feat(kitty): enable remote control + stable listen_on; propagate KITTY_LISTEN_ON into tmux"
```

---

## Task 3: Refactor placement into a shared helper (aeye)

**Files:**
- Modify: `scripts/tmux-claude-images.sh` (`launch_kitty`, ~lines 112-136)
- Test: `tests/host-mode.bats` (existing kitty stub already present)

- [ ] **Step 1: Extract `place_kitty_beside_host`**

Pull the placement block out of `launch_kitty` into a function that echoes the `kitty @ launch`/placement args for a given window id, so both `launch_kitty` and the unstash branch (Task 4) call it:

```bash
# Echoes the kitty placement args (vsplit beside the tmux-hosting window),
# honoring KITTY_WINDOW_ID when present, else the active window.
kitty_place_args() {
	if [[ -n ${KITTY_WINDOW_ID:-} ]]; then
		kitty @ goto-layout --match "window_id:$KITTY_WINDOW_ID" splits >/dev/null 2>&1 || true
		printf '%s\0' --match "window_id:$KITTY_WINDOW_ID" --location=vsplit --next-to "id:$KITTY_WINDOW_ID" --keep-focus
	else
		kitty @ goto-layout splits >/dev/null 2>&1 || true
		printf '%s\0' --location=vsplit --keep-focus
	fi
}
```
Have `launch_kitty` read it: `mapfile -d '' placement < <(kitty_place_args)`.

- [ ] **Step 2: Run existing host-mode tests**

Run: `direnv exec . bats tests/host-mode.bats`
Expected: PASS (behavior unchanged — pure refactor).

- [ ] **Step 3: Commit**

```bash
git add scripts/tmux-claude-images.sh
git commit -m "refactor(launcher): extract kitty_place_args from launch_kitty"
```

---

## Task 4: The `--reconcile` action (aeye) — TDD

**Files:**
- Modify: `scripts/tmux-claude-images.sh` (arg parse + `reconcile()`)
- Test: `tests/carousel-reconcile.bats` (new)

The bats file stubs `kitty` and `tmux` like `tests/host-mode.bats`. The stub `kitty` records its args to `$KITTY_LOG` and answers `@ ls` from a fixture JSON; the stub `tmux` answers `list-panes` from a fixture. Each test asserts on the `detach-window` calls the reconcile emits.

- [ ] **Step 1: Failing test — non-kitty mode is a no-op**

```bash
@test "reconcile is a no-op unless AEYE_HOST=kitty" {
	unset AEYE_HOST
	run bash "$APP" --reconcile
	[ "$status" -eq 0 ]
	[ ! -s "$KITTY_LOG" ]   # never touched kitty
}
```
Run: `direnv exec . bats tests/carousel-reconcile.bats -f no-op`
Expected: FAIL (`--reconcile` unknown).

- [ ] **Step 2: Add arg parse + gate**

In the arg handling, add `--reconcile) ACTION=reconcile ;;` and:
```bash
reconcile() {
	[[ ${AEYE_HOST:-} == kitty ]] || return 0
	command -v kitty >/dev/null 2>&1 || return 0
	kitty @ ls >/dev/null 2>&1 || return 0
	exec 9>"$STATE_DIR/.carousel-reconcile.lock"
	flock -n 9 || return 0
	_reconcile_apply
}
```
Run the test → PASS.

- [ ] **Step 3: Failing test — off-screen carousel is stashed**

```bash
@test "a carousel whose pane is not in the visible window is stashed" {
	export AEYE_HOST=kitty
	# stub tmux: visible window has pane %5 only
	echo '%5' >"$VISIBLE_PANES"
	# stub kitty @ ls: one carousel window for pane %9, in the active (non-stash) tab
	cp "$FIXTURES/ls-active-9.json" "$KITTY_LS_JSON"
	run bash "$APP" --reconcile
	[ "$status" -eq 0 ]
	grep -q 'detach-window --match var:claude_img_src=%9 --target-tab var:aeye_stash=1' "$KITTY_LOG"
}
```
Run → FAIL.

- [ ] **Step 4: Implement `_reconcile_apply` (approach A — adjust per spike)**

```bash
_reconcile_apply() {
	local visible window_panes paneid tab
	# panes of the attached client's active window
	window_panes="$(tmux list-panes -t "$(tmux display-message -p '#{window_id}')" -F '#{pane_id}' 2>/dev/null)"
	# enumerate carousel windows: "paneid<TAB>is_stashed"
	kitty @ ls 2>/dev/null | jq -r '
		.. | objects | select(.user_vars?.claude_img_src) |
		[ .user_vars.claude_img_src, (.user_vars.aeye_stash // "" | length>0) ] | @tsv' |
	while IFS=$'\t' read -r paneid stashed; do
		if grep -qxF "$paneid" <<<"$window_panes"; then
			[[ $stashed == true ]] && _unstash "$paneid"      # visible window -> show
		else
			[[ $stashed == true ]] || _stash "$paneid"        # off-screen -> stash
		fi
	done
}
_stash() { kitty @ detach-window --match "var:claude_img_src=$1" --target-tab var:aeye_stash=1 >/dev/null 2>&1 || true; }
```
(Determining `stashed` by a per-window `aeye_stash` var set when stashing; the stash tab is created lazily on first `_stash` — confirm exact create+target flags from the Task 1 spike.)
Run the stash test → PASS.

- [ ] **Step 5: Failing test — visible carousel is unstashed and re-placed**

```bash
@test "a stashed carousel whose pane is in the visible window is brought back" {
	export AEYE_HOST=kitty
	echo '%9' >"$VISIBLE_PANES"
	cp "$FIXTURES/ls-stashed-9.json" "$KITTY_LS_JSON"
	run bash "$APP" --reconcile
	grep -q 'detach-window --match var:claude_img_src=%9' "$KITTY_LOG"   # moved back to host tab
}
```
Run → FAIL.

- [ ] **Step 6: Implement `_unstash` using the shared placement helper**

```bash
_unstash() {
	# move back to the host tab, then re-place as a vsplit beside the host
	kitty @ detach-window --match "var:claude_img_src=$1" --target-tab var:aeye_host=1 >/dev/null 2>&1 || true
	# re-vsplit beside host — exact command set finalized by the Task 1 spike
	local args; mapfile -d '' args < <(kitty_place_args)
	kitty @ ... # reposition the existing window per spike outcome
}
```
Run → PASS.

- [ ] **Step 7: Failing test — idempotent re-run mutates nothing**

```bash
@test "re-running reconcile in a steady state makes no kitty mutations" {
	export AEYE_HOST=kitty
	echo '%9' >"$VISIBLE_PANES"
	cp "$FIXTURES/ls-active-9.json" "$KITTY_LS_JSON"   # already correct: visible + not stashed
	run bash "$APP" --reconcile
	! grep -q 'detach-window' "$KITTY_LOG"
}
```
Run → confirm the `[[ $stashed == true ]]` guards already make it PASS; if not, add the skip.

- [ ] **Step 8: Run the whole reconcile suite + shellcheck**

```bash
direnv exec . bats tests/carousel-reconcile.bats
direnv exec . shellcheck scripts/tmux-claude-images.sh
```
Expected: all PASS, shellcheck clean.

- [ ] **Step 9: Commit**

```bash
git add scripts/tmux-claude-images.sh tests/carousel-reconcile.bats tests/fixtures/ls-*.json
git commit -m "feat(launcher): --reconcile stashes/unstashes carousels to the visible tmux window"
```

---

## Task 5: tmux hooks drive the reconcile (lazytmux)

**Files:**
- Modify: `lazytmux: config/tmux.conf.nix` (near the existing `set-hook` block + the `nBind`/toggle wiring)

- [ ] **Step 1: Bind reconcile on visible-window-change hooks**

Where the toggle binary is wired (`n-toggle != null`), reuse the **same binary the
existing `prefix+I` bind (`nBind`) invokes** — the reconcile is just a flag on that
same script — and append `--reconcile`. Using the `nBind` path expression verbatim
(here written `${TOGGLE_BIN}` for the exact `${n-toggle}/bin/<name>` it already uses):
```tmux
set-hook -g client-session-changed 'run-shell -b "${TOGGLE_BIN} --reconcile"'
set-hook -g session-window-changed 'run-shell -b "${TOGGLE_BIN} --reconcile"'
set-hook -g client-attached       'run-shell -b "${TOGGLE_BIN} --reconcile"'
```
`-b` runs in the background so focus changes never block. The reconcile self-gates to
kitty mode, so this is inert for tmux-split users.

- [ ] **Step 2: Verify the hooks emit only when the toggle is wired**

Run lazytmux's tmux-config test/snapshot (or `nix build` the config) and confirm the `set-hook` lines appear only under `n-toggle != null`.
Expected: present when wired, absent otherwise.

- [ ] **Step 3: Commit** (lazytmux branch)

```bash
git commit -m "feat(tmux): reconcile aeye carousel on visible-window change"
```

---

## Task 6: Viewer state persist/restore (aeye Go) — ONLY IF spike picked B

**Files:**
- Create: `gallery_state.go`
- Modify: `gallery.go` (load state on init), signal handling (save on SIGTERM/exit)
- Test: `gallery_state_test.go`

- [ ] **Step 1: Failing test — round-trip cursor/zoom for a pane**

```go
func TestViewStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := viewState{Cursor: 3, Zoom: 2, Region: "r1"}
	saveViewState(dir, "%9", want)
	got := loadViewState(dir, "%9")
	if got != want { t.Fatalf("got %+v want %+v", got, want) }
}
```
Run: `go test ./... -run TestViewStateRoundTrip`
Expected: FAIL.

- [ ] **Step 2: Implement save/load (JSON at `<state>/images/<pane>.view.json`)**

```go
type viewState struct{ Cursor, Zoom int; Region string }
func saveViewState(dir, pane string, s viewState) { /* json.Marshal -> os.WriteFile */ }
func loadViewState(dir, pane string) viewState   { /* read+unmarshal, zero value on miss */ }
```
Run → PASS.

- [ ] **Step 3: Wire into the viewer** — load on startup (apply cursor/zoom/region), save on exit/SIGTERM. Change `_stash` (Task 4) to `kitty @ close-window` and `_unstash` to `launch_kitty` (which now restores state).

- [ ] **Step 4: Test + commit**

```bash
go test ./...
git add gallery_state.go gallery_state_test.go gallery.go
git commit -m "feat(viewer): persist per-pane view state for stash-by-close"
```

---

## Task 7: Integration smoke test (manual)

- [ ] **Step 1:** `AEYE_HOST=kitty`, open Claude in tmux window A, `prefix+I` → carousel splits beside it.
- [ ] **Step 2:** Switch to tmux window B (no carousel) → carousel stashes (vanishes). Switch back to A → it returns (approach A: instant, state intact; B: brief re-render, state restored).
- [ ] **Step 3:** Switch tmux session → same stash/restore behavior.
- [ ] **Step 4:** Focus a sibling shell pane in window A → carousel stays (window-level granularity).
- [ ] **Step 5:** Drag an image out of the (now kitty-native) carousel → native OSC 72 drop works.

---

## Rollout order

Task 2 (prereq, unblocks the spike) → Task 1 (spike, picks A/B) → Task 3 → Task 4 → Task 5 → Task 6 (only if B) → Task 7. aeye and lazytmux changes land as their own PRs; nix-config on its own branch.
