# Toggleable viewer trace (tooling for #125)

- Issue: #125 (carousel opens with partial preview + empty filmstrip; a manual resize settles it)
- Date: 2026-07-14
- Status: design approved, pending spec review

## Problem

Opening the carousel intermittently paints a partial preview rect and empty
filmstrip thumbnails; moving/resizing the pane settles it. The failure is
intermittent and did not reproduce in the one faithful trace captured from the
real environment (that trace showed a correct first `WindowSizeMsg` of 124×58 at
~28ms). Agent-side reproduction is impossible: panes spawned from a
non-interactive tool shell get no controlling tty, so bubbletea never receives a
usable size — a harness artifact, not the real bug.

We cannot fix the real bug without observing it in the field. This feature is
the instrument: shippable, toggleable tracing the user leaves on until a glitchy
open is captured.

## Goals

- Toggle tracing via `AEYE_DEBUG`, reaching the viewer on the launch paths the
  user actually uses (tmux keybinding, `/aeye` skill, auto-open hook).
- Capture enough signal to **discriminate a glitchy open from a healthy one** —
  specifically the store-vs-paint race, not just Go-side message order.
- Zero meaningful overhead when disabled.
- A tiny, correct safety guard (`0×0` size) that both prevents a known-bad state
  and doubles as a field signal.

## Non-goals / deferred

- **The actual #125 fix.** Deferred until a captured trace identifies the real
  failure mode. Shipping a paint-race fix now would be guessing.
- **kitty graphics store-ACK query.** The gold-standard terminal-side signal, but
  reading kitty's response competes with bubbletea's input reader (risk of
  breaking input). Deferred; revisit only if the in-process proxy proves
  ambiguous on a real glitch trace.
- **`sizeProbe` machinery** (the experimental re-query). Verified a no-op in the
  real trace; reverted.

## Design

### 1. Trace facility — `trace.go` (package `main`)

Mirrors the existing `logDropped` precedent (`gallery_render.go:217`): best-effort
file logging in the state dir, never fails the viewer.

- **Gating:** a package-level `traceEnabled bool` resolved once at startup from
  `AEYE_DEBUG`. Unset/empty → disabled. `tracef(...)` returns immediately when
  disabled. Hot call sites (per-message, per-frame) are additionally wrapped in
  `if traceEnabled { ... }` so no `[]any` slice is allocated when off.
- **Output file (per-pane, truncate-on-open):**
  - `AEYE_DEBUG=1` (or `true`/`on`) → `<state-dir>/trace-<pane>.log`, where
    `<state-dir>` = `filepath.Dir(manifestPath(pane))` and `<pane>` is the
    sanitized manifest key. Per-pane avoids interleaving every session's viewer
    into one file (the state dir is machine-wide shared).
  - `AEYE_DEBUG=<path>` → that exact file.
  - Opened once (`sync.Once`) with `O_CREATE|O_WRONLY|O_TRUNC` (fresh each
    launch — the glitch shows at open, so the current file always holds the
    relevant trace). `-<pane>.log`/custom paths are outside the GC globs
    (`gc_sweep` only touches `*.jsonl/*.owner/*.lock`), so they are never swept.
- **Format:** a one-line header on open (RFC3339 wall-clock, pid, aeye version,
  backend, term, tmux yes/no, pane key, image count), then lines stamped
  `[+NNNNms]` relative to process start.

### 2. Trace points (viewer lifecycle + terminal boundary)

In the carousel `Update`/`View`/`transmitView`:

- **Message loop:** `msg %T` per message, **excluding** `MouseMotionMsg` and the
  periodic tick (they would flood and bury the first-frame sequence).
- **`WindowSizeMsg`:** `w`, `h`, `firstReady`, computed preview geometry.
- **`0×0` guard:** logs `WindowSizeMsg IGNORED w=0 h=0` (see §3).
- **`settleMsg`:** fired + the re-store it triggers.
- **`transmitView` (terminal-boundary signal):** store-vs-skip, geometry, image
  count, and the **`fmt.Fprint(m.tty, …)` return `(n, err)` plus APC byte
  count** — a short or failed store write explains a blank cell directly, with no
  timing race needed.
