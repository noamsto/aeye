# Open the carousel under ghostty and wezterm (no tmux)

**Date:** 2026-06-17
**Status:** Approved design
**Issue:** [#58](https://github.com/noamsto/aeye/issues/58)

## Goal

Outside tmux, `scripts/tmux-claude-images.sh` can open the viewer only two ways:
a tmux split, or a kitty remote-control window (`kitty @ launch`). Running a
coding agent in **ghostty** or **wezterm** *without* tmux falls to `MODE=none`
and prints *"image carousel needs tmux or kitty remote control"*. The Go viewer
already renders in both hosts; the gap is purely **window spawning**. This adds a
`ghostty` and a `wezterm` launch mode so the toggle works there too.

## Scope (this PR)

| Host (no tmux) | Placement | Toggle/close | Image quality |
|----------------|-----------|--------------|---------------|
| **ghostty** | separate window (`+new-window` / `open -na`) | `pgrep` the viewer, `kill` it | crisp — kitty protocol (already works) |
| **wezterm** | real split (`wezterm cli split-pane`) | stored pane-id + `kill-pane` | chafa block-art (see below) |

**No Go changes.** ghostty already renders crisp because `chooseGridBackend`
(`gallery_render.go:140`) maps `xterm-ghostty*` to the kitty backend. wezterm
speaks the kitty protocol but **not** its unicode placeholders (U+10EEEE / `U=1`
virtual placement — [wezterm#986](https://github.com/wezterm/wezterm/issues/986)),
which aeye's renderer relies on. wezterm's `TERM` doesn't match the kitty branch,
so it already falls through to the chafa block-art path — and it **must stay
there**. Adding wezterm to the kitty backend would render garbage.

## Non-goals (separate follow-up)

- **Crisp real-pixel rendering on non-kitty terminals.** A pluggable raster
  backend that picks the protocol per terminal — **sixel** (wezterm, foot) and
  **OSC 1337** (iTerm2, wezterm; iTerm2 supports neither kitty nor sixel). aeye's
  block-art path works because block-art is *text* that flows in the bubbletea
  grid; sixel/OSC-1337 are raster overlays painted at cursor coordinates with no
  placeholder mechanism to track scroll/reflow, so they need their own backend.
  Filed separately.
- **A ghostty split.** ghostty has no stable split CLI; `+split`/`+close` are an
  open proposal ([ghostty#12556](https://github.com/ghostty-org/ghostty/issues/12556)),
  not shipped. Keystroke-injection tools are too fragile. Revisit if `+split`
  lands — only `launch_ghostty` would change.
- **Warp / Alacritty / iTerm2 spawn modes.** They slot into the same contract
  later (Alacritty: `msg create-window`; Warp: tmux-only). Not this PR.

## Architecture

All work is in `scripts/tmux-claude-images.sh` plus tests and docs.

### Detection — `resolve_target`

Append two branches after the kitty branch. Priority: **tmux → kitty → wezterm →
ghostty → none**. Each host sets a distinct, non-overlapping env signal, so order
among the non-tmux three is not load-bearing; tmux stays first (a multiplexer
host wins over its outer terminal).

```sh
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
```

Both new modes key by `$CLAUDE_CODE_SESSION_ID` (like the kitty path), because
outside tmux the PostToolUse hook names the manifest by session id.

### `launch_wezterm`

wezterm has a real multiplexer CLI: `split-pane` returns the new pane-id;
`kill-pane` removes it. We persist the pane-id in a state file keyed by session
id and use it for the toggle. `split-pane` defaults its target to the agent's
pane via `$WEZTERM_PANE`, so the split lands next to the agent.

```sh
launch_wezterm() {
    local panefile="$IMAGES_DIR/$KEY.wezterm-pane" pane=""
    [[ -f $panefile ]] && pane="$(<"$panefile")"
    # Liveness: `wezterm cli list` prints a PANEID column (3rd field, row 1 is the
    # header). A stale id (pane already closed) falls through to a fresh split.
    if [[ -n $pane ]] &&
        wezterm cli list 2>/dev/null | awk -v p="$pane" 'NR>1 && $3==p{f=1} END{exit !f}'; then
        [[ -n $ENSURE_OPEN ]] && return # already open
        wezterm cli kill-pane --pane-id "$pane" >/dev/null 2>&1 || true
        rm -f "$panefile"
        return
    fi
    pane="$(wezterm cli split-pane --right --percent 40 --cwd "$STATE_DIR" -- \
        env AEYE_DIR="$STATE_DIR" CLAUDE_STATUS_DIR="$STATE_DIR" "$VIEWER_BIN" "$KEY")"
    printf '%s\n' "$pane" >"$panefile"
}
```

The `env …` prefix forwards the state dir explicitly — `wezterm cli` spawns under
the mux server's environment, which never saw ours (same reason kitty's path uses
`--env`).

### `launch_ghostty`

ghostty has no window-query/close IPC (`+close` unshipped), so the toggle keys
off the **viewer process** itself: it runs as `"$VIEWER_BIN" "$KEY"`, and `$KEY`
is the unique CC session id, so `pgrep -f` finds exactly our viewer. Killing it
exits the viewer, which closes the `-e` window.

```sh
launch_ghostty() {
    local pids
    pids="$(pgrep -f -- "$VIEWER_BIN $KEY" 2>/dev/null || true)"
    if [[ -n $pids ]]; then
        [[ -n $ENSURE_OPEN ]] && return # already open
        kill $pids 2>/dev/null || true # viewer exit closes its ghostty window
        return
    fi
    local cmd=(env AEYE_DIR="$STATE_DIR" CLAUDE_STATUS_DIR="$STATE_DIR" "$VIEWER_BIN" "$KEY")
    # --working-directory is explicit to dodge the 1.3.0 -e working-dir
    # auto-detect corruption (ghostty#11356).
    case "$(uname -s)" in
    Darwin) open -na ghostty --args --working-directory="$STATE_DIR" -e "${cmd[@]}" ;;
    *) ghostty +new-window --working-directory="$STATE_DIR" -e "${cmd[@]}" ;;
    esac
}
```

`ghostty +new-window` works on Linux via D-Bus but is unsupported on macOS, where
`open -na ghostty --args …` opens a new window in the running app instead.

### `main` updates

- Group the session-id guard: `kitty | wezterm | ghostty)` all require `$KEY`
  (reuse the existing "no CLAUDE_CODE_SESSION_ID" message).
- `MODE=none` message → *"image carousel needs tmux, kitty remote control,
  wezterm, or ghostty"*.
- The `launch_$MODE` dynamic dispatch already routes to the new functions; no
  change there.

### Extension-contract comment

A short block above `resolve_target` documenting how to add the next terminal,
so the dispatch structure stays the contract (no registry — YAGNI for ~4 hosts):

```sh
# Adding a terminal:
#   1. resolve_target: detect it (distinct env var) and set MODE/KEY/MANIFEST.
#   2. launch_<mode>(): open-or-toggle the viewer as `"$VIEWER_BIN" "$KEY"`.
#   3. Crisp images need the kitty graphics protocol's UNICODE PLACEHOLDERS
#      (U+10EEEE) — add the $TERM prefix to chooseGridBackend in
#      gallery_render.go. Without them the host falls back to chafa automatically.
```

## Testing

New bats file `tests/toggle-window.bats` (the existing `tests/toggle.bats` setup
is tmux-centric — forces `$TMUX` and a pane-keyed manifest). New setup unsets
`TMUX`/`KITTY_LISTEN_ON`, sets `CLAUDE_CODE_SESSION_ID`, writes a
`$CLAUDE_CODE_SESSION_ID.jsonl` manifest, and stubs binaries on `PATH`.

Resolve seam (`--resolve` prints `MODE`):

- `$WEZTERM_PANE` set → `MODE=wezterm`.
- `$TERM=xterm-ghostty` set → `MODE=ghostty`.
- neither (and no tmux/kitty) → `MODE=none`.

Launch behaviour (stub `ghostty`, `wezterm`, `pgrep`, `uname`, `open`; log args):

- **ghostty spawn:** `pgrep` empty, `uname`→`Linux` → `ghostty` stub logs
  `+new-window` with the viewer path + key.
- **ghostty toggle-off:** start a real `sleep 300 &`; `pgrep` stub echoes its PID;
  bare toggle → assert that PID is gone (real `kill`, no stub).
- **ghostty ensure-open no-op:** `pgrep` returns a live PID → no `ghostty` spawn
  call logged.
- **wezterm spawn:** `wezterm` stub: `list`→empty, `split-pane`→echo `42` → assert
  `split-pane` logged and panefile contains `42`.
- **wezterm toggle-off:** panefile holds `42`; `wezterm list` stub reports pane
  `42` alive → assert `kill-pane --pane-id 42` logged and panefile removed.

`shellcheck scripts/tmux-claude-images.sh` must stay clean. The justfile/CI bats
target picks up the new file automatically.

## Risks & limitations

- **No real ghostty/wezterm in CI** (and none on this Linux host either). Launch
  tests are stub-based; behaviour on a real host is verified manually. Note this
  in the PR.
- **`pgrep` false match window.** During launch, the short-lived
  `ghostty +new-window … "$VIEWER_BIN" "$KEY"` process also contains the match
  string. It exits immediately, so a *subsequent* toggle sees only the viewer.
  Acceptable.
- **`wezterm cli list` column assumption** (PANEID = 3rd field). If a wezterm
  release changes the text layout, switch to `--format json`. Avoiding `jq`
  keeps the script dependency-free, matching the current tmux/kitty paths.

## Docs

- **README** — add a "Terminal support" matrix (placement + image quality across
  tmux / kitty / ghostty / wezterm / Alacritty / Warp / other), and update the
  dual-mode feature bullet that currently says only "tmux split or kitty window".
- The `MODE=none` user-facing string (above).
- Commit as `feat:` so release-please picks it up; **do not** hand-edit
  CHANGELOG.md.
