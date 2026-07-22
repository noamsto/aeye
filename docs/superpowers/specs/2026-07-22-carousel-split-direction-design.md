# Width-aware carousel split direction

Issue: [#144](https://github.com/noamsto/aeye/issues/144)

## Problem

The image carousel always opens as a **side** split (left │ right) across every
backend. On a narrow window — a laptop screen, or a tmux window that is tall
rather than wide — the side split halves the already-scarce columns and images
render cramped. The split should follow the window's *longer* axis instead, with
an override for when the automatic choice is wrong.

## Semantic axes

The work uses two backend-neutral names and maps them at each backend's edge,
because the backends name directions inconsistently (tmux `-h` is side-by-side;
kitty `vsplit` is the same thing).

| Semantic axis | tmux | kitty | wezterm | iterm (AppleScript) |
|---|---|---|---|---|
| **SIDE** (left │ right) | `-h` | `vsplit` | `--right` | `split vertically` |
| **BOTTOM** (top / bottom) | `-v` | `hsplit` | `--bottom` | `split horizontally` |

Today all backends hardcode **SIDE** except iTerm, which already uses
`split horizontally` = **BOTTOM** (`scripts/tmux-claude-images.sh:255`). That
pre-existing inconsistency is normalized by routing iTerm through the same axis
resolver as everything else.

## Axis resolution (launch time)

A single resolver in `scripts/tmux-claude-images.sh`, consulted immediately
before each backend performs its split:

- `AEYE_SPLIT=side` or `AEYE_SPLIT=bottom` → force that axis, skip measuring.
- `AEYE_SPLIT` unset or `auto` → measure the **host window** and split its
  longer *pixel* axis. A terminal cell is roughly 2× taller than wide, so the
  window's pixel aspect is `cols : 2·rows`:
  - `cols > 2·rows` → landscape → **SIDE**
  - otherwise → **BOTTOM**
- Dimensions unreadable → fall back to **SIDE** (today's behavior — no
  regression).

The comparison is pure integer arithmetic (POSIX, BSD-`awk`-safe). The `2·rows`
cell-aspect factor lives in one named constant so it can be tuned in one place.

### Where dimensions come from, per backend

- **tmux**: `tmux display -p -t "$KEY" '#{window_width} #{window_height}'`.
- **kitty**: window geometry from `kitty @ ls` (columns/lines of the host
  window).
- **wezterm**: `wezterm cli list` (the host pane's size in cells).
- **iterm**: an AppleScript query for the current session's `columns` / `rows`.

Deliberately **not** `tput`/`$COLUMNS`/`$LINES`: the launcher is spawned from a
hook or keybind, so its stdout is frequently not a tty (making `tput` fail) and
`$COLUMNS`/`$LINES` are shell-local and not exported to the script. This is true
on both Linux and macOS; querying each terminal's own API is robust on both.

## Live toggle — `s`

The viewer flips the running carousel's axis in place. `s` is unused today
(taken keys: `q ctrl+c ctrl+hjkl hjkl` arrows `z Z + - = _ 0 esc tab shift+tab
] [ n p g G o O y d r x u` and digits `1-9`); mnemonic: **s**plit. Handled in
the `tea.KeyMsg` switch at `gallery.go:415`.

- **tmux**: the viewer knows its own pane (`$TMUX_PANE`) and the host `KEY` (its
  CLI argument). It flips with `tmux move-pane -h|-v -s <self> -t <host>` — a
  same-window re-split. The viewer **process survives** (the pane is moved, not
  killed), receives a resize, and repaints through the existing
  `WindowSizeMsg` path, which already re-transmits graphics on resize.
  - The current axis is stored in a `@claude_img_axis` **pane option**, set at
    launch next to the existing `@claude_img_src` (`:100`). `s` reads it, picks
    the opposite, performs the `move-pane`, then updates the option.
- **Other backends**: `s` is a documented no-op in v1. kitty/wezterm/iterm have
  no clean single-window axis flip; their live toggle is a fast-follow.

## Scope

**In v1:**
- Auto-detection + `AEYE_SPLIT` override across **all four** backends
  (tmux, kitty, wezterm, iterm) — this is what actually fixes narrow screens
  everywhere.
- Live `s` toggle **tmux-only**.

**Deferred (fast-follow):**
- Live `s` toggle for kitty / wezterm / iterm.

## macOS compatibility

The tmux path — where the user primarily works, and where the live toggle lands
— is fully macOS-compatible; tmux behaves identically and nix installs the same
binary on both OSes. The integer heuristic and `printf -v`/array usage are
bash-3.2-safe (macOS's system bash) regardless of the nix bash in use.

Two macOS-touching items are explicit requirements rather than assumptions:

1. **Out-of-tmux dimension reads** use each terminal's own API
   (`wezterm cli list`, iTerm AppleScript `columns`/`rows`) — not
   `tput`/`$COLUMNS` — for the reason given above.
2. **iTerm `split vertically`** (the SIDE branch) is confirmed against the
   iTerm2 scripting dictionary during implementation; it mirrors the existing
   `split horizontally with default profile command …` line.

## Testing

- **bats** (`tests/`, alongside `launch-hidden.bats`): axis-resolver truth
  table — forced `side`/`bottom`; auto with mocked `tmux display` dims
  (landscape → SIDE, portrait → BOTTOM); unreadable dims → SIDE fallback.
- **Go**: a pure `chooseAxis(cols, rows)` helper unit-tested at the boundary
  (`cols == 2·rows`), so the heuristic is reachable without going through bash.
  The bash resolver and the Go toggle share this logic conceptually.
- **Manual**: a narrow tmux window auto-opens BOTTOM; `s` flips to SIDE and
  back; `AEYE_SPLIT=side` forces SIDE on a narrow window; `AEYE_SPLIT=bottom`
  forces BOTTOM on a wide one.

## Non-goals

- Configurable split *percentage* (stays at each backend's current default).
- Persisting the last-chosen axis across sessions.
- Hysteresis / re-evaluation on window resize after launch (the axis is fixed
  at launch; the user re-balances with `s`).
