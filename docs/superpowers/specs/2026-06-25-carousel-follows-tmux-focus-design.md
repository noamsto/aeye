# Carousel follows tmux focus (kitty-pane mode)

**Issue:** [#100](https://github.com/noamsto/aeye/issues/100) · follow-up to [#90](https://github.com/noamsto/aeye/issues/90) (kitty-pane launch mode)
**Status:** Design — pending review

## Goal

In kitty-pane mode (`AEYE_HOST=kitty`), make the carousel track the visible tmux
window: shown beside the tmux-hosting kitty window when its owning pane's tmux
window is on screen, stashed (with viewer state preserved) when you switch to a
window or session where that pane isn't shown. It should feel like the
tmux-split carousel, which follows focus for free.

## Background

tmux multiplexes inside a single kitty window. The kitty-pane carousel
(`launch_kitty` in `scripts/tmux-claude-images.sh`) opens as a separate kitty
*split* beside that window, tagged with the user var `claude_img_src=$TMUX_PANE`.
kitty has no knowledge of tmux session/window switches, so the split stays put
when the user moves to another tmux window or session — the carousel for pane A
hangs around while the user looks at an unrelated window. A tmux-split carousel
doesn't have this problem: tmux only draws the active window's panes, so it
hides and reappears naturally.

kitty-pane mode exists so the native OSC 72 drag-out works (tmux can't relay the
protocol). The cost of leaving tmux is losing this natural focus-following; this
design restores it.

## Scope

In scope:
- A reconcile step (aeye) that stashes/unstashes carousel windows to match the
  visible tmux window.
- tmux `set-hook` bindings (lazytmux) that drive the reconcile on the events
  where the visible window changes.
- The kitty RC prerequisite (nix-config), already documented as out-of-scope in
  the #90 design and required for any of this to work from tmux.

Out of scope:
- tmux-split mode — unaffected; the reconcile self-gates to kitty mode and
  no-ops otherwise.
- Multi-client tmux (two clients on one session viewing different windows). The
  carousel lives in one kitty window; we assume the single-attached-client setup
  that `launch_kitty` already assumes.
- Cross-surface navigation (Ctrl+hjkl between tmux panes and the kitty split) —
  separate concern, already noted out-of-scope in #90.

## Design

### 1. Intent is the kitty window's existence

There is no new per-pane state file. "The carousel is on for pane X" means "a
kitty window tagged `claude_img_src=X` exists" — visible *or* stashed. The
existing paths already establish this: `prefix+I` / `launch_kitty` creates the
window, pressing it again kills it, and `--ensure-open` (auto-open on a new
image/diagram) creates it. The reconcile step only ever *moves* existing
windows; it never creates or destroys them, so it can't fight the toggle.

### 2. Visibility rule

A carousel window is shown beside the tmux host iff its owning pane belongs to
the tmux window currently on screen for the attached client; otherwise it is
stashed. Granularity is the **window**, not the pane: focusing a sibling shell
pane next to Claude in the same visible window keeps the carousel up (the Claude
pane is still on screen). It stashes only when the visible window changes
(window switch or session switch).

### 3. The reconcile step (aeye)

A new action — `tmux-claude-images.sh --reconcile` (or a sibling script invoked
the same way) — runs:

1. Gate: do nothing unless kitty mode applies (`AEYE_HOST=kitty` or kitty
   detected) and `kitty @ ls` succeeds. Otherwise exit 0 immediately.
2. Compute the visible window's pane set:
   `tmux list-panes -t '{current}' -F '#{pane_id}'` for the attached client's
   active window.
3. Enumerate carousel windows from `kitty @ ls` — every window carrying a
   `claude_img_src` user var, with its pane id.
4. For each carousel window: if its pane id is in the visible set → ensure it is
   in the active kitty tab, vsplit beside the tmux host (unstash if stashed);
   else → move it to the stash tab.

The reconcile is idempotent: a window already in the correct place is skipped, so
repeated hook firing is harmless. A lightweight lockfile (in the state dir)
serializes overlapping runs.

The vsplit-beside-host placement currently inlined in `launch_kitty` is
refactored into a shared helper that both `launch_kitty` and the unstash branch
call, so placement stays consistent.

### 4. Stash mechanism — approach A (spike-validated 2026-06-25)

**A. Live-stash to a hidden kitty tab — CHOSEN.** A dedicated kitty tab (tagged
`aeye_stash=1`), created lazily on first stash, kept out of the tab bar via kitty
config. Stash = `kitty @ detach-window --match var:claude_img_src=X --target-tab
var:aeye_stash=1`. Unstash = `kitty @ detach-window --match var:claude_img_src=X
--target-tab id:<host-tab>` after ensuring the host tab is in the `splits` layout.

Spike outcome (driven over a live RC socket): the round-trip preserves the
window id — the viewer process stays alive, so cursor / zoom / drill-in survive
and return is instant with no re-render. Crucially, `detach-window --target-tab`
into a tab already in the `splits` layout **re-splits the window beside the
existing one automatically** — no manual repositioning. Verified: launch →
`splits` with two 60/60-col windows; stash → host tab back to one window, carousel
alive in the stash tab; unstash → `splits` with two 60/60-col windows again, same
window id. So `_unstash` only needs the detach + a `goto-layout splits` guard on
the host tab; the shared placement helper's layout-switch is reused, but no
`--location`/`--next-to` reposition is required.

**B. Fallback — close + persist/restore viewer state (NOT needed).** Retained
only as a record: kill on stash; the aeye viewer persists per-pane view state
(cursor, zoom, region) and restores on relaunch. Avoided because A validated
cleanly. This would have been a Go change
to the viewer (extends the existing per-pane zoom scratch in `gallery_zoom.go`).

### 5. tmux hooks (lazytmux)

Bind the reconcile script on the events where the visible window changes:
`client-session-changed`, `session-window-changed`, and `client-attached`. These
live alongside the existing reflow hooks in `config/tmux.conf.nix`. The bindings
are installed unconditionally; the reconcile script self-gates to kitty mode, so
tmux-split users pay nothing but a fast exit.

### 6. Prerequisite (nix-config)

kitty RC must be reachable from inside tmux: `allow_remote_control yes` + a
stable `listen_on` socket in kitty, and `update-environment KITTY_LISTEN_ON` in
tmux so panes inherit the socket. This was documented but not implemented in the
#90 design; it is required for the launcher *and* the reconcile to talk to kitty.

## Error handling

- RC unreachable / any `kitty @` failure → reconcile exits 0 without acting. It
  must never block or slow a tmux focus change.
- Stash tab missing when a stash is requested → create it lazily.
- Window already correctly placed → skip (idempotent).
- Concurrent hook firing → a lockfile in the state dir serializes runs; a run
  that can't take the lock exits (the holder's pass already reconciles).

## Testing

- `bats` against a stubbed `kitty` and `tmux` (mirrors `tests/host-mode.bats`):
  - carousel whose pane is in the visible window → unstashed / left in place;
  - carousel whose pane is off-screen → stashed;
  - RC down or non-kitty mode → no-op, exit 0;
  - idempotent re-run → no kitty mutations the second time.
- The A-vs-B spike and the final drag-out smoke test are manual (no RC socket in
  CI).

## Rollout

aeye (reconcile action + shared placement helper + spike; Go state persistence
only if B) → lazytmux (hooks) → nix-config (kitty RC enablement). Each lands and
is verified before the next.
