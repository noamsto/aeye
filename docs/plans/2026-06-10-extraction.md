# agent-carousel Extraction Implementation Plan (Plan 1 of 2)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Carve lazytmux's image carousel into a standalone, agent-agnostic repo (`github.com/noamsto/agent-carousel`) — a Go viewer binary + Claude capture adapter + its own plugin/skill — and rewire lazytmux to consume it as a flake input, with the carousel behaving exactly as it does today.

**Architecture:** The Go viewer is already agent-agnostic — it renders a per-key manifest (`{path,source,ts,mtime}` JSONL, keyed by tmux pane id *or* `$CLAUDE_CODE_SESSION_ID`). The new repo owns the viewer binary, the dual-mode (tmux split / `kitty @ launch`) toggle, and a self-contained Claude Code plugin (PostToolUse capture hook + `image-gallery` skill). lazytmux consumes the binary + toggle package and registers the plugin; its own claude-status plugin keeps its PostToolUse `status.sh` hook (the two plugins' hooks coexist).

**Tech Stack:** Go 1.25 (bubbletea/v2, lipgloss/v2, golang.org/x/image), Nix flake-parts, git-hooks.nix, Claude Code plugin (hooks.json + skills), bats (adapter tests).

**Reference state:** lazytmux HEAD `0396874` (PR #47 — dual-mode kitty/tmux carousel — is merged). The Go viewer files are untouched by #47; only the shell scripts/flake/plugin changed.

---

## File Structure

**New repo `agent-carousel/` (sibling of lazytmux; already `git init`-ed on `main` with `docs/` + `README.md`):**

| Path | Responsibility |
|------|----------------|
| `flake.nix` | flake-parts; `packages.default` = viewer binary `agent-carousel`; `packages.toggle` = `tmux-claude-images` script with the binary path baked in; devShell; `nix flake check` (go test + pre-commit). |
| `go.mod` / `go.sum` | module `github.com/noamsto/agent-carousel`. |
| `main.go` | entry point — `runGallery(key)` from `os.Args[1]`. No `--gallery` multiplexing. |
| `gallery.go`, `gallery_render.go`, `gallery_cache.go` | the viewer, moved verbatim from `picker/`. |
| `theme.go` | `detectTheme()` — moved from `picker/main.go:982`. |
| `gallery_test.go`, `gallery_cache_test.go` | moved verbatim. |
| `scripts/tmux-claude-images.sh` | dual-mode toggle; `@picker_generate@` → the Nix-baked binary path. |
| `adapters/claude-code/plugin/.claude-plugin/plugin.json` | plugin manifest. |
| `adapters/claude-code/plugin/.claude-plugin/marketplace.json` | the repo's own marketplace. |
| `adapters/claude-code/plugin/hooks/hooks.json` | `PostToolUse` → `images.sh`. |
| `adapters/claude-code/plugin/scripts/images.sh` | self-contained capture (folds in `claude-images-update.sh`). |
| `adapters/claude-code/plugin/skills/image-gallery/SKILL.md` | the skill, moved. |
| `docs/MANIFEST.md` | the manifest JSONL contract. |
| `.envrc`, `.gitignore`, `treefmt.toml` | repo hygiene (bootstrap-nix-repo conventions). |
| `tests/adapter.bats` | adapter capture + toggle-resolution tests (ported from lazytmux). |

**lazytmux edits (the carve):**

| Path | Change |
|------|--------|
| `flake.nix` | add `agent-carousel` input; thread `agent-carousel` packages into `config/tmux.conf.nix`. |
| `picker/main.go` | delete the `--gallery` dispatch block (lines 103–113); delete `detectTheme` (982–998). |
| `picker/gallery.go`, `gallery_render.go`, `gallery_cache.go`, `gallery_test.go`, `gallery_cache_test.go` | delete. |
| `config/tmux.conf.nix` | drop `claude-images-update`/`tmux-claude-images` from `scriptNames`; drop `tmux-claude-images` from `scriptsWithIcons`; bind `prefix+I` to the agent-carousel toggle package; remove the now-unused gallery `@picker_generate@` path if nothing else uses it (the session/window picker still uses `picker-generate-bin`, so keep that). |
| `claude-plugin/hooks/hooks.json` | remove the `images.sh` line from `PostToolUse`. |
| `claude-plugin/scripts/images.sh` | delete. |
| `claude-plugin/skills/image-gallery/` | delete. |
| `modules/home-manager.nix` | register the agent-carousel plugin/skill (mirror the existing skills symlink). |
| `tests/claude-images.bats`, `tests/claude-images-launch.bats` | delete (moved to the new repo). |

---

## Part A — Stand up the agent-carousel repo (green build)

### Task 1: Bring the Go viewer into the new repo

**Files:**
- Create: `agent-carousel/go.mod`, `agent-carousel/go.sum`
- Create: `agent-carousel/{gallery.go,gallery_render.go,gallery_cache.go,gallery_test.go,gallery_cache_test.go,theme.go,main.go}`

- [ ] **Step 1: Copy the Go viewer files + go.mod/go.sum verbatim**

```bash
cd /home/noams/Data/git/noamsto
cp lazytmux/picker/gallery.go        agent-carousel/gallery.go
cp lazytmux/picker/gallery_render.go agent-carousel/gallery_render.go
cp lazytmux/picker/gallery_cache.go  agent-carousel/gallery_cache.go
cp lazytmux/picker/gallery_test.go   agent-carousel/gallery_test.go
cp lazytmux/picker/gallery_cache_test.go agent-carousel/gallery_cache_test.go
cp lazytmux/picker/go.mod agent-carousel/go.mod
cp lazytmux/picker/go.sum agent-carousel/go.sum
```

- [ ] **Step 2: Rename the module**

Edit `agent-carousel/go.mod` line 1:

```
module github.com/noamsto/agent-carousel
```

- [ ] **Step 3: Create `agent-carousel/theme.go`** (moved `detectTheme` from `picker/main.go`)

```go
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// detectTheme reads the light/dark theme from theme-state.json, defaulting to
// "dark" when absent or unparseable.
func detectTheme() string {
	xdg := os.Getenv("XDG_STATE_HOME")
	if xdg == "" {
		xdg = filepath.Join(os.Getenv("HOME"), ".local", "state")
	}
	data, err := os.ReadFile(filepath.Join(xdg, "theme-state.json"))
	if err != nil {
		return "dark"
	}
	var cfg struct {
		Theme string `json:"theme"`
	}
	if json.Unmarshal(data, &cfg) != nil || cfg.Theme == "" {
		return "dark"
	}
	return cfg.Theme
}
```

- [ ] **Step 4: Create `agent-carousel/main.go`** (standalone entry; no `--gallery` multiplexing)

```go
package main

import (
	"fmt"
	"os"
)

// usage: agent-carousel <key>
// <key> is a tmux pane id (%N) or a Claude Code session id — whatever the
// capture adapter used to name the manifest file.
func main() {
	key := ""
	if len(os.Args) > 1 {
		key = os.Args[1]
	}
	if err := runGallery(key); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

- [ ] **Step 5: Tidy and test**

```bash
cd /home/noams/Data/git/noamsto/agent-carousel
go mod tidy
go vet ./...
go test ./...
```

Expected: `go vet` silent (exit 0); `go test` prints `ok  github.com/noamsto/agent-carousel`.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum gallery.go gallery_render.go gallery_cache.go gallery_test.go gallery_cache_test.go theme.go main.go
git commit -m "feat: agent-agnostic carousel viewer (extracted from lazytmux)"
```

### Task 2: Add the Nix flake

**Files:**
- Create: `agent-carousel/flake.nix`, `agent-carousel/.envrc`, `agent-carousel/.gitignore`

- [ ] **Step 1: Write `agent-carousel/flake.nix`**

```nix
{
  description = "agent-carousel — a tmux/kitty image carousel for coding agents.";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-parts.url = "github:hercules-ci/flake-parts";
    git-hooks-nix.url = "github:cachix/git-hooks.nix";
    git-hooks-nix.inputs.nixpkgs.follows = "nixpkgs";
  };

  outputs = inputs @ {flake-parts, ...}:
    flake-parts.lib.mkFlake {inherit inputs;} {
      imports = [inputs.git-hooks-nix.flakeModule];

      systems = ["x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin"];

      perSystem = {
        config,
        pkgs,
        lib,
        self',
        ...
      }: {
        pre-commit.settings.hooks = {
          gofmt.enable = true;
          govet.enable = true;
          golangci-lint.enable = true;
          shellcheck.enable = true;
          shfmt.enable = true;
          typos.enable = true;
          check-merge-conflicts.enable = true;
          trim-trailing-whitespace.enable = true;
        };

        devShells.default = pkgs.mkShell {
          inherit (config.pre-commit) shellHook;
          packages =
            config.pre-commit.settings.enabledPackages
            ++ [pkgs.go pkgs.gopls pkgs.gotools pkgs.golangci-lint pkgs.chafa pkgs.bats];
        };

        packages = {
          default = pkgs.buildGoModule {
            pname = "agent-carousel";
            version = "0.1.0";
            src = ./.;
            vendorHash = null; # set after `nix build` prints the expected hash
            doCheck = true;
            meta = {
              description = "tmux/kitty image carousel for coding agents";
              mainProgram = "agent-carousel";
              license = lib.licenses.mit;
            };
          };

          # The dual-mode toggle with the binary path baked in (no @placeholder@).
          toggle = pkgs.writeShellApplication {
            name = "tmux-claude-images";
            runtimeInputs = [self'.packages.default];
            text = builtins.replaceStrings ["@picker_generate@"] ["agent-carousel"]
              (builtins.readFile ./scripts/tmux-claude-images.sh);
          };
        };

        apps.default = {
          type = "app";
          program = "${self'.packages.default}/bin/agent-carousel";
        };
      };
    };
}
```

- [ ] **Step 2: Write `.envrc` and `.gitignore`**

`.envrc`:
```
use flake
```

`.gitignore`:
```
result
result-*
.direnv
```

- [ ] **Step 3: Build to discover the vendorHash**

```bash
cd /home/noams/Data/git/noamsto/agent-carousel
nix build .#default 2>&1 | grep -A2 "got:"
```

Expected: a `got: sha256-...` line. Copy that value.

- [ ] **Step 4: Set the real `vendorHash`** in `flake.nix` (replace `vendorHash = null;` with the `got:` value), then build clean:

```bash
nix build .#default
./result/bin/agent-carousel --help 2>/dev/null; echo "exit $?"
nix build .#toggle && ./result/bin/tmux-claude-images --resolve
```

Expected: `nix build .#default` succeeds; `.#toggle --resolve` prints a `MODE\tKEY\tMANIFEST` line (or `none\t\t` outside tmux/kitty).

- [ ] **Step 5: Commit**

```bash
git add flake.nix flake.lock .envrc .gitignore
git commit -m "build: flake-parts flake (viewer binary + toggle package + checks)"
```

### Task 3: Bring the dual-mode toggle script

**Files:**
- Create: `agent-carousel/scripts/tmux-claude-images.sh`

- [ ] **Step 1: Copy the toggle script verbatim**

```bash
mkdir -p /home/noams/Data/git/noamsto/agent-carousel/scripts
cp /home/noams/Data/git/noamsto/lazytmux/scripts/tmux-claude-images.sh \
   /home/noams/Data/git/noamsto/agent-carousel/scripts/tmux-claude-images.sh
```

The script keeps its `@picker_generate@` token — `flake.nix` (Task 2) replaces it with `agent-carousel` at build time. No edit to the script body is needed; the two invocation sites (`launch_tmux`, `launch_kitty`) both call `@picker_generate@ --gallery '$KEY'`.

- [ ] **Step 2: The binary no longer takes `--gallery`** — update the two call sites to drop the flag (the standalone binary takes the key directly).

In `scripts/tmux-claude-images.sh`, replace both occurrences of:
```
@picker_generate@ --gallery '$KEY'
```
and
```
@picker_generate@ --gallery "$KEY"
```
with (respectively):
```
@picker_generate@ '$KEY'
```
and
```
@picker_generate@ "$KEY"
```

- [ ] **Step 3: shellcheck + rebuild toggle**

```bash
cd /home/noams/Data/git/noamsto/agent-carousel
shellcheck scripts/tmux-claude-images.sh
nix build .#toggle && ./result/bin/tmux-claude-images --resolve
```

Expected: shellcheck clean; `--resolve` prints a resolution line.

- [ ] **Step 4: Commit**

```bash
git add scripts/tmux-claude-images.sh
git commit -m "feat: dual-mode (tmux/kitty) carousel toggle"
```

### Task 4: Bring the Claude capture adapter + plugin

**Files:**
- Create: `agent-carousel/adapters/claude-code/plugin/{.claude-plugin/plugin.json,.claude-plugin/marketplace.json,hooks/hooks.json,scripts/images.sh,skills/image-gallery/SKILL.md}`

- [ ] **Step 1: Move the capture logic in as a self-contained plugin script**

The lazytmux plugin's `images.sh` was a thin wrapper around the PATH binary `claude-images-update`. In the standalone plugin, fold the full capture logic in so the plugin works without Nix. Copy the capture body:

```bash
mkdir -p /home/noams/Data/git/noamsto/agent-carousel/adapters/claude-code/plugin/scripts
cp /home/noams/Data/git/noamsto/lazytmux/scripts/claude-images-update.sh \
   /home/noams/Data/git/noamsto/agent-carousel/adapters/claude-code/plugin/scripts/images.sh
```

This script is already self-contained (reads the PostToolUse JSON on stdin, keys by `$TMUX_PANE`/`$CLAUDE_CODE_SESSION_ID`, appends to `$IMAGES_DIR/<key>.jsonl`). No edits needed — it degrades to a no-op outside tmux/kitty with no key.

- [ ] **Step 2: Write `hooks/hooks.json`**

```json
{
  "hooks": {
    "PostToolUse": [
      {
        "hooks": [
          {"type": "command", "command": "\"${CLAUDE_PLUGIN_ROOT}\"/scripts/images.sh"}
        ]
      }
    ]
  }
}
```

- [ ] **Step 3: Write `.claude-plugin/plugin.json`**

```json
{
  "name": "agent-carousel",
  "version": "0.1.0",
  "description": "Capture images this Claude Code session touches and browse them in a tmux/kitty carousel.",
  "hooks": "./hooks/hooks.json",
  "skills": "./skills"
}
```

- [ ] **Step 4: Write `.claude-plugin/marketplace.json`**

```json
{
  "name": "agent-carousel",
  "owner": {"name": "noamsto"},
  "plugins": [
    {
      "name": "agent-carousel",
      "source": "./",
      "description": "Image carousel for coding agents (Claude Code capture adapter)."
    }
  ]
}
```

- [ ] **Step 5: Move the skill** (and fix the command reference — the toggle command name is unchanged: `tmux-claude-images`)

```bash
mkdir -p /home/noams/Data/git/noamsto/agent-carousel/adapters/claude-code/plugin/skills
cp -r /home/noams/Data/git/noamsto/lazytmux/claude-plugin/skills/image-gallery \
      /home/noams/Data/git/noamsto/agent-carousel/adapters/claude-code/plugin/skills/image-gallery
```

The skill already references the `tmux-claude-images` command and `prefix + I`, both preserved. No edit needed.

- [ ] **Step 6: shellcheck the adapter script**

```bash
shellcheck /home/noams/Data/git/noamsto/agent-carousel/adapters/claude-code/plugin/scripts/images.sh
```

Expected: clean.

- [ ] **Step 7: Commit**

```bash
cd /home/noams/Data/git/noamsto/agent-carousel
git add adapters
git commit -m "feat: Claude Code capture adapter + self-contained plugin"
```

### Task 5: Document the manifest contract + port adapter tests

**Files:**
- Create: `agent-carousel/docs/MANIFEST.md`
- Create: `agent-carousel/tests/adapter.bats`

- [ ] **Step 1: Write `docs/MANIFEST.md`**

```markdown
# Manifest contract

The viewer reads a per-key manifest at
`${AGENT_CAROUSEL_DIR:-${CLAUDE_STATUS_DIR:-/tmp/claude-status}}/images/<key>.jsonl`.

- `<key>` is a tmux pane id with the leading `%` stripped, **or** a coding-agent
  session id (e.g. `$CLAUDE_CODE_SESSION_ID`) — whichever the capture adapter
  used to launch the viewer.
- One JSON object per line:

  ```json
  {"type":"image","path":"/abs/path.png","source":"Read","ts":"2026-06-10T12:00:00+0000","mtime":1717977600}
  ```

  - `path` — absolute path to an image file (png/jpe?g/gif/webp/bmp).
  - `source` — free-form producer tag (the agent's tool name, e.g. `Read`/`Write`).
  - `ts` — ISO-8601 capture time.
  - `mtime` — source file mtime (epoch seconds), used for dedup.

- Consumers MUST tolerate duplicate `(path, mtime)` lines (concurrent adapter
  firings).
- Any producer that appends valid lines shows up in the viewer. New agents are
  added as new adapters under `adapters/<agent>/`; the viewer never changes.
```

- [ ] **Step 2: Port the adapter tests** — copy lazytmux's bats, fix the script path to the in-repo adapter script

```bash
mkdir -p /home/noams/Data/git/noamsto/agent-carousel/tests
cp /home/noams/Data/git/noamsto/lazytmux/tests/claude-images.bats \
   /home/noams/Data/git/noamsto/agent-carousel/tests/adapter.bats
```

Then edit `tests/adapter.bats`: repoint the script-under-test from the
lazytmux Nix path to `adapters/claude-code/plugin/scripts/images.sh` (search the
file for the `claude-images-update` / script path setup and replace with the
adapter path; the test logic — payload on stdin, assert manifest line — is
unchanged).

- [ ] **Step 3: Run the bats**

```bash
cd /home/noams/Data/git/noamsto/agent-carousel
nix develop -c bats tests/adapter.bats
```

Expected: all tests pass.

- [ ] **Step 4: `nix flake check` green**

```bash
nix flake check
```

Expected: succeeds (pre-commit hooks + go tests pass).

- [ ] **Step 5: Commit**

```bash
git add docs/MANIFEST.md tests/adapter.bats
git commit -m "docs: manifest contract; test: port adapter capture tests"
```

### Task 6: Push the repo so lazytmux can consume it by ref

- [ ] **Step 1: Create the GitHub repo and push**

```bash
cd /home/noams/Data/git/noamsto/agent-carousel
gh repo create noamsto/agent-carousel --private --source=. --remote=origin --push
```

Expected: repo created, `main` pushed.

- [ ] **Step 2: Capture the current revision** for pinning lazytmux's input:

```bash
git rev-parse HEAD
```

---

## Part B — Carve lazytmux to consume agent-carousel

> Order matters (spec "Deployment note"): wire the input + keep the carousel working **before** deleting, all on one branch, so `main` never has a broken intermediate.

### Task 7: Create the carve branch + worktree

- [ ] **Step 1: From lazytmux, create an isolated worktree on a feature branch** (no GitHub issue yet → drop the id per the branch-naming convention)

```bash
cd /home/noams/Data/git/noamsto/lazytmux
wt switch -c refactor/extract-agent-carousel
```

Expected: worktree created and the post-switch hook navigates to its tmux window. Run all remaining lazytmux steps from this worktree.

### Task 8: Add the flake input + thread the packages

**Files:**
- Modify: `lazytmux/flake.nix`
- Modify: `lazytmux/config/tmux.conf.nix`

- [ ] **Step 1: Add the input** to `lazytmux/flake.nix` `inputs` (after the `tmux-state` block):

```nix
    agent-carousel = {
      url = "github:noamsto/agent-carousel";
      inputs.nixpkgs.follows = "nixpkgs";
    };
```

- [ ] **Step 2: Thread the packages into the config import.** Find where `flake.nix` calls `import config/tmux.conf.nix` (near `tmux-state-pkg = inputs.tmux-state.packages.${pkgs.system}.default;`, line ~127) and add alongside it:

```nix
              carousel-bin = inputs.agent-carousel.packages.${pkgs.system}.default;
              carousel-toggle = inputs.agent-carousel.packages.${pkgs.system}.toggle;
```

Pass both into the `import ../config/tmux.conf.nix { ... }` argument set (mirror how `tmux-state-pkg` is passed).

- [ ] **Step 3: Accept the new args** in `config/tmux.conf.nix`'s function head (where `tmux-state-pkg` is accepted) — add `carousel-bin`, `carousel-toggle`.

- [ ] **Step 4: Build to confirm the input resolves** (lock the new input first):

```bash
nix flake lock
nix build .#default 2>&1 | tail -5
```

Expected: builds (carousel still served by the OLD in-tree path — nothing deleted yet).

- [ ] **Step 5: Commit**

```bash
git add flake.nix flake.lock config/tmux.conf.nix
git commit -m "build: add agent-carousel flake input"
```

### Task 9: Repoint prefix+I + drop the in-tree scripts from packaging

**Files:**
- Modify: `lazytmux/config/tmux.conf.nix`

- [ ] **Step 1: Repoint the `prefix + I` keybind** (currently `config/tmux.conf.nix:409`):

Replace:
```nix
    bind I run-shell '${script.tmux-claude-images}/bin/tmux-claude-images'
```
with:
```nix
    bind I run-shell '${carousel-toggle}/bin/tmux-claude-images'
```

- [ ] **Step 2: Remove the carousel scripts from `scriptNames`** (`config/tmux.conf.nix:186-189` region) — delete the `"claude-images-update"` and `"tmux-claude-images"` entries.

- [ ] **Step 3: Remove `tmux-claude-images` from `scriptsWithIcons`** (line 208) — it no longer exists in lazytmux.

- [ ] **Step 4: Build**

```bash
nix build .#default 2>&1 | tail -5
```

Expected: builds. (The session/window picker still uses `picker-generate-bin`; only the gallery scripts are gone.)

- [ ] **Step 5: Commit**

```bash
git add config/tmux.conf.nix
git commit -m "refactor: bind prefix+I to agent-carousel toggle; drop in-tree carousel scripts"
```

### Task 10: Delete the gallery Go files + dispatch

**Files:**
- Delete: `lazytmux/picker/{gallery.go,gallery_render.go,gallery_cache.go,gallery_test.go,gallery_cache_test.go}`
- Modify: `lazytmux/picker/main.go`

- [ ] **Step 1: Delete the gallery files**

```bash
cd /home/noams/Data/git/noamsto/lazytmux   # (the worktree path)
gtrash put picker/gallery.go picker/gallery_render.go picker/gallery_cache.go picker/gallery_test.go picker/gallery_cache_test.go
```

- [ ] **Step 2: Remove the `--gallery` dispatch block** in `picker/main.go` (the `for i, a := range args { if a == "--gallery" { ... } }` block, ~lines 102–114). Delete the entire loop; `main()` proceeds directly to the existing `flags := map[string]bool{}` line.

- [ ] **Step 3: Remove `detectTheme`** from `picker/main.go` (~lines 982–998) — it moved to agent-carousel and nothing else in `picker/` uses it.

- [ ] **Step 4: Verify nothing else references the removed symbols**

```bash
cd picker
grep -rn "runGallery\|detectTheme\|--gallery" *.go
go build ./... && go test ./...
```

Expected: grep returns nothing; build + tests pass.

- [ ] **Step 5: Commit**

```bash
cd /home/noams/Data/git/noamsto/lazytmux
git add picker
git commit -m "refactor: remove gallery viewer from picker (moved to agent-carousel)"
```

### Task 11: Split the plugin + remove the skill

**Files:**
- Modify: `lazytmux/claude-plugin/hooks/hooks.json`
- Delete: `lazytmux/claude-plugin/scripts/images.sh`, `lazytmux/claude-plugin/skills/image-gallery/`
- Delete: `lazytmux/tests/claude-images.bats`, `lazytmux/tests/claude-images-launch.bats`

- [ ] **Step 1: Remove the `images.sh` line from `PostToolUse`** in `claude-plugin/hooks/hooks.json`. The block becomes:

```json
    "PostToolUse": [
      {
        "hooks": [
          {"type": "command", "command": "\"${CLAUDE_PLUGIN_ROOT}\"/scripts/status.sh processing"}
        ]
      }
    ],
```

- [ ] **Step 2: Delete the moved files**

```bash
gtrash put claude-plugin/scripts/images.sh
gtrash put -r claude-plugin/skills/image-gallery
gtrash put tests/claude-images.bats tests/claude-images-launch.bats
```

- [ ] **Step 3: Verify the JSON is valid**

```bash
jq . claude-plugin/hooks/hooks.json >/dev/null && echo OK
```

Expected: `OK`.

- [ ] **Step 4: Commit**

```bash
git add claude-plugin tests
git commit -m "refactor: drop image capture + skill from lazytmux plugin (moved to agent-carousel)"
```

### Task 12: Register the agent-carousel plugin/skill in the HM module

**Files:**
- Modify: `lazytmux/modules/home-manager.nix`
- Modify: `lazytmux/flake.nix` (thread the carousel plugin path to the module if needed)

- [ ] **Step 1: Expose the agent-carousel plugin source to the module.** The module currently symlinks `../claude-plugin/skills/*` when `cfg.skills.enable` (lines 437–441). agent-carousel's skill now lives in its flake source. Thread `inputs.agent-carousel` (or its plugin path) into the module args, then symlink the agent-carousel skill alongside lazytmux's:

In `modules/home-manager.nix`, where the skills `home.file` attrset is built (around line 437), add the agent-carousel skill set. Given the agent-carousel plugin path
`${inputs.agent-carousel}/adapters/claude-code/plugin/skills`:

```nix
        // lib.optionalAttrs cfg.skills.enable (
          lib.mapAttrs' (name: _: {
            name = ".claude/skills/${name}";
            value.source = "${carouselPluginSkills}/${name}";
          }) (builtins.readDir carouselPluginSkills)
        )
```

where `carouselPluginSkills` is passed from `flake.nix` as
`"${inputs.agent-carousel}/adapters/claude-code/plugin/skills"`.

- [ ] **Step 2: Update the `skills.enable` description** (line ~284) to note both the lazytmux and agent-carousel skills are installed unless the plugins are installed via marketplace.

- [ ] **Step 3: Build the HM module check**

```bash
cd /home/noams/Data/git/noamsto/lazytmux
nix flake check 2>&1 | tail -15
```

Expected: succeeds.

- [ ] **Step 4: Commit**

```bash
git add flake.nix modules/home-manager.nix
git commit -m "feat: register agent-carousel skill via HM module"
```

### Task 13: Full verification of the carve

- [ ] **Step 1: Build + flake check**

```bash
cd /home/noams/Data/git/noamsto/lazytmux
nix build .#default
nix flake check
```

Expected: both succeed.

- [ ] **Step 2: Manual smoke test** (in a kitty terminal, inside tmux)

```bash
./result/bin/tmux -L carousel-test new-session -d
# attach, read an image with a Claude session in a pane, press prefix+I
```

Verify: `prefix + I` opens the carousel split; `h`/`l` navigate; `q` closes; the
chafa fallback works in a non-kitty terminal.

- [ ] **Step 3: Open a PR**

```bash
gh pr create --assignee @me --title "refactor: extract image carousel into agent-carousel repo" \
  --body "Carves the carousel viewer + Claude capture adapter + skill into github.com/noamsto/agent-carousel; lazytmux now consumes it as a flake input. Viewer behavior unchanged. See agent-carousel/docs/2026-06-10-design.md."
```

---

## Self-Review

**Spec coverage:** standalone repo (Tasks 1–6) ✓; agent-agnostic viewer + `adapters/claude-code/` split (Task 4) ✓; manifest contract (Task 5) ✓; lazytmux consumes via input (Tasks 8–12) ✓; `detectTheme` straggler (Task 1 Step 3) ✓; dual-mode toggle (Task 3) ✓; pane-or-session keying (Task 5 MANIFEST.md) ✓; deploy ordering — wire before delete (Part B order) ✓; command-name preserved `tmux-claude-images` (Task 4 Step 5) ✓.

**Deferred (Plan 2 / future):** zoom/pan → Plan 2; D2 diagrams, non-kitty zoom, other adapters → future.

**Open verification at execution time:** the exact `flake.nix` lines that thread `tmux-state-pkg` (Task 8) — match the surrounding pattern rather than assuming line numbers, as the file may have drifted. The bats port (Task 5 Step 2) needs the engineer to read the source test's script-path setup and repoint it.
