# D2 Text Rendering (Phase 0) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the `.d2 → carousel` render pipeline produce legible diagrams — text (incl. bold/italic) renders, theme follows light/dark mode, and a regression test locks it in.

**Architecture:** `resvg` ignores d2's embedded `@font-face` fonts and drops every label. Fix it in `diagrams.sh`: after `d2` emits the SVG, rewrite the synthetic font-family names to an installed family (and inject `font-weight`/`font-style` so bold/italic select the right face), then run `resvg` against a bundled font dir hermetically. Theme + sketch are passed to `d2` from env, so the nix wrapper can pick a theme per light/dark mode.

**Tech Stack:** bash, `d2` 0.7.x, `resvg` 0.47, `sed`, bats, Nix (flake-parts).

**Spec:** `docs/2026-06-14-d2-diagrams-rich-and-legible-design.md`

---

## File Structure

| File | Responsibility |
|---|---|
| `adapters/claude-code/plugin/scripts/d2-fix-fonts.sh` | **new** — pure SVG transform: remap d2's synthetic font names + inject bold/italic. Isolated so it is unit-testable without the full hook. |
| `adapters/claude-code/plugin/scripts/diagrams.sh` | **modify** — pass theme/sketch to `d2`; call the font-fix; run `resvg` hermetically when a font dir is set. |
| `tests/fixtures/d2-fonts-sample.svg` | **new** — minimal SVG carrying d2's synthetic font patterns + `.text-bold`/`.text-italic` rules, for the unit test. |
| `tests/d2-fix-fonts.bats` | **new** — unit tests for the transform. |
| `tests/diagrams.bats` | **modify** — assert the hook passes theme/sketch + font-dir flags; add a real-binary integration test asserting zero `No match`. |
| `flake.nix` | **modify** — add `source-sans` + `source-code-pro`; export `AGENT_CAROUSEL_D2_FONT*` in the devShell so tests + dev render hermetically. |

**Env contract (used by `diagrams.sh`):**
- `AGENT_CAROUSEL_D2_FONT` — family to remap to (default `Noto Sans`).
- `AGENT_CAROUSEL_D2_FONT_DIR` — optional; when set, resvg uses only this dir (`--skip-system-fonts --use-fonts-dir`).
- `AGENT_CAROUSEL_D2_THEME` — d2 theme id (default `105`, Buttered Toast). The nix wrapper sets this to `200` (Dark Mauve) in dark mode.
- `AGENT_CAROUSEL_D2_SKETCH` — `0` disables sketch; anything else (default) enables `--sketch`.

> **Downstream (not in this repo):** for the *deployed* hook to render hermetically in d2's own typeface, the nix-config Claude wrapper must export `AGENT_CAROUSEL_D2_FONT_DIR=<source-sans>/share/fonts/truetype` and `AGENT_CAROUSEL_D2_FONT="Source Sans 3"`. Tracked as a follow-up nix-config change, mirroring the lazytmux input bump.

---

## Task 1: Font-fix transform (pure, TDD)

**Files:**
- Create: `adapters/claude-code/plugin/scripts/d2-fix-fonts.sh`
- Create: `tests/fixtures/d2-fonts-sample.svg`
- Test: `tests/d2-fix-fonts.bats`

- [ ] **Step 1: Create the fixture SVG**