- **`View()` (terminal-boundary signal):** frame emits with timestamp, output
  length, and whether the preview placeholder is present — gated to the first
  ~1s / until settle so it does not flood. The relative `[+NNNNms]` ordering of a
  completed store-write vs a frame-emit is the in-process proxy for the
  store-vs-paint race.
- **`reload` / live theme switch.**

### 3. Keep the `0×0` guard; revert `sizeProbe`

Retain the `WindowSizeMsg` guard: a `0×0` size is ignored rather than committed,
because `computeLayout(0,0)` clamps to a 1×1 layout with no recovery until a
manual resize — precisely the #125 symptom. It is cheap and correct, and its
trace line is the one signal that confirms/denies a real `0×0` in the field.

Revert only the active `sizeProbe` re-query machinery (`sizeProbeCmd`,
`sizeProbeMsg`, the `Init` batch entries, the `sizeProbeMsg` case) and delete the
throwaway `gallery_debug.go` (its `dbg()` is superseded by `tracef`).

### 4. Launcher env forwarding — `scripts/tmux-claude-images.sh`

Today each `launch_*` forwards only an explicit allow-list into the spawned
viewer; `AEYE_DEBUG` is not among them, so it never reaches the viewer on
kitty/wezterm/ghostty/iterm and is fragile on tmux. Forward `AEYE_DEBUG` into
**every** launcher, the same way `AEYE_OWNER`/`AEYE_DIR` are, and only when it is
set in the launcher's own environment:

- `launch_tmux` — add to the `env …` prefix (`:93`).
- `launch_kitty` — add to the `--env` args (`:172-184`).
- `launch_wezterm` / `launch_ghostty` / `launch_iterm` — add to their `env …`
  invocations (`:208`, etc.).

### Where the user sets `AEYE_DEBUG` (must reach the *launcher*)

The launcher reads `AEYE_DEBUG` from its own env, then forwards it. The launcher
runs under a different env per path:

| Launch path        | Launcher runs under        | Set `AEYE_DEBUG` via                     |
|--------------------|----------------------------|------------------------------------------|
| tmux keybinding    | `run-shell` = tmux env     | `tmux set-environment -g AEYE_DEBUG 1`   |
| `/aeye` skill/hook | Claude Code process env    | shell profile export (`~/.config/fish/`) |

Primary path here is `MODE=tmux`, so `tmux set-environment -g AEYE_DEBUG 1` is
the main knob. This is distinct from `AEYE_BIN`, which is also read in the
launcher's env (`:433`) but points at the binary, not forwarded to the viewer.

## Testing

- `tracef` is a no-op when `AEYE_DEBUG` is unset (no file created).
- `AEYE_DEBUG=<path>` writes a header and lines to that file.
- `AEYE_DEBUG=1` resolves to `<state-dir>/trace-<pane>.log`.
- Re-open truncates (fresh file per launch).
- Launcher test seam: `AEYE_DEBUG` is present in the built viewer command for
  each `launch_*` when set, absent when unset (extend the existing bats seams in
  `tests/`).

## Delivery / rollout

1. Land as its own PR (`feat(viewer): toggleable AEYE_DEBUG trace`) on the #125
   branch.
2. Refresh the nix package (`vendorHash` unaffected — no new module deps).
3. User rebuilds their installed `aeye`, then `tmux set-environment -g
   AEYE_DEBUG 1` and leaves it on.
4. Interim (before the nix bump lands): point `AEYE_BIN` at a stable-path build
   (not the temp scratchpad).

## Risks

- The in-process proxy may still not fully discriminate the race; if a captured
  glitch trace is ambiguous, escalate to the deferred kitty store-ACK query.
- `truncate-on-open` loses an earlier glitchy trace if the user reopens before
  noticing; accepted for cleanliness since the glitch is visible at open.
