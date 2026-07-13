---
name: image-gallery
description: Use when the user wants to see the images from this conversation — screenshots, images you viewed or wrote, generated pictures. Opens the aeye image carousel (preview + filmstrip) in a tmux split or a kitty window.
---

# Image Gallery

The aeye plugin captures every image this Codex pane touches (apply_patch /
view_image / Bash tool calls) into a per-pane manifest, and renders them as a
browsable **carousel** — a big preview of the selected image plus a filmstrip
of thumbnails — in a tmux split or a kitty window.

## Opening it

When the user asks to *see* / *show* / *browse* the images (or a specific one)
from this conversation, open the carousel by running:

```bash
tmux-claude-images
```

The command auto-detects its host and toggles the viewer (run it again to
close):

- **Inside tmux** — a split pane, keyed by `$TMUX_PANE`. The user can also open
  it with `prefix + I` if their tmux config binds it (lazytmux does).
- **Outside tmux, in kitty with remote control** (`$KITTY_LISTEN_ON` set) — a
  `kitty @ launch` window, keyed by the session.

- It does nothing if no images have been captured yet (in tmux it prints
  `no images yet for this pane`) — the manifest fills as you touch images via
  apply_patch/view_image/Bash.
- Inside the carousel: `h`/`l` move, `↵`/`o` open the image in the default
  viewer, `O` open its folder, `q` quit. It auto-refreshes as new images arrive.

## Requirements

- Needs the aeye viewer on PATH (the `tmux-claude-images` command)
  plus a host to open in: either tmux, or kitty with remote control enabled
  (`$KITTY_LISTEN_ON` set). Outside both it prints a hint and does nothing. If
  the command isn't found, the viewer isn't installed — don't try to substitute
  another tool.
- Full-fidelity preview needs a kitty-graphics terminal (kitty/ghostty);
  elsewhere it falls back to `chafa` block-art.