Create `tests/fixtures/d2-fonts-sample.svg` (mirrors d2's real output: prefixed, spaced CSS rules + quoted synthetic families):

```xml
<svg xmlns="http://www.w3.org/2000/svg"><style type="text/css"><![CDATA[
.d2-123 .text {font-family:"d2-123-font-regular"}
.d2-123 .text-bold {font-family:"d2-123-font-bold"}
.d2-123 .text-italic {font-family:"d2-123-font-italic"}
]]></style><text class="text-bold fill-N1">Hello</text></svg>
```

- [ ] **Step 2: Write the failing test**

Create `tests/d2-fix-fonts.bats`:

```bash
#!/usr/bin/env bats

setup() {
	APP="$(dirname "$BATS_TEST_DIRNAME")/adapters/claude-code/plugin/scripts/d2-fix-fonts.sh"
	SVG="$BATS_TEST_TMPDIR/s.svg"
	cp "$BATS_TEST_DIRNAME/fixtures/d2-fonts-sample.svg" "$SVG"
}

@test "removes every synthetic d2 font-family name" {
	bash "$APP" "$SVG"
	run grep -cE 'd2-[0-9]+-font-[a-z]+' "$SVG"
	[ "$output" -eq 0 ]
}

@test "remaps to the default family (Noto Sans) when no override" {
	bash "$APP" "$SVG"
	run grep -c 'Noto Sans' "$SVG"
	[ "$output" -ge 1 ]
}

@test "honors AGENT_CAROUSEL_D2_FONT override" {
	AGENT_CAROUSEL_D2_FONT="Source Sans 3" bash "$APP" "$SVG"
	run grep -c 'Source Sans 3' "$SVG"
	[ "$output" -ge 1 ]
}

@test "injects font-weight on the bold rule and font-style on the italic rule" {
	bash "$APP" "$SVG"
	run grep -c 'font-weight:bold' "$SVG"
	[ "$output" -eq 1 ]
	run grep -c 'font-style:italic' "$SVG"
	[ "$output" -eq 1 ]
}
```

- [ ] **Step 3: Run the test, verify it fails**

Run: `bats tests/d2-fix-fonts.bats`
Expected: FAIL — `d2-fix-fonts.sh` does not exist.

- [ ] **Step 4: Implement the transform**

Create `adapters/claude-code/plugin/scripts/d2-fix-fonts.sh`:

```bash
#!/usr/bin/env bash
# Rewrite d2's embedded @font-face family names in an SVG to an installed
# family, and inject weight/style so bold/italic labels pick the right face.
# resvg ignores @font-face, so without this every text label is dropped.
set -euo pipefail

svg="$1"
family="${AGENT_CAROUSEL_D2_FONT:-Noto Sans}"

sed -E \
	-e "s/d2-[0-9]+-font-[a-z]+/${family}/g" \
	-e 's/(\.text-bold \{)/\1font-weight:bold;/g' \
	-e 's/(\.text-italic \{)/\1font-style:italic;/g' \
	"$svg" >"$svg.tmp"
mv "$svg.tmp" "$svg"
```

- [ ] **Step 5: Run the test, verify it passes**

Run: `bats tests/d2-fix-fonts.bats`
Expected: PASS (4 tests).

- [ ] **Step 6: Shellcheck**

Run: `shellcheck adapters/claude-code/plugin/scripts/d2-fix-fonts.sh`
Expected: no warnings.

- [ ] **Step 7: Commit**

```bash
git add adapters/claude-code/plugin/scripts/d2-fix-fonts.sh tests/fixtures/d2-fonts-sample.svg tests/d2-fix-fonts.bats
git commit -m "feat(diagrams): font-fix transform so resvg renders d2 text"
```

---

## Task 2: Wire theme + sketch + font-fix into diagrams.sh

**Files:**
- Modify: `adapters/claude-code/plugin/scripts/diagrams.sh:38-60` (the render block)
- Test: `tests/diagrams.bats` (including the shared `setup()` stubs)

> **Why the stub fix below matters:** the current `setup()` stubs write the SVG/PNG to `$2`, which assumes `d2 <in> <out>`. Once the hook passes `-t <theme> --sketch`, the output is no longer `$2` — every pre-existing test would break. Switch the stubs to `"${@: -1}"` (the last arg is always the output path) so they survive flag changes.

- [ ] **Step 1: Fix the shared stubs in `setup()` to write to the last arg**

In `tests/diagrams.bats`, change the `d2` stub body from `printf '<svg/>' >"$2"` to `printf '<svg/>' >"${@: -1}"`, and the `resvg` stub body from `printf 'PNG' >"$2"` to `printf 'PNG' >"${@: -1}"`. The two heredocs become:

```bash
	cat >"$STUB_BIN/d2" <<'STUB'
#!/usr/bin/env bash
printf '<svg/>' >"${@: -1}"
STUB
	cat >"$STUB_BIN/resvg" <<'STUB'
#!/usr/bin/env bash
printf 'PNG' >"${@: -1}"
STUB
```

- [ ] **Step 2: Write failing tests for the new hook behavior**

Append to `tests/diagrams.bats`. These override the stubs to log the args each binary receives:

```bash
@test "hook passes theme + sketch to d2" {
	cat >"$STUB_BIN/d2" <<'STUB'
#!/usr/bin/env bash
echo "$*" >>"$D2_ARGLOG"
printf '<svg/>' >"${@: -1}"
STUB
	chmod +x "$STUB_BIN/d2"
	export D2_ARGLOG="$BATS_TEST_TMPDIR/d2args.log"
	export AGENT_CAROUSEL_D2_THEME=200
	run_app hook-write-d2.json
	run cat "$D2_ARGLOG"
	[[ "$output" == *"-t 200"* ]]
	[[ "$output" == *"--sketch"* ]]
}

@test "AGENT_CAROUSEL_D2_SKETCH=0 disables sketch" {
	cat >"$STUB_BIN/d2" <<'STUB'
#!/usr/bin/env bash
echo "$*" >>"$D2_ARGLOG"
printf '<svg/>' >"${@: -1}"
STUB
	chmod +x "$STUB_BIN/d2"
	export D2_ARGLOG="$BATS_TEST_TMPDIR/d2args.log"
	export AGENT_CAROUSEL_D2_SKETCH=0
	run_app hook-write-d2.json
	run cat "$D2_ARGLOG"
	[[ "$output" != *"--sketch"* ]]
}

@test "font dir set -> resvg gets --skip-system-fonts --use-fonts-dir" {
	cat >"$STUB_BIN/resvg" <<'STUB'
#!/usr/bin/env bash
echo "$*" >>"$RESVG_ARGLOG"
printf 'PNG' >"${@: -1}"
STUB
	chmod +x "$STUB_BIN/resvg"
	export RESVG_ARGLOG="$BATS_TEST_TMPDIR/resvgargs.log"
	export AGENT_CAROUSEL_D2_FONT_DIR="$BATS_TEST_TMPDIR/fonts"
	mkdir -p "$AGENT_CAROUSEL_D2_FONT_DIR"
	run_app hook-write-d2.json
	run cat "$RESVG_ARGLOG"
	[[ "$output" == *"--skip-system-fonts"* ]]
	[[ "$output" == *"--use-fonts-dir $AGENT_CAROUSEL_D2_FONT_DIR"* ]]
}
```

- [ ] **Step 3: Run the new tests, verify they fail**

Run: `bats tests/diagrams.bats`
Expected: the 3 new tests FAIL (hook passes no `-t`/`--sketch`; resvg gets no font flags).

- [ ] **Step 4: Update the render block in diagrams.sh**

Replace lines 38-60 (the `if [[ ! -f $png ]]; then ... fi` block) with:

```bash
if [[ ! -f $png ]]; then
	d2_bin="${AGENT_CAROUSEL_D2:-d2}"
	resvg_bin="${AGENT_CAROUSEL_RESVG:-resvg}"
	command -v "$d2_bin" >/dev/null 2>&1 || exit 0
	command -v "$resvg_bin" >/dev/null 2>&1 || exit 0
	svg="$DIAGRAMS_DIR/$hash.svg"
	err="$DIAGRAMS_DIR/$hash.err"

	d2_args=(-t "${AGENT_CAROUSEL_D2_THEME:-105}")
	[[ "${AGENT_CAROUSEL_D2_SKETCH:-1}" != 0 ]] && d2_args+=(--sketch)
	if ! "$d2_bin" "${d2_args[@]}" "$candidate" "$svg" 2>"$err"; then
		printf -v now '%(%FT%T%z)T' -1
		printf '%s\t%s\t%s\n' "$now" "$hash" "$(tr '\n' ' ' <"$err")" \
			>>"$DIAGRAMS_DIR/render-errors.log"
		rm -f "$svg" "$err"
		exit 0
	fi

	# resvg can't use d2's embedded @font-face fonts; rewrite to an installed family.
	bash "$(dirname "${BASH_SOURCE[0]}")/d2-fix-fonts.sh" "$svg"

	resvg_args=()
	if [[ -n ${AGENT_CAROUSEL_D2_FONT_DIR:-} ]]; then
		resvg_args+=(--skip-system-fonts --use-fonts-dir "$AGENT_CAROUSEL_D2_FONT_DIR")
	fi
	if ! "$resvg_bin" "${resvg_args[@]}" "$svg" "$png" 2>>"$err"; then
		printf -v now '%(%FT%T%z)T' -1
		printf '%s\t%s\t%s\n' "$now" "$hash" "$(tr '\n' ' ' <"$err")" \
			>>"$DIAGRAMS_DIR/render-errors.log"
		rm -f "$svg" "$err" "$png"
		exit 0
	fi
	rm -f "$svg" "$err"
fi
```

- [ ] **Step 5: Run the full hook test suite, verify all pass**

Run: `bats tests/diagrams.bats`
Expected: PASS — the 3 new tests plus all pre-existing tests (now that the stubs write to the last arg).

- [ ] **Step 6: Shellcheck**

Run: `shellcheck adapters/claude-code/plugin/scripts/diagrams.sh`
Expected: no warnings.

- [ ] **Step 7: Commit**

```bash
git add adapters/claude-code/plugin/scripts/diagrams.sh tests/diagrams.bats
git commit -m "feat(diagrams): theme/sketch from env + hermetic font dir in render hook"
```

---

## Task 3: Real-binary regression test (zero No-match)

The unit + stub tests prove the transform and arg-wiring; this proves the *real* pipeline actually resolves all fonts. It skips cleanly when tools/fonts are absent.

**Files:**
- Create: `tests/d2-render-real.bats`

- [ ] **Step 1: Write the integration test**

Create `tests/d2-render-real.bats`:

```bash
#!/usr/bin/env bats
# Real d2 + resvg. Proves text fonts resolve (zero "No match for font-family").
# Skips when binaries or a usable font are unavailable.

setup() {
	FIX="$(dirname "$BATS_TEST_DIRNAME")/adapters/claude-code/plugin/scripts/d2-fix-fonts.sh"
	D2D="$BATS_TEST_TMPDIR/in.d2"
	SVG="$BATS_TEST_TMPDIR/in.svg"
	printf 'a: **bold** label\nb: _italic_ label\na -> b: edge\n' >"$D2D"
}

@test "real render resolves every font (no 'No match for font-family')" {
	command -v d2 >/dev/null || skip "d2 not installed"
	command -v resvg >/dev/null || skip "resvg not installed"

	d2 "$D2D" "$SVG"
	bash "$FIX" "$SVG"

	# Prefer the hermetic bundle when the env points at one; else system fonts.
	args=()
	if [[ -n ${AGENT_CAROUSEL_D2_FONT_DIR:-} ]]; then
		args=(--skip-system-fonts --use-fonts-dir "$AGENT_CAROUSEL_D2_FONT_DIR")
	fi
	run bash -c 'resvg "$@" "'"$SVG"'" "'"$BATS_TEST_TMPDIR"'/out.png" 2>&1' _ "${args[@]}"
	[[ "$output" != *"No match for font-family"* ]]
}
```

- [ ] **Step 2: Run it inside the devShell**

Run: `nix develop -c bats tests/d2-render-real.bats`
Expected: PASS (the devShell provides `d2`, `resvg`, and — after Task 4 — `AGENT_CAROUSEL_D2_FONT_DIR`). Without the font env it still passes via system fonts; if `d2`/`resvg` are missing it SKIPs.

- [ ] **Step 3: Commit**

```bash
git add tests/d2-render-real.bats
git commit -m "test(diagrams): real-render regression — zero unresolved fonts"
```

---

## Task 4: Bundle fonts in the devShell + env contract

**Files:**
- Modify: `flake.nix:38-43` (the devShell)

- [ ] **Step 1: Add fonts + export the font env in the devShell**

Replace the `devShells.default` block (lines 38-43) with:

```nix
        devShells.default = pkgs.mkShell {
          inherit (config.pre-commit) shellHook;
          AGENT_CAROUSEL_D2_FONT = "Source Sans 3";
          AGENT_CAROUSEL_D2_FONT_DIR = "${pkgs.source-sans}/share/fonts/truetype";
          packages =
            config.pre-commit.settings.enabledPackages
            ++ [pkgs.go pkgs.gopls pkgs.gotools pkgs.golangci-lint pkgs.chafa pkgs.bats pkgs.goreleaser pkgs.gh pkgs.d2 pkgs.resvg pkgs.source-sans pkgs.source-code-pro];
        };
```

- [ ] **Step 2: Verify the font dir exists and holds the faces**

Run: `nix develop -c bash -c 'ls "$AGENT_CAROUSEL_D2_FONT_DIR" | grep -E "SourceSans3-(Regular|Bold|It)\.ttf"'`
Expected: lists `SourceSans3-Regular.ttf`, `SourceSans3-Bold.ttf`, `SourceSans3-It.ttf`.

- [ ] **Step 3: Re-run the real-render test hermetically**

Run: `nix develop -c bats tests/d2-render-real.bats`
Expected: PASS — now exercising `--skip-system-fonts --use-fonts-dir <Source Sans 3>` with `AGENT_CAROUSEL_D2_FONT="Source Sans 3"`.

- [ ] **Step 4: Validate the flake**

Run: `nix flake check`
Expected: no eval errors.

- [ ] **Step 5: Commit**

```bash
git add flake.nix
git commit -m "build(diagrams): bundle Source Sans 3 + Source Code Pro for hermetic text rendering"
```

---

## Final verification

- [ ] **Run the whole bats suite in the devShell**

Run: `nix develop -c bats tests/`
Expected: all pass (unit transform, hook wiring, real render, plus pre-existing diagram/toggle tests).

- [ ] **Manual smoke (optional, in a kitty+tmux pane)**

Write a `.d2` with a bold/italic label to the scratch dir and confirm the carousel shows a diagram **with legible text**. (`AGENT_CAROUSEL_D2_THEME=200` to check dark.)

## Out of scope for Phase 0 (tracked elsewhere)
- nix-config Claude-wrapper export of `AGENT_CAROUSEL_D2_FONT_DIR`/`_FONT`/`_THEME`-by-mode (downstream integration, like the lazytmux bump).
- Rich authoring skill (Phase 1) and carousel display upgrades (Phase 2).
