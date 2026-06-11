# agent-carousel

A terminal image carousel for coding agents — a big preview plus a filmstrip of
thumbnails, rendered in a tmux split or kitty window (dual-mode). Shows every
image a coding-agent session touches (reads, writes, screenshots) so you can
browse them without leaving the terminal.

Full-fidelity preview on kitty-graphics terminals (kitty/ghostty); `chafa`
block-art fallback elsewhere.

## Install

The carousel has two PATH entrypoints: the `agent-carousel` viewer and the
`tmux-claude-images` toggle that opens it. Put **both** on your PATH — the
toggle launches the viewer into a fresh tmux/kitty pane, which resolves
`agent-carousel` from your PATH (override with `$AGENT_CAROUSEL_BIN`).

```bash
nix profile install github:noamsto/agent-carousel          # viewer
nix profile install github:noamsto/agent-carousel#toggle   # toggle
```

Or grab the prebuilt archive (viewer **and** toggle) for your platform from the
[releases page](https://github.com/noamsto/agent-carousel/releases) and extract
both onto your PATH. `go install github.com/noamsto/agent-carousel@latest` works
too, but installs only the viewer.

The block-art fallback needs [`chafa`](https://hpjansson.org/chafa/) on PATH;
kitty/ghostty render directly without it.

### Claude Code plugin

This repo doubles as its own single-plugin marketplace. The plugin is the
**capture** half — a PostToolUse hook that records every image your session
touches so the carousel has something to show:

```
/plugin marketplace add noamsto/agent-carousel
/plugin install agent-carousel@agent-carousel
```

Then ask Claude to *show the images from this conversation*, or invoke the
`image-gallery` skill. The plugin only captures; opening the carousel uses the
`tmux-claude-images` command from the install step above.

<details>
<summary>Via lazytmux (Nix / Home Manager)</summary>

[lazytmux](https://github.com/noamsto/lazytmux) consumes this repo as a flake
input — it puts the viewer and `tmux-claude-images` on PATH and binds
`prefix + I`. If you run lazytmux you already have the carousel; just add the
plugin above for capture.

</details>

## Architecture

The viewer is **agent-agnostic**. It renders a per-pane manifest and has no
knowledge of which agent produced it — the [manifest JSONL](docs/MANIFEST.md) is
the stable interface. Each agent gets a small **capture adapter** that appends
to the manifest; the viewer never changes.

- **Viewer** (Go binary) — reads `${AGENT_CAROUSEL_DIR:-${CLAUDE_STATUS_DIR:-/tmp/claude-status}}/images/<pane>.jsonl`,
  renders the carousel via the kitty graphics protocol (or chafa fallback).
- **Adapters** (`adapters/`) — per-agent capture. Today: `claude-code/`
  (a Claude Code PostToolUse hook + plugin + skill).

Extracted from [lazytmux](https://github.com/noamsto/lazytmux), which consumes
this repo as a flake input.

## Status

Live standalone repo — the viewer binary, Claude Code capture adapter, and
plugin skill all build and are consumed by
[lazytmux](https://github.com/noamsto/lazytmux) as a flake input.

See [docs/2026-06-10-design.md](docs/2026-06-10-design.md) for the design
notes. Zoom/pan (Plan 2) is not yet implemented.
