# Carousel: delete image with `x` + undo window

Issue: #137

## Problem

The carousel accumulates every screenshot and diagram captured during a
session. There is no way to remove an unwanted image — the user is stuck
scrolling past clutter. Add a delete action, with a short regret window so a
mistaken keypress is recoverable.

## Behavior

- **`x`** marks the *selected* image as pending-deletion. The entry stays
  visible and gains three cues:
  - a **danger-colored border** on its filmstrip cell and, if selected, the
    preview frame;
  - a **marked caption** — `✗ <name>`;
  - a **depleting countdown bar** in the caption — e.g. `▓▓▓░░ 2s`.
  The footer action row shows `Deleting <name> — u to undo`.
- **`u`** cancels a pending deletion within the window (restores the normal
  border/caption).
- After a **5-second** window with no undo, the deletion **commits**: the
  underlying file(s) are removed and the entry disappears from the carousel.

Single-level undo: at most one deletion is pending at a time. A second `x`
(on any entry) commits the prior pending one and marks the new selection.
Quitting (`q`/`ctrl+c`) commits any pending deletion so it does not reappear on
the next launch.

## Deletion semantics

On commit, files are removed with `os.Remove` (the 5s window is the regret
buffer, so no trash indirection is needed; these are transient files under the
status dir):

- **Plain image** (screenshot): remove `entry.Path`.
- **d2 diagram**: remove the whole cluster so a theme switch cannot resurrect a
  half-deleted diagram — both theme PNG files (`-light`/`-dark`), both SVGs, and the
  `.d2` source. Missing siblings are ignored.

**No manifest rewrite.** The viewer stays a read-only manifest consumer — it
never writes `images/<pane>.jsonl`, avoiding a write race with the append-only
capture hooks. Once the file is gone, `loadManifest`'s existing "drop entries
that don't decode" logic keeps the committed deletion out permanently. The stale
JSONL line self-drops on the next reload.

## Interaction with the 1.5s reload poll

`galleryTickCmd` reloads the manifest every 1.5s, and `loadManifest` returns
every decodable entry on disk. Because a pending deletion's file is still on disk
during the window, `reload()` must **re-apply the pending mark** to the reloaded
entry whose path matches `pending.path` — otherwise the mark would be lost each
poll. This is a match, not a filter: the entry legitimately stays in the list
while pending.

## Model changes (`galleryModel`)

- `pending *pendingDeletion` — the single in-flight deletion, or nil.
  - `path string` — manifest path, the match key across reloads.
  - `deadline time.Time` — when the countdown reaches zero / commit fires.

  No cursor-restore field is needed: the pending entry stays in the list while
  marked, so `u` just clears the mark without moving the cursor. Only *commit*
  removes an entry, and the post-commit cursor is handled by the existing clamp
  in `reload`/`selectIndex`.
- `delGen uint64` — generation counter debouncing the commit timer, mirroring
  the existing `vecGen`/`rasterGen` pattern. Each `x`/`u` bumps it; a stale
  commit tick (gen mismatch) is dropped.
- `dangerColor imgcolor.Color` — resolved once at startup like
  `selColor`/`dimColor`.

## Timers

Two bubbletea `tea.Tick` messages, both carrying `delGen` for staleness checks:

- **Commit tick** — fires at the 5s deadline → `deleteCommitMsg{gen}`. If
  `gen == m.delGen` and `m.pending != nil`, remove the file(s), clear
  `m.pending`, and rebuild the view so the entry drops out.
- **Countdown tick** — ~300ms cadence, scheduled **only while a deletion is
  pending**, re-rendering the text layer so the bar depletes. It stops
  (reschedules nothing) once `m.pending` is nil. This re-renders only lipgloss
  text — the kitty/sixel rasters are stored by image-id and are **not**
  re-transmitted, so the cost is negligible.

No pixel dimming: there is no portable per-image opacity in the kitty/sixel
protocol, and a smooth fade would require decode → darken → re-encode →
re-transmit every frame. The border + countdown bar convey the pending state
without any raster work.

## Rendering

In the filmstrip cell loop and the preview frame, when the entry's path matches
`m.pending.path`:

- use `dangerColor` for the border instead of the normal selected/dim border;
- prefix the caption with `✗ ` and append the countdown bar computed from
  `time.Until(pending.deadline)`.

Footer: add `x del` to the `actionKeys` row. The transient `m.status` line
already takes over that row for pending feedback.

## Commit triggers (summary)

| Trigger | Effect |
|---|---|
| Commit tick at deadline | remove file(s), clear pending |
| Second `x` (any entry) | commit prior pending, mark new selection |
| Quit (`q`/`ctrl+c`) | commit any pending, then quit |
| `u` | cancel pending (no file removed) |

## Edge cases

- **Empty carousel / no selection**: `x` is a no-op.
- **Pending entry already gone from disk** (committed by another path, or file
  vanished): reload finds no match → `pending` cleared, treated as done.
- **`u` with no pending**: no-op.
- **`m.status` cleared on next keypress**: pressing some *other* key during the
  window hides the `u to undo` hint text, but `u` still works until the timer
  fires. Accepted as-is.

## Out of scope / future work

- **Multi-select mode** — a thumbnail-grid or side-list view with
  space-to-select and batch delete. The tombstone/commit primitive here
  generalizes to a *set* of pending deletions, so this builds on top rather than
  replacing anything. Grid-with-space-select is the shape worth prototyping
  first.
- Preventing a *future* session's transcript backfill from re-rendering a
  deleted diagram. Deletion is scoped to the current session's files; a later
  backfill that re-renders from the transcript is a separate concern.

## Testing

- `pendingDeletion` lifecycle: `x` sets pending + bumps `delGen`; `u` clears it;
  a stale commit tick (gen mismatch) is dropped; a current one removes files.
- File-cluster resolution: plain entry → `[Path]`; d2 entry → both theme PNG files,
  both SVGs, `.d2` source (with missing siblings tolerated).
- Reload re-applies the pending mark by path match and does not lose it across a
  poll.
- Second `x` commits the prior pending deletion.
- Cursor lands sensibly after a commit (next entry, or previous if last).
