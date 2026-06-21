# Drag the selected image out of the carousel

**Issue:** [#86](https://github.com/noamsto/aeye/issues/86)
**Status:** Design — pending review

## Goal

Let the user drag the **selected image** out of aeye and drop it into another
application (Slack, a browser, a file manager). aeye picks the best available
mechanism at runtime via a three-tier ladder, so the feature degrades cleanly
across terminals and platforms instead of being kitty-only or failing silently.

## Background

A TUI cannot be an OS drag source on its own — the terminal owns the mouse and
only forwards events inside its grid. Two mechanisms work around this, and yazi
([PR #4005](https://github.com/sxyazi/yazi/pull/4005)) ships both:

1. **kitty OSC 72 DnD protocol** ([spec](https://sw.kovidgoyal.net/kitty/dnd-protocol/)) —
   the terminal becomes the drag source on the app's behalf. kitty ≥ 0.47 only,
   but cross-platform (Linux **and** macOS).
2. **GUI helper** (`ripdrag` / `dragon`) — a tiny GTK window the user drags from.
   Linux-only (X11/Wayland); no macOS build exists.

aeye already shells out to `wl-copy`/`xclip`/`xdg-open` and already writes raw
kitty escape sequences (with a tmux-passthrough wrapper), so both mechanisms fit
existing patterns.

## The three-tier ladder

On a drag request for the selected image, aeye uses the first available path:

| Tier | Mechanism | Available when |
|------|-----------|----------------|
| 1 | Native OSC 72 drag-out | terminal answers the OSC 72 capability query (kitty ≥ 0.47, not inside tmux) |
| 2 | `ripdrag` / `dragon` GUI helper | OSC 72 unavailable **and** a helper is on `PATH` (Linux + GUI session) |
| 3 | Clipboard copy + status hint | neither above — reuses existing `copySelected`, status: `drag-out needs kitty or ripdrag/dragon` |

The tier is chosen by capability, not by guessing the terminal name. Inside
tmux the OSC 72 query won't round-trip, so tier 1 naturally falls through to the
helper — no special-casing needed.

### Platform coverage (consequence of the ladder)

| Platform + terminal | Tier |
|---|---|
| Linux + kitty ≥ 0.47 (no tmux) | 1 — native |
| Linux + anything else | 2 — helper (if installed) else 3 |
| macOS + kitty ≥ 0.47 (no tmux) | 1 — native |
| macOS + anything else | 3 — clipboard (no GUI helper exists) |

## Architecture

New file `dragout.go` (+ `dragout_test.go`), holding the tier selection and the
two active mechanisms. Detection mirrors the existing `probeSixel`
(gallery_raster.go); the helper shell-out mirrors `clipboardTool`/`copyImageToClipboard`
(clipboard.go). Wiring into the model stays in `gallery.go`.

### Capability detection — `probeDragProtocol() bool`

Models `probeSixel` exactly: open `/dev/tty`, `term.MakeRaw`, write the OSC 72
query followed by a DA1 request, read the reply in a goroutine with a ~150ms
deadline, and fully drain so a late reply can't leak onto bubbletea's stdin.

```
write: \x1b]72;t=q\x1b\\   then   \x1b[c
```

Race the two replies: if an `OSC 72 ; t=q` response (`\x1b]72;...`) arrives
**before** the DA1 terminator `c`, the terminal supports DnD → `true`. If the
`c` arrives first (or we time out / there's no tty), → `false`. This is the
detection method the spec prescribes.

Probed **once at startup** (alongside the existing backend selection in
`newGalleryModel`) and cached on the model as `dragNative bool`. Re-probing per
drag would flicker raw mode mid-session.

### Tier 2 — GUI helper

`dragHelper() (name string, args []string)`, parallel to `clipboardTool`:
prefer `ripdrag`, then `dragon`, else empty. Spawned like `copyImageToClipboard`
with `exec.Command(name, args...)` on the selected image's path. `ripdrag`/`dragon`
daemonize to hold the drag window, so the call returns promptly.

- `ripdrag <path>`
- `dragon --and-exit <path>` (dragon exits after one drop)

Helper presence (`lookPath`) is checked at drag time, not startup — cheap, and
keeps the status message accurate if the user installs one mid-session.

### Tier 1 — native OSC 72 drag-out (the hard part)

The runtime handshake is interactive and bidirectional, unlike aeye's existing
one-shot graphics escapes:

```
app  → OSC 72 ; t=o:x=1 ; <machine-id> ST     "arm a drag"
      (user performs the mouse drag gesture; terminal notifies app)
app  → OSC 72 ; t=o:o=3 ; text/uri-list ST    offer copy|move of a file URI
app  → OSC 72 ; t=p:x=0 ; <base64 file://path> ST   pre-send data (chunked, m=0 = done)
app  → OSC 72 ; t=P:x=-1                       initiate the OS drag
term → OSC 72 ; t=E ; OK                        (or POSIX error)
```

We offer a single `text/uri-list` payload — the `file://` URI of the selected
image — which is what file managers and chat apps accept for a dropped file.

**Key open risk to validate in a spike before full implementation:** capturing
the *inbound* OSC 72 events while bubbletea owns stdin. bubbletea v2 (v2.0.7,
on ultraviolet) must surface unrecognized OSC sequences as a message we can
match in `Update`; if it does, inbound events route through a new
`dragEventMsg` case. If it does **not**, the native tier is deferred and the
ladder ships with tiers 2–3 only (the probe simply returns the model to the
helper path). Outbound sequences reuse the existing `tmuxPassthrough` writer.

### Trigger and model wiring

- New keybinding **`d`** (currently unbound) in `gallery.go`'s `KeyPressMsg`
  switch, on the selected image (`m.images[m.cursor].Path`). A single
  `m.dragSelected()` method runs the ladder.
- `d` *arms* the drag (tier 1) or *spawns the helper* (tier 2) or *copies +
  warns* (tier 3). On tier 1 the user then drags with the mouse; on the others
  the gesture happens in the helper window / not at all.
- Status line reports the outcome, matching `copySelected`'s pattern:
  `Drag armed — drag with the mouse`, `Opened drag window (ripdrag)`, or the
  tier-3 hint.
- README: add `d` to the keybinding docs and a one-line note on the tiers +
  optional `ripdrag`/`dragon` dependency.

## Testing

Pure, injectable functions so the suite stays hermetic (the existing convention —
`chooseGridBackend` takes `probeSixel` as a parameter):

- `parseDragQueryReply` — given raw probe bytes, returns supported/not for the
  OSC-72-before-`c` race (table test: OSC72-first, DA1-first, garbage, empty).
- `dragHelper` — with `lookPath` injected: ripdrag present, dragon present,
  neither, both (prefers ripdrag).
- OSC 72 sequence builders (arm / offer / data / initiate) — assert exact bytes
  incl. base64 of a known `file://` URI and the `tmuxPassthrough` wrapping.
- Tier selection — `dragNative` true vs false × helper present vs absent →
  expected tier, asserted on the resulting status string.

Live terminal I/O (`probeDragProtocol`, the inbound event loop) is not unit-tested,
matching `probeSixel`, which is also untested.

## Out of scope

- Dropping files **into** aeye.
- Multi-select drag (aeye has no multi-select).
- A macOS-native (Swift/AppleScript) drag helper for non-kitty macOS terminals.
