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
| 3 | Best-effort clipboard, else status hint | neither above — try `copySelected`; show `drag-out needs kitty or ripdrag/dragon` |

The tier is chosen by capability, not by guessing the terminal name. tmux
doesn't reflect the OSC 72 query back (it forwards DCS-wrapped output but
doesn't understand `OSC 72`), so the probe gets no reply and tier 1 falls
through to the helper — no special-casing needed.

**Tier 3 caveat:** `copySelected` → `copyImageToClipboard` is Linux-only today
(`wl-copy`/`xclip`); on **non-kitty macOS** it returns "no clipboard tool found",
so tier 3 there degrades to *just the status hint* — no working drag-out. Wiring
`pbcopy` into the clipboard path would fix that but is a separate concern
(tracked in [#86](https://github.com/noamsto/aeye/issues/86), out of scope here).

### Platform coverage (consequence of the ladder)

| Platform + terminal | Tier |
|---|---|
| Linux + kitty ≥ 0.47 (no tmux) | 1 — native |
| Linux + anything else | 2 — helper (if installed) else 3 |
| macOS + kitty ≥ 0.47 (no tmux) | 1 — native |
| macOS + anything else | 3 — status hint only (no GUI helper exists; clipboard path is Linux-only) |

## Architecture

New file `dragout.go` (+ `dragout_test.go`), holding the tier selection and the
two active mechanisms. Detection mirrors the existing `probeSixel`
(gallery_raster.go); the helper shell-out mirrors `clipboardTool`/`copyImageToClipboard`
(clipboard.go). Wiring into the model stays in `gallery.go`.

### Capability detection

The OSC 72 probe and the existing sixel probe both work by writing a query and
reading the `/dev/tty` reply to the DA1 `c` terminator under one raw-mode
window. **Fold them into a single probe** rather than opening `/dev/tty` and
toggling raw mode twice at startup (which also risks the two replies
interleaving): write the OSC 72 query *then* the DA1 request, read once, and
parse both capabilities from the same stream.

```
write: \x1b]72;t=q\x1b\\   then   \x1b[c
read : drain to the DA1 'c' terminator
```

DnD support = an `OSC 72 ; t=q` response (`\x1b]72;...`) appears in the stream
**before** the DA1 `c`. If `c` arrives first (or timeout / no tty), DnD is
unsupported. Sixel parsing reads the DA1 attributes from the same reply,
unchanged. The reply parser is pure and table-tested; the tty I/O stays
untested like `probeSixel`.

Concretely: generalize the current single-purpose `probeSixel` into a probe that
returns both flags (or have `chooseGridBackend` and the new `dragNative` field
share one probe result). Probed **once at startup** in `newGalleryModel` and
cached as `dragNative bool` — re-probing per drag would flicker raw mode
mid-session. If reusing one probe proves awkward, a standalone
`probeDragProtocol()` modeled on `probeSixel` is the fallback, accepting the
second raw-mode toggle.

### Tier 2 — GUI helper

`dragHelper() (name string, args []string)`, parallel to `clipboardTool`:
prefer `ripdrag`, then `dragon`, else empty. Spawned like `copyImageToClipboard`
with `exec.Command(name, args...)` on the selected image's path. `ripdrag`/`dragon`
daemonize to hold the drag window, so the call returns promptly.

- `ripdrag <path>`
- `dragon -x <path>` (`-x`/`--and-exit`: exit after one drop)

Exact flags are confirmed against the installed binaries at implementation —
both tools' CLIs are small and stable, but versions differ.

Helper presence (`lookPath`) is checked at drag time, not startup — cheap, and
keeps the status message accurate if the user installs one mid-session.

### Tier 1 — native OSC 72 drag-out (the hard part)

The runtime handshake is interactive and bidirectional, unlike aeye's existing
one-shot graphics escapes:

```
app  → OSC 72 ; t=o:x=1 ; <machine-id> ST     "arm a drag" (machine-id optional)
      (user performs the mouse drag gesture; terminal notifies app)
app  → OSC 72 ; t=o:o=3 ; text/uri-list ST    offer copy|move of a file URI
app  → OSC 72 ; t=p:x=0 ; <base64 file://path> ST   send data (chunked, m=0 = done)
app  → OSC 72 ; t=P:x=-1                       initiate the OS drag
term → OSC 72 ; t=E ; OK                        (or POSIX error)
```

We offer a single `text/uri-list` payload — the `file://` URI of the selected
image — which is what file managers and chat apps accept for a dropped file.

**The entire native tier is the project's main risk and is gated on a spike**
(see staging below). The above sequence is the protocol's shape, but two things
must be confirmed against a live kitty ≥ 0.47 before committing to it:

1. **Inbound capture** — can we read the terminal's OSC 72 events (drag-gesture
   notification, `t=E` result) while bubbletea v2 (v2.0.7, on ultraviolet) owns
   stdin? It must surface unrecognized OSC sequences as a message we can match
   in `Update` (routed to a new `dragEventMsg`). If not, native is deferred.
2. **Exact ordering & arming semantics** — when the data is sent relative to the
   gesture notification, what `t=o:x=1` arming does to bubbletea's normal mouse
   capture, and the precise field meanings. The summary above is from the spec
   prose and is not yet validated end-to-end.

Outbound sequences reuse the existing `tmuxPassthrough` writer.

### Implementation staging

Native is gated, so build in this order — each step ships value independently:

1. **Probe + tiers 2–3** (low risk): combined capability probe, `dragHelper`,
   the `d` keybinding, `dragSelected` ladder, status messages, tests, README.
   This alone delivers working drag-out on Linux + a graceful hint everywhere
   else.
2. **Native spike** (timeboxed): a throwaway prove-out of inbound OSC 72 capture
   under bubbletea v2 against live kitty ≥ 0.47.
3. **Native tier** — only if the spike succeeds. Otherwise file a follow-up and
   ship with 1.

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

- Probe reply parser — given the raw combined reply, returns DnD supported/not
  for the OSC-72-before-`c` race **and** leaves sixel parsing intact (table test:
  OSC72-then-DA1, DA1-only, garbage, empty).
- `dragHelper` — with `lookPath` injected: ripdrag present, dragon present,
  neither, both (prefers ripdrag).
- OSC 72 sequence builders (arm / offer / data / initiate) — assert exact bytes
  incl. base64 of a known `file://` URI and the `tmuxPassthrough` wrapping.
- Tier selection — `dragNative` true vs false × helper present vs absent →
  expected tier, asserted on the resulting status string.

Live terminal I/O (the combined probe's tty read, the inbound event loop) is not
unit-tested, matching `probeSixel`, which is also untested.

## Out of scope

- Dropping files **into** aeye.
- Multi-select drag (aeye has no multi-select).
- A macOS-native (Swift/AppleScript) drag helper for non-kitty macOS terminals.
