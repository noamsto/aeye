---
name: image-gallery
description: Use when the user wants to see the images from this conversation — screenshots, images you Read or Wrote, generated pictures. Opens lazytmux's image carousel (preview + filmstrip) in a tmux split for the current pane.
---

# Image Gallery

lazytmux captures every image this Claude Code pane touches (Read / Write /
screenshot tools) into a per-pane manifest, and renders them as a browsable
**carousel** — a big preview of the selected image plus a filmstrip of
thumbnails — in a tmux split pane.

## Opening it

When the user asks to *see* / *show* / *browse* the images (or a specific one)
from this conversation, open the carousel by running:

```bash
tmux-claude-images
```

That toggles a split pane for **this** pane's images (it targets `$TMUX_PANE`).
Run it again to close. The user can also open it themselves with `prefix + I`.

- It's a no-op printing `no images yet for this pane` if nothing has been
  captured yet — the manifest fills as you Read/Write/screenshot images.
- Inside the carousel: `h`/`l` move, `↵`/`o` open the image in the default
  viewer, `O` open its folder, `q` quit. It auto-refreshes as new images arrive.

## Requirements

- Runs only inside tmux with lazytmux installed (the `tmux-claude-images`
  command is on PATH there). If the command isn't found, lazytmux isn't active —
  don't try to substitute another tool.
- Full-fidelity preview needs a kitty-graphics terminal (kitty/ghostty);
  elsewhere it falls back to `chafa` block-art.
