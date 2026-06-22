# Configurable Kitty-Pane Launch Mode — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let `AEYE_HOST=kitty` make a kitty-in-tmux user open the viewer in a native kitty split (where OSC 72 drag works), with a graceful tmux-split fallback.

**Architecture:** All changes are in the launcher `scripts/tmux-claude-images.sh`. `resolve_target()` gains an `AEYE_HOST` override and derives the manifest `KEY` from the environment the same way the capture hook does (so capture and viewer never disagree). `launch_kitty()` gains an in-tmux placement path and a reachability fallback. Tests are bats, using the existing `--resolve` seam and PATH-stubbed `tmux`/`kitty`.

**Tech Stack:** Bash, bats, kitty remote control (`kitty @`), tmux.

**Spec:** `docs/superpowers/specs/2026-06-21-kitty-pane-launch-mode-design.md` · **Issue:** [#90](https://github.com/noamsto/aeye/issues/90)

---

## File Structure

- **Modify `scripts/tmux-claude-images.sh`** — `resolve_target()` (override + key decoupling), `launch_kitty()` (in-tmux placement + fallback).
- **Create `tests/host-mode.bats`** — resolution + fallback tests, own setup with `tmux`/`kitty`/`aeye` stubs.
- **Modify `README.md`** — "Enable kitty-pane mode" setup section.

## Conventions

- Run bats inside the devshell: `direnv exec . bats tests/host-mode.bats`.
- Run `shellcheck` on the script after editing: `direnv exec . shellcheck scripts/tmux-claude-images.sh` — must be clean (project rule).
- Commit per task.

---

### Task 1: `AEYE_HOST` override + key decoupling in `resolve_target`

**Files:**
- Create: `tests/host-mode.bats`
- Modify: `scripts/tmux-claude-images.sh` (`resolve_target`, ~lines 33-58)

- [ ] **Step 1: Write the failing tests** — create `tests/host-mode.bats`:

```bash
#!/usr/bin/env bats

setup() {
	export CLAUDE_STATUS_DIR="$BATS_TEST_TMPDIR/state"
	mkdir -p "$CLAUDE_STATUS_DIR/images"
	APP="$(dirname "$BATS_TEST_DIRNAME")/scripts/tmux-claude-images.sh"

	# Simulate being inside tmux (most tests force kitty from here).
	export TMUX="/tmp/fake-tmux-socket"
	export TMUX_PANE="%7"
	# Non-empty manifest keyed by the pane (7), so launches proceed past the guard.
	echo '{"type":"image","path":"/x.png","source":"d2"}' >"$CLAUDE_STATUS_DIR/images/7.jsonl"

	STUB_BIN="$BATS_TEST_TMPDIR/bin"
	mkdir -p "$STUB_BIN"
	export TMUX_LOG="$BATS_TEST_TMPDIR/tmux.log" KITTY_LOG="$BATS_TEST_TMPDIR/kitty.log"
	: >"$TMUX_LOG"; : >"$KITTY_LOG"

	cat >"$STUB_BIN/tmux" <<'STUB'
#!/usr/bin/env bash
echo "$*" >>"$TMUX_LOG"
case "$1" in
list-panes) : ;;            # no existing viewer
split-window) echo '%99' ;;
*) : ;;
esac
STUB
	# kitty stub: bare `@ ls` = reachability probe (exit $STUB_KITTY_REACHABLE,
	# default 0 = up); `@ ls --match` = toggle check (exit $STUB_KITTY_MATCH,
	# default 1 = no existing viewer); everything else logs and succeeds.
	cat >"$STUB_BIN/kitty" <<'STUB'
#!/usr/bin/env bash
echo "$*" >>"$KITTY_LOG"
case "$*" in
"@ ls") exit "${STUB_KITTY_REACHABLE:-0}" ;;
"@ ls --match"*) exit "${STUB_KITTY_MATCH:-1}" ;;
*) exit 0 ;;
esac
STUB
	printf '#!/usr/bin/env bash\n:\n' >"$STUB_BIN/aeye"
	chmod +x "$STUB_BIN/tmux" "$STUB_BIN/kitty" "$STUB_BIN/aeye"
	export PATH="$STUB_BIN:$PATH"
}

@test "AEYE_HOST=kitty in tmux forces kitty mode but keeps the tmux pane KEY" {
	export AEYE_HOST=kitty
	run bash "$APP" --resolve
	[ "$status" -eq 0 ]
	[ "$(printf '%s' "$output" | cut -f1)" = "kitty" ]
	[ "$(printf '%s' "$output" | cut -f2)" = "%7" ]
}

@test "AEYE_HOST unset in tmux resolves to tmux mode" {
	unset AEYE_HOST
	run bash "$APP" --resolve
	[ "$(printf '%s' "$output" | cut -f1)" = "tmux" ]
}

@test "AEYE_HOST is honored over auto-detection" {
	export AEYE_HOST=ghostty
	run bash "$APP" --resolve
	[ "$(printf '%s' "$output" | cut -f1)" = "ghostty" ]
}

@test "bare kitty (no tmux) keys the manifest by session id" {
	unset AEYE_HOST TMUX TMUX_PANE
	export KITTY_LISTEN_ON="unix:/tmp/x" CLAUDE_CODE_SESSION_ID="sess123"
	run bash "$APP" --resolve
	[ "$(printf '%s' "$output" | cut -f1)" = "kitty" ]
	[ "$(printf '%s' "$output" | cut -f2)" = "sess123" ]
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `direnv exec . bats tests/host-mode.bats`
Expected: FAIL — `AEYE_HOST=kitty` currently still resolves `MODE=tmux` (override not implemented), so the first and third tests fail.

- [ ] **Step 3: Implement** — replace the whole `resolve_target()` in `scripts/tmux-claude-images.sh` with:

```bash
resolve_target() {
	# Key the manifest exactly as the capture hook (adapters/.../images.sh) does —
	# pane id inside tmux, else the Claude session id — INDEPENDENT of launch MODE.
	# That way capture and viewer always read the same file even when AEYE_HOST
	# sends a tmux user down the kitty launch path.
	KEY="${TMUX_PANE:-${CLAUDE_CODE_SESSION_ID:-}}"
	MANIFEST="$IMAGES_DIR/${KEY#%}.jsonl"

	# AEYE_HOST forces the launcher; unset = auto-detect. Only `kitty` is useful
	# from inside tmux (it can open a split over the RC socket); other values are
	# honored but sensible only outside tmux, and degrade via launch_kitty's
	# fallback / main's mode guard.
	if [[ -n ${AEYE_HOST:-} ]]; then
		MODE="$AEYE_HOST"
		return
	fi
	if [[ -n ${TMUX:-} ]]; then
		MODE=tmux
	elif [[ -n ${KITTY_LISTEN_ON:-} ]]; then
		MODE=kitty
	elif [[ -n ${WEZTERM_PANE:-} ]]; then
		MODE=wezterm
	elif [[ ${TERM:-} == xterm-ghostty* || -n ${GHOSTTY_RESOURCES_DIR:-} ]]; then
		MODE=ghostty
	elif [[ ${TERM_PROGRAM:-} == iTerm.app ]]; then
		MODE=iterm
	else
		MODE=none
	fi
}
```

- [ ] **Step 4: Run to verify they pass**

Run: `direnv exec . bats tests/host-mode.bats`
Expected: PASS (4/4). Then `direnv exec . shellcheck scripts/tmux-claude-images.sh` → clean.

- [ ] **Step 5: Commit**

```bash
git add tests/host-mode.bats scripts/tmux-claude-images.sh
git commit -m "feat: AEYE_HOST launcher override + env-derived manifest key (#90)"
```

---

### Task 2: `launch_kitty` in-tmux split placement

**Files:**
- Modify: `scripts/tmux-claude-images.sh` (`launch_kitty`, the `placement` block ~lines 100-104)
- Modify: `tests/host-mode.bats`

- [ ] **Step 1: Add the failing test** to `tests/host-mode.bats`:

```bash
@test "kitty launch from inside tmux opens a vsplit (no KITTY_WINDOW_ID)" {
	export AEYE_HOST=kitty
	unset KITTY_WINDOW_ID
	run bash "$APP"
	[ "$status" -eq 0 ]
	grep -q "launch" "$KITTY_LOG"
	grep -q -- "--location=vsplit" "$KITTY_LOG"
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `direnv exec . bats tests/host-mode.bats -f "vsplit"`
Expected: FAIL — with `KITTY_WINDOW_ID` unset the current `placement` stays empty, so `kitty @ launch` is logged without `--location=vsplit`.

- [ ] **Step 3: Implement** — replace the `placement` block in `launch_kitty` (the `if [[ -n ${KITTY_WINDOW_ID:-} ]]; then … fi`) with:

```bash
	local placement=()
	if [[ -n ${KITTY_WINDOW_ID:-} ]]; then
		kitty @ goto-layout --match "window_id:$KITTY_WINDOW_ID" splits >/dev/null 2>&1 || true
		placement=(--match "window_id:$KITTY_WINDOW_ID" --location=vsplit --next-to "id:$KITTY_WINDOW_ID" --keep-focus)
	else
		# Inside tmux, KITTY_WINDOW_ID isn't propagated, so anchor to the active
		# window: switch it to the splits layout, then vsplit beside it. Assumes the
		# active kitty window hosts tmux as a single window (the normal setup).
		kitty @ goto-layout splits >/dev/null 2>&1 || true
		placement=(--location=vsplit --keep-focus)
	fi
```

- [ ] **Step 4: Run to verify it passes**

Run: `direnv exec . bats tests/host-mode.bats`
Expected: PASS (5/5). `direnv exec . shellcheck scripts/tmux-claude-images.sh` → clean.

- [ ] **Step 5: Commit**

```bash
git add scripts/tmux-claude-images.sh tests/host-mode.bats
git commit -m "feat: launch_kitty opens a vsplit when run from inside tmux (#90)"
```

---

### Task 3: `launch_kitty` reachability fallback

**Files:**
- Modify: `scripts/tmux-claude-images.sh` (`launch_kitty`, prepend a probe)
- Modify: `tests/host-mode.bats`

- [ ] **Step 1: Add the failing test** to `tests/host-mode.bats`:

```bash
@test "kitty unreachable from tmux falls back to a tmux split" {
	export AEYE_HOST=kitty
	export STUB_KITTY_REACHABLE=1   # bare `kitty @ ls` fails → socket down
	run bash "$APP"
	[ "$status" -eq 0 ]
	grep -q "split-window" "$TMUX_LOG"        # fell back to the tmux path
	! grep -q "launch" "$KITTY_LOG"           # never tried to open a kitty window
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `direnv exec . bats tests/host-mode.bats -f "unreachable"`
Expected: FAIL — without the probe, `launch_kitty` proceeds to `kitty @ launch` instead of falling back, so `split-window` is absent and `launch` is present.

- [ ] **Step 3: Implement** — insert this block at the very top of `launch_kitty()` (before the existing toggle `kitty @ ls --match …`):

```bash
	# A bare `kitty @ ls` lists windows iff the remote-control socket is reachable
	# (distinct from the toggle's `@ ls --match`, which also fails on no match).
	# Inside tmux the socket usually isn't reachable, so degrade to a tmux split
	# rather than failing; outside tmux there's nothing to fall back to.
	if ! kitty @ ls >/dev/null 2>&1; then
		if [[ -n ${TMUX:-} ]]; then
			echo "aeye: kitty remote control unreachable from tmux; using a tmux split (see README: kitty-pane mode)" >&2
			launch_tmux
			return
		fi
		echo "aeye: kitty remote control unreachable (enable allow_remote_control + listen_on)" >&2
		exit 1
	fi
```

- [ ] **Step 4: Run to verify it passes**

Run: `direnv exec . bats tests/host-mode.bats`
Expected: PASS (6/6). `direnv exec . shellcheck scripts/tmux-claude-images.sh` → clean.

- [ ] **Step 5: Commit**

```bash
git add scripts/tmux-claude-images.sh tests/host-mode.bats
git commit -m "feat: fall back to a tmux split when kitty RC is unreachable (#90)"
```

---

### Task 4: README — enable kitty-pane mode

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Add a setup subsection.** Near the drag-out / terminal-support docs, add:

```markdown
### Enable kitty-pane mode (native drag-out inside tmux)

Native drag-out needs a real kitty pane — tmux can't carry the protocol. To make
the viewer open as a kitty split instead of a tmux split while you work in tmux:

1. **kitty** — allow remote control on a stable socket (`kitty.conf`):
   ```
   allow_remote_control yes
   listen_on unix:/tmp/kitty-{kitty_pid}
   ```
2. **tmux** — let panes see kitty's socket and the mode flag (`tmux.conf`):
   ```
   set -ga update-environment "KITTY_LISTEN_ON AEYE_HOST"
   ```
3. **Opt in** — `export AEYE_HOST=kitty`.

If the socket isn't reachable, aeye falls back to a tmux split (drag-out then
uses `ripdrag`/`dragon` or the clipboard). Moving focus between the kitty split
and your tmux panes with one keymap (e.g. Ctrl+hjkl) is a terminal-config
concern — see smart-splits.nvim / a kitty `neighboring_window` mapping.
```

- [ ] **Step 2: Verify spelling** (pre-commit `typos` hook)

Run: `direnv exec . typos README.md`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: how to enable kitty-pane mode (#90)"
```

---

## Self-Review (completed during authoring)

- **Spec coverage:** `AEYE_HOST` override (Task 1) ✓; key decoupling (Task 1, asserted via the `%7` pane-key test) ✓; in-tmux vsplit (Task 2) ✓; graceful fallback (Task 3) ✓; README prerequisite incl. `AEYE_HOST` in `update-environment` (Task 4) ✓; bats via `--resolve` + stubs and shellcheck (all tasks) ✓. Out-of-scope (nav, the kitty/tmux config itself) correctly untouched.
- **Placeholder scan:** none — every step has real code/commands.
- **Consistency:** `resolve_target`/`launch_kitty`/`launch_tmux`, `KEY`, `MODE`, `AEYE_HOST`, `STUB_KITTY_REACHABLE`/`STUB_KITTY_MATCH`, `KITTY_LOG`/`TMUX_LOG` names match across tasks. The kitty stub's `@ ls` vs `@ ls --match` distinction matches Task 3's bare-probe vs Task 2's toggle.
- **Risk:** the in-tmux `goto-layout splits` side effect is documented (spec §3); tests stub it so they don't depend on a live kitty.
