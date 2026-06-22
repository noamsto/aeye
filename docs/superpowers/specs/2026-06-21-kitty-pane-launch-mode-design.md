# Configurable kitty-pane launch mode

**Issue:** [#90](https://github.com/noamsto/aeye/issues/90) · follow-up to [#86](https://github.com/noamsto/aeye/issues/86) / PR [#87](https://github.com/noamsto/aeye/pull/87)
**Status:** Design — pending review

## Goal

Let a kitty user who runs tmux open the aeye viewer in a **native kitty split**
instead of a tmux split, so the native OSC 72 drag-out (#87) works in their
normal workflow. Opt-in via config, with a graceful fallback when the kitty
remote-control socket isn't reachable.

## Background

Native OSC 72 drag-out only works in a bare kitty pane — tmux can't relay the
protocol's inbound frames (confirmed empirically: kitty's own `kitten dnd` fails
through tmux in both directions). The only escape hatch is to put the viewer in
a real kitty split. aeye's launcher (`scripts/tmux-claude-images.sh`) already has
a `launch_kitty()` that opens a kitty split via `kitty @ launch`, but it's only
reachable when *outside* tmux — `resolve_target()` checks `$TMUX` first and
always picks the tmux split for a tmux user.

## Scope

In scope (aeye):
- An `AEYE_HOST` env var that overrides the launcher's detected launch MODE.
- Making `launch_kitty()` work from inside tmux (no `$KITTY_WINDOW_ID`).
- Keeping the manifest **key** consistent between capture and viewer.
- Graceful fallback + a README setup section + a `bats` test.

Out of scope (the user's nix-config, documented but not implemented):
- kitty `allow_remote_control` + a stable `listen_on` socket; tmux
  `update-environment KITTY_LISTEN_ON` so panes can reach it.
- Ctrl+hjkl navigation across tmux panes ↔ the kitty split (smart-splits.nvim /
  kitty keybinds).

## Design

### 1. `AEYE_HOST` overrides the launch MODE only

`resolve_target()` computes a `MODE` (`tmux`/`kitty`/`wezterm`/`ghostty`/`iterm`/
`none`) by env detection, `$TMUX` first. Add at the top: if `AEYE_HOST` is set to
one of those values, it forces `MODE` and detection is skipped. Unset = today's
behavior. So `AEYE_HOST=kitty` makes a tmux user get the kitty split.

### 2. Decouple the manifest KEY from the MODE

This is the subtle correctness point. The capture hook (`images.sh`) and the
session hooks key the manifest by `${TMUX_PANE:-$CLAUDE_CODE_SESSION_ID}` — pane
id inside tmux, session id otherwise — independent of any launch mode. Today
`resolve_target()` couples KEY to MODE (the kitty branch keys by session id). If
`AEYE_HOST=kitty` kept that coupling, a tmux user's viewer would read the
session-keyed manifest while capture writes the pane-keyed one → **the carousel
shows nothing.**

Fix: compute `KEY`/`MANIFEST` from the environment the *same way the capture hook
does* — `KEY="${TMUX_PANE:-$CLAUDE_CODE_SESSION_ID}"` — **regardless of MODE**.
MODE then only selects which `launch_*` function runs and how the viewer window
is opened/toggled. Capture and viewer can't disagree because they derive the key
identically. `launch_kitty` passes this same `KEY` to the viewer and tags the
window `var:claude_img_src=$KEY` as it already does.

### 3. `launch_kitty` from inside tmux

`launch_kitty`'s window placement is already guarded by
`if [[ -n ${KITTY_WINDOW_ID:-} ]]`. Inside tmux `$KITTY_WINDOW_ID` is unset, so
that block is skipped and `kitty @ launch` falls back to the active window — but
without `--location=vsplit` it won't split. Change: when `$KITTY_WINDOW_ID` is
unset, still pass `--location=vsplit --keep-focus` (targeting the active window,
no `--match`/`--next-to`), and `goto-layout splits` on the active window first so
vsplit takes effect. The toggle (`kitty @ ls --match var:claude_img_src=$KEY`)
already works socket-only, no `$KITTY_WINDOW_ID` needed.

### 4. Graceful fallback

`launch_kitty` requires a reachable socket. If `MODE=kitty` was reached by the
`AEYE_HOST` override but `kitty @ ls` fails (no `KITTY_LISTEN_ON` / socket
unreachable from tmux), fall back to `launch_tmux` and print a one-line stderr
hint pointing at the README setup. Never hard-fails; a misconfigured override
degrades to today's tmux split.

### 5. Prerequisite (documented, not implemented)

README gains a short "Enable kitty-pane mode (native drag-out in tmux)" section:
kitty `allow_remote_control yes` + stable `listen_on`; tmux
`set -ga update-environment KITTY_LISTEN_ON`; then `export AEYE_HOST=kitty`.

## Testing

`tests/*.bats` already exercises the launcher. Add a bats test for MODE/KEY
resolution as a pure function of the environment (stub `kitty`/`tmux` as needed):

- `AEYE_HOST=kitty` with `$TMUX` set → `MODE=kitty`, `KEY` = the tmux pane id
  (not the session id) — the decoupling guard.
- `AEYE_HOST` unset → detection unchanged (tmux → tmux, bare kitty → kitty).
- `AEYE_HOST=kitty` but `kitty @ ls` stub fails → falls back to the tmux path.
- An invalid `AEYE_HOST` value → ignored, detection used.

Manual: in kitty+tmux with the prerequisite config + `AEYE_HOST=kitty`, toggle →
viewer opens in a kitty split → drag the image out → it drops.

## Out of scope

- The kitty/tmux config that exposes the socket (user's nix-config).
- Ctrl+hjkl navigation across the tmux/kitty boundary (user's nix-config).
- Any change to the native drag protocol itself (shipped in #87).
