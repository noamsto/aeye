# Installing aeye (agent guide)

A step-by-step install you can follow autonomously. After each step, **run the
verification command and confirm the output** before moving on — don't assume.

aeye has two parts:

1. **Two PATH binaries** — the `aeye` viewer and the
   `tmux-claude-images` toggle that opens it. The toggle launches the viewer
   into a fresh tmux/kitty pane, which resolves `aeye` from *that
   pane's* PATH — so **both must be on PATH**, not just the toggle.
2. **A capture adapter** — the Claude Code plugin (a PostToolUse hook) that
   records every image the session touches into a per-pane manifest, so the
   carousel has something to show.

You need both: binaries to *render*, plugin to *capture*.

## Step 0 — Check the host

The carousel renders into a tmux split or a kitty window. It needs **one** of:

- **tmux** — `[ -n "$TMUX" ]` is true, or `tmux info` succeeds.
- **kitty with remote control** — `[ -n "$KITTY_LISTEN_ON" ]`, which requires
  `allow_remote_control yes` + `listen_on …` in `kitty.conf`.

```bash
[ -n "$TMUX" ] && echo "host: tmux" || { [ -n "$KITTY_LISTEN_ON" ] && echo "host: kitty" || echo "NO HOST — install tmux or enable kitty remote control"; }
```

If neither is present the toggle just prints a hint and does nothing — fix the
host before continuing.

> Note: if you run tmux *inside* kitty, tmux always wins (the viewer opens as a
> tmux split). The kitty-window path is only for kitty **without** tmux.

## Step 1 — Install the two binaries

Pick the method that fits the environment. **Prefer prebuilt binaries unless Nix
is already in use** — it's toolchain-free and ships both entrypoints.

**A. Prebuilt binaries (no toolchain — recommended default).** Linux/macOS,
amd64/arm64. Downloads the viewer *and* the toggle:

```bash
os=$(uname -s | tr '[:upper:]' '[:lower:]'); arch=$(uname -m)
case "$arch" in x86_64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; esac
mkdir -p ~/.local/bin   # must be on PATH
curl -fsSL "https://github.com/noamsto/aeye/releases/latest/download/aeye_${os}_${arch}.tar.gz" \
  | tar -xz -C ~/.local/bin aeye tmux-claude-images
```

(Or download an archive manually from
<https://github.com/noamsto/aeye/releases> and extract both onto PATH.)

**B. Nix (each installs separately — run both):**

```bash
nix profile install github:noamsto/aeye          # viewer  (packages.default)
nix profile install github:noamsto/aeye#toggle   # toggle  (tmux-claude-images)
```

**C. Go (viewer only — still need the toggle):**

```bash
go install github.com/noamsto/aeye@latest   # installs ONLY the viewer
```

The toggle is a shell script, not a Go binary, so `go install` can't fetch it —
get `tmux-claude-images` from the release archive (method A) or
`scripts/tmux-claude-images.sh` in this repo, and put it on PATH.

> Already running [lazytmux](https://github.com/noamsto/lazytmux)? Both binaries
> are already on PATH and `prefix + I` is bound — skip to Step 3.

### Verify

```bash
command -v aeye && command -v tmux-claude-images && echo "OK: both on PATH"
```

Both paths must print. If only one does, the missing half isn't installed —
revisit this step before continuing (the toggle is useless without the viewer,
and vice-versa).

## Step 2 — Optional dependencies

- **`chafa`** — block-art fallback for terminals without the kitty graphics
  protocol. Not needed on kitty/ghostty (they render directly). Install only if
  the target terminal is neither.
- **`d2` + `resvg`** — let the agent draw [D2](https://d2lang.com) diagrams that
  render into the carousel. Without them the diagram hook no-ops silently;
  install only if diagrams are wanted.

```bash
command -v chafa || echo "(optional) chafa not installed — needed only off kitty/ghostty"
command -v d2 && command -v resvg && echo "diagrams: ready" || echo "(optional) diagrams need d2 + resvg"
```

## Step 3 — Install the capture adapter for your agent

### Claude Code

These are **Claude Code slash commands**, run inside the Claude Code session
(not the shell):

```
/plugin marketplace add noamsto/aeye
/plugin install aeye@aeye
```

This repo doubles as its own single-plugin marketplace; both the marketplace and
the plugin are named `aeye`. The plugin only *captures* — opening the
carousel still uses the `tmux-claude-images` binary from Step 1.

### Codex CLI

Run in a shell (not inside Codex). The marketplace root is `adapters/codex/`,
not the repo root:

```bash
codex plugin marketplace add <path-to-aeye>/adapters/codex
codex plugin add aeye@aeye
```

Then, **inside** Codex, run `/hooks` once to review and trust the plugin's
hooks — Codex won't run them until you do. Trust is keyed to the hooks' hash,
so a plugin update requires re-running `/hooks`.

<!-- TODO(codex): finalize once Nix wiring lands (Nix/Home-Manager auto-install
for the Codex plugin is not done yet). -->

## Step 4 — Smoke test

1. Cause an image to be captured — e.g. have the session `Read` any
   `.png`/`.jpg`, or take a screenshot. The hook appends it to the manifest at
   `${AEYE_DIR:-${CLAUDE_STATUS_DIR:-/tmp/claude-status}}/images/<pane>.jsonl`.
2. Confirm the manifest exists and is non-empty:

   ```bash
   ls -l "${AEYE_DIR:-${CLAUDE_STATUS_DIR:-/tmp/claude-status}}"/images/*.jsonl
   ```

3. Open the carousel:

   ```bash
   tmux-claude-images
   ```

   It should open a split/window showing a preview + filmstrip. Run it again to
   close. (If no images are captured yet, in tmux it prints
   `no images yet for this pane` — that's expected, not a failure.)

**Done when:** both binaries are on PATH (Step 1), the plugin is installed
(Step 3), and `tmux-claude-images` opens the carousel after an image is captured
(Step 4).

## Reference — environment variables

| Variable | Purpose |
|---|---|
| `AEYE_BIN` | Override the viewer binary the toggle launches (default: `aeye` on PATH). |
| `AEYE_DIR` | State dir for manifests. Falls back to `CLAUDE_STATUS_DIR`, then `/tmp/claude-status`. |
| `CLAUDE_STATUS_DIR` | Secondary state-dir fallback (shared with claude-status tooling). |
| `AEYE_D2` / `AEYE_RESVG` | Override the `d2` / `resvg` binaries used for diagram rendering. |
