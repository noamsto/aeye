# Backfill manifest on resume (#44) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** On `claude --resume`, rebuild the per-pane image/diagram manifest from the session transcript so the carousel is populated instead of empty.

**Architecture:** Extract path-extraction and `.d2` rendering from `images.sh`/`diagrams.sh` into a shared `lib/manifest-extract.sh`. Add a `session-backfill.sh` SessionStart hook that replays the transcript line-by-line as synthetic hook payloads through those shared helpers, appending dedup-guarded manifest entries (ts from the transcript) and claiming the manifest via the `.owner` sidecar.

**Tech Stack:** Bash, `jq`, `bats` (tests), `d2`/`resvg` (diagram rendering, stubbed in tests).

---

## File structure

- Create: `adapters/claude-code/plugin/scripts/lib/manifest-extract.sh` — pure extraction + render helpers (`extract_image_path`, `extract_d2_path`, `d2_png_for`, `d2_render`).
- Modify: `adapters/claude-code/plugin/scripts/images.sh` — source the lib, call `extract_image_path`.
- Modify: `adapters/claude-code/plugin/scripts/diagrams.sh` — source the lib, call `extract_d2_path` + `d2_render`.
- Create: `adapters/claude-code/plugin/scripts/session-backfill.sh` — the replay hook.
- Modify: `adapters/claude-code/plugin/hooks/hooks.json` — register the new SessionStart hook.
- Create: `tests/manifest-extract.bats` — unit tests for the lib.
- Create: `tests/session-backfill.bats` — backfill behavior tests.
- Create: `tests/fixtures/transcript-basic.jsonl` — fixture transcript.
- Modify: `tests/hooks-json.bats` — assert the new hook is registered.

The reference logic to extract lives in `images.sh:36-85` (resolve/extract image path) and `diagrams.sh:19-100` (resolve `.d2` + render). Read both before starting Task 1.

---

### Task 1: Extract `extract_image_path` into the lib and rewire `images.sh`

**Files:**
- Create: `adapters/claude-code/plugin/scripts/lib/manifest-extract.sh`
- Create: `tests/manifest-extract.bats`
- Modify: `adapters/claude-code/plugin/scripts/images.sh:36-87`

- [ ] **Step 1: Write the failing lib unit test**

Create `tests/manifest-extract.bats`:

```bash
#!/usr/bin/env bats

setup() {
	LIB="$(dirname "$BATS_TEST_DIRNAME")/adapters/claude-code/plugin/scripts/lib/manifest-extract.sh"
	IMG="$BATS_TEST_TMPDIR/pic.png"
	printf 'x' >"$IMG"
	# shellcheck source=/dev/null
	source "$LIB"
}

@test "extract_image_path: tool_input.file_path that exists" {
	payload="$(jq -nc --arg p "$IMG" '{cwd:"/work",tool_input:{file_path:$p},tool_response:{}}')"
	run extract_image_path "$payload"
	[ "$status" -eq 0 ]
	[ "$output" = "$IMG" ]
}

@test "extract_image_path: missing file -> empty" {
	payload="$(jq -nc '{cwd:"/work",tool_input:{file_path:"/nope/x.png"},tool_response:{}}')"
	run extract_image_path "$payload"
	[ "$status" -eq 0 ]
	[ -z "$output" ]
}

@test "extract_image_path: relative path resolved against cwd" {
	mkdir -p "$BATS_TEST_TMPDIR/proj"
	printf 'x' >"$BATS_TEST_TMPDIR/proj/shot.png"
	payload="$(jq -nc --arg c "$BATS_TEST_TMPDIR/proj" '{cwd:$c,tool_input:{output_path:"shot.png"},tool_response:{}}')"
	run extract_image_path "$payload"
	[ "$output" = "$BATS_TEST_TMPDIR/proj/shot.png" ]
}

@test "extract_image_path: phase-2 scan of tool_response" {
	payload="$(jq -nc --arg p "$IMG" '{cwd:"/work",tool_input:{},tool_response:{content:[{type:"text",text:("saved to "+$p)}]}}')"
	run extract_image_path "$payload"
	[ "$output" = "$IMG" ]
}

@test "extract_image_path: non-image payload -> empty" {
	payload="$(jq -nc '{cwd:"/work",tool_input:{command:"ls"},tool_response:{}}')"
	run extract_image_path "$payload"
	[ -z "$output" ]
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `bats tests/manifest-extract.bats`
Expected: FAIL — `manifest-extract.sh` does not exist (source error).

- [ ] **Step 3: Create the lib with `extract_image_path`**

Create `adapters/claude-code/plugin/scripts/lib/manifest-extract.sh`. Move the extraction logic verbatim from `images.sh` into a function that echoes the path or nothing and always returns 0:

```bash
#!/usr/bin/env bash
# Shared path-extraction + d2-render helpers for the image/diagram hooks and the
# resume backfill. Pure: no manifest writes, no keying, no toggle. Each function
# echoes a result (or nothing) and returns 0 so callers under `set -euo pipefail`
# are never aborted by a "not found" outcome.

# extract_image_path PAYLOAD -> echoes a resolved, existing image path or nothing.
# Two phases mirror the live images.sh: explicit tool_input paths, then a scan of
# tool_response strings for an embedded path.
extract_image_path() {
	local payload="$1" cwd p candidate response_path
	cwd="$(jq -r '.cwd // empty' <<<"$payload" 2>/dev/null)"

	# Fast-bail before jq unless the raw payload mentions an image extension.
	shopt -s nocasematch
	if [[ ! $payload =~ \.(png|jpe?g|gif|webp|bmp) ]]; then
		shopt -u nocasematch
		return 0
	fi
	shopt -u nocasematch

	local resolve is_ext
	resolve() { # $1 path -> resolved against cwd if relative
		local q="$1"
		[[ $q != /* && -n $cwd ]] && q="$cwd/$q"
		printf '%s' "$q"
	}
	is_ext() { [[ ${1,,} =~ \.(png|jpe?g|gif|webp|bmp)$ ]]; }

	# Phase 1: explicit tool_input paths.
	for p in \
		"$(jq -r '.tool_input.file_path // empty' <<<"$payload" 2>/dev/null)" \
		"$(jq -r '.tool_input.path // empty' <<<"$payload" 2>/dev/null)" \
		"$(jq -r '.tool_input.output_path // empty' <<<"$payload" 2>/dev/null)"; do
		[[ -n $p ]] || continue
		candidate="$(resolve "$p")"
		is_ext "$candidate" || continue
		[[ -f $candidate ]] || continue
		printf '%s' "$candidate"
		return 0
	done

	# Phase 2: scan tool_response strings for an embedded path.
	response_path="$(jq -r '
    [.tool_response | .. | strings
      | select(length < 4096)
      | capture("(?<p>(?:/|\\./)[^\\s]*\\.(?:png|jpe?g|gif|webp|bmp))"; "i")
      | .p
    ] | first // empty
  ' <<<"$payload" 2>/dev/null)"
	if [[ -n $response_path ]]; then
		response_path="$(resolve "$response_path")"
		if is_ext "$response_path" && [[ -f $response_path ]]; then
			printf '%s' "$response_path"
		fi
	fi
	return 0
}
```

- [ ] **Step 4: Run the lib test to verify it passes**

Run: `bats tests/manifest-extract.bats`
Expected: PASS (5 tests).

- [ ] **Step 5: Rewire `images.sh` to use the lib**

In `images.sh`, after the `payload` is read (keep lines 1-20 keying/payload), replace the inline fast-bail + Phase 1 + Phase 2 block (current lines 22-87, including `resolve_path`/`is_image_ext`) with a source + call. Keep `source_tool` (the manifest needs it). Result around the extraction section:

```bash
# shellcheck source=lib/manifest-extract.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/manifest-extract.sh"

source_tool="$(jq -r '.tool_name // "?"' <<<"$payload" 2>/dev/null)"
path="$(extract_image_path "$payload")"
[[ -n $path ]] || exit 0
```

Everything from `mtime=` (current line 89) onward — owner self-heal, append — stays unchanged.

- [ ] **Step 6: Run the images.sh suite to verify zero behavior change**

Run: `bats tests/adapter.bats`
Expected: PASS (all existing tests, including the regex-DoS guard and owner self-heal).

- [ ] **Step 7: shellcheck both files**

Run: `shellcheck adapters/claude-code/plugin/scripts/lib/manifest-extract.sh adapters/claude-code/plugin/scripts/images.sh`
Expected: no warnings.

- [ ] **Step 8: Commit**

```bash
git add adapters/claude-code/plugin/scripts/lib/manifest-extract.sh adapters/claude-code/plugin/scripts/images.sh tests/manifest-extract.bats
git commit -m "refactor(hooks): extract image-path resolution into shared lib"
```

---

### Task 2: Add `.d2` extraction + render to the lib and rewire `diagrams.sh`

**Files:**
- Modify: `adapters/claude-code/plugin/scripts/lib/manifest-extract.sh`
- Modify: `tests/manifest-extract.bats`
- Modify: `adapters/claude-code/plugin/scripts/diagrams.sh:19-100`

- [ ] **Step 1: Write the failing lib tests for d2 helpers**

Append to `tests/manifest-extract.bats`:

```bash
@test "extract_d2_path: existing .d2 file_path" {
	d2="$BATS_TEST_TMPDIR/flow.d2"
	printf 'a -> b\n' >"$d2"
	payload="$(jq -nc --arg p "$d2" '{cwd:"/work",tool_input:{file_path:$p}}')"
	run extract_d2_path "$payload"
	[ "$output" = "$d2" ]
}

@test "extract_d2_path: a .png file_path -> empty" {
	payload="$(jq -nc '{cwd:"/work",tool_input:{file_path:"/x/pic.png"}}')"
	run extract_d2_path "$payload"
	[ -z "$output" ]
}

@test "d2_png_for: hash-stable png path under the diagrams dir" {
	d2="$BATS_TEST_TMPDIR/flow.d2"
	printf 'a -> b\n' >"$d2"
	run d2_png_for "$d2" "/tmp/diagrams"
	[ "$status" -eq 0 ]
	[[ $output == /tmp/diagrams/*.png ]]
	# stable for identical content
	first="$output"
	run d2_png_for "$d2" "/tmp/diagrams"
	[ "$output" = "$first" ]
}

@test "d2_render: renders png via stubbed d2/resvg" {
	STUB="$BATS_TEST_TMPDIR/bin"
	mkdir -p "$STUB"
	printf '#!/usr/bin/env bash\nprintf "<svg/>" >"${@: -1}"\n' >"$STUB/d2"
	printf '#!/usr/bin/env bash\nprintf "PNG" >"${@: -1}"\n' >"$STUB/resvg"
	chmod +x "$STUB/d2" "$STUB/resvg"
	export PATH="$STUB:$PATH"
	d2="$BATS_TEST_TMPDIR/flow.d2"
	printf 'a -> b\n' >"$d2"
	run d2_render "$d2" "$BATS_TEST_TMPDIR/diagrams"
	[ "$status" -eq 0 ]
	[ -f "$output" ]
	[[ $output == *.png ]]
}
```

- [ ] **Step 2: Run to verify failure**

Run: `bats tests/manifest-extract.bats`
Expected: FAIL — `extract_d2_path`/`d2_png_for`/`d2_render` not defined.

- [ ] **Step 3: Add the d2 helpers to the lib**

Append to `manifest-extract.sh`. `d2_png_for` and `d2_render` carry the render block from `diagrams.sh` (d2 → fix-fonts → svg-contrast → resvg), minus the markdown-warn, append, and toggle (those stay in `diagrams.sh`). `d2_render` renders only when the png is absent; renderers missing → returns 1 with no output:

```bash
# extract_d2_path PAYLOAD -> echoes a resolved, existing .d2 path or nothing.
extract_d2_path() {
	local payload="$1" cwd candidate
	cwd="$(jq -r '.cwd // empty' <<<"$payload" 2>/dev/null)"
	candidate="$(jq -r '.tool_input.file_path // empty' <<<"$payload" 2>/dev/null)"
	[[ -n $candidate ]] || return 0
	[[ $candidate != /* && -n $cwd ]] && candidate="$cwd/$candidate"
	[[ ${candidate,,} == *.d2 ]] || return 0
	[[ -f $candidate ]] || return 0
	printf '%s' "$candidate"
	return 0
}

# d2_png_for SRC DIAGRAMS_DIR -> echoes the cache png path (hash of source content).
d2_png_for() {
	local src="$1" dir="$2" hash
	hash="$(sha256sum "$src" | cut -c1-16)"
	printf '%s/%s.png' "$dir" "$hash"
}

# d2_render SRC DIAGRAMS_DIR -> renders SRC to a cached png (if absent) and echoes
# the png path. Returns 1 (no output) when renderers are missing or rendering
# fails (failure is logged to render-errors.log). Does not append or toggle.
d2_render() {
	local src="$1" dir="$2" png svg err
	png="$(d2_png_for "$src" "$dir")"
	svg="${png%.png}.svg"
	mkdir -p "$dir"

	if [[ -f $png ]]; then
		printf '%s' "$png"
		return 0
	fi

	local d2_bin="${AEYE_D2:-d2}" resvg_bin="${AEYE_RESVG:-resvg}"
	command -v "$d2_bin" >/dev/null 2>&1 || return 1
	command -v "$resvg_bin" >/dev/null 2>&1 || return 1
	err="$dir/$(basename "${png%.png}").err"

	local now
	_d2_log_err() { # $1 message; logs and cleans partials
		printf -v now '%(%FT%T%z)T' -1
		printf '%s\t%s\t%s\n' "$now" "$(basename "${png%.png}")" "$1" \
			>>"$dir/render-errors.log"
		rm -f "$svg" "$err" "$png"
	}

	local d2_args=(-t "${AEYE_D2_THEME:-105}")
	[[ ${AEYE_D2_SKETCH:-1} != 0 ]] && d2_args+=(--sketch)
	if ! "$d2_bin" "${d2_args[@]}" "$src" "$svg" 2>"$err"; then
		_d2_log_err "$(tr '\n' ' ' <"$err")"
		return 1
	fi

	if ! bash "$(dirname "${BASH_SOURCE[0]}")/../d2-fix-fonts.sh" "$svg" 2>>"$err"; then
		_d2_log_err "$(tr '\n' ' ' <"$err")"
		return 1
	fi

	local contrast_bin
	contrast_bin="$(command -v "${AEYE_BIN:-aeye}" 2>/dev/null || true)"
	[[ -n $contrast_bin ]] && "$contrast_bin" svg-contrast "$svg" 2>>"$err" || true

	local resvg_args=()
	if [[ -n ${AEYE_D2_FONT_DIR:-} ]]; then
		resvg_args+=(--skip-system-fonts --use-fonts-dir "$AEYE_D2_FONT_DIR")
	fi
	if ! "$resvg_bin" "${resvg_args[@]}" "$svg" "$png" 2>>"$err"; then
		_d2_log_err "$(tr '\n' ' ' <"$err")"
		return 1
	fi
	rm -f "$err"
	printf '%s' "$png"
	return 0
}
```

Note: `d2-fix-fonts.sh` is now referenced as `../d2-fix-fonts.sh` because the lib lives one directory deeper than `diagrams.sh`.

- [ ] **Step 4: Run lib tests to verify pass**

Run: `bats tests/manifest-extract.bats`
Expected: PASS (9 tests).

- [ ] **Step 5: Rewire `diagrams.sh`**

Keep `diagrams.sh` lines 1-17 (keying/payload). Replace the candidate resolution (19-29), the hash/png/svg derivation (32-34), and the render block (39-100) with lib calls, preserving the markdown-warn (gated on a fresh render), the owner self-heal, the dedup-append, and the toggle. The middle of `diagrams.sh` becomes:

```bash
# shellcheck source=lib/manifest-extract.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/manifest-extract.sh"

candidate="$(extract_d2_path "$payload")"
[[ -n $candidate ]] || exit 0

mkdir -p "$DIAGRAMS_DIR"
png="$(d2_png_for "$candidate" "$DIAGRAMS_DIR")"
svg="${png%.png}.svg"
manifest="$IMAGES_DIR/$pane_file.jsonl"

# Fresh render (png absent before) is the only time the markdown check applies.
was_missing=1
[[ -f $png ]] && was_missing=0
d2_render "$candidate" "$DIAGRAMS_DIR" >/dev/null || exit 0

# d2 emits |md / |markdown as an HTML <foreignObject>, which resvg can't paint —
# those nodes rasterize blank while d2 exits 0. Warn the agent. Non-blocking.
if [[ $was_missing -eq 1 ]] && grep -q '<foreignObject' "$svg"; then
	printf -v now '%(%FT%T%z)T' -1
	printf '%s\t%s\tWARN markdown block(s) render blank in resvg (<foreignObject>)\n' \
		"$now" "$(basename "$candidate")" >>"$DIAGRAMS_DIR/render-errors.log"
	warn="$(basename "$candidate") contains markdown (|md / |markdown) block(s) that render BLANK in the carousel: resvg can't paint the HTML <foreignObject> that D2 emits for markdown. Rewrite those node bodies as plain quoted labels (use \\n for line breaks)."
	jq -nc --arg ctx "$warn" \
		'{hookSpecificOutput:{hookEventName:"PostToolUse",additionalContext:$ctx}}'
fi
```

The owner self-heal block (current 102-112), the dedup-append (114-124), and the toggle (126-131) stay unchanged below this.

- [ ] **Step 6: Run the diagram suites**

Run: `bats tests/diagrams.bats tests/d2-render-real.bats`
Expected: PASS for both (render, dedup, markdown-warn, toggle, theme/sketch/font-dir args, contrast, real-d2 regression).

- [ ] **Step 7: shellcheck**

Run: `shellcheck adapters/claude-code/plugin/scripts/lib/manifest-extract.sh adapters/claude-code/plugin/scripts/diagrams.sh`
Expected: no warnings.

- [ ] **Step 8: Commit**

```bash
git add adapters/claude-code/plugin/scripts/lib/manifest-extract.sh adapters/claude-code/plugin/scripts/diagrams.sh tests/manifest-extract.bats
git commit -m "refactor(hooks): extract d2 resolution + render into shared lib"
```

---

### Task 3: Create `session-backfill.sh` and its tests

**Files:**
- Create: `adapters/claude-code/plugin/scripts/session-backfill.sh`
- Create: `tests/fixtures/transcript-basic.jsonl`
- Create: `tests/session-backfill.bats`

- [ ] **Step 1: Create the fixture transcript**

Create `tests/fixtures/transcript-basic.jsonl`. `IMGPATH` and `DOTD2` are substituted by the test. One image Read (appears twice → dedup), one `.d2` Write, one non-image Bash, and a non-resume line is not needed here (gating is tested via stdin source):

```jsonl
{"type":"assistant","cwd":"/work","timestamp":"2026-06-16T10:00:01.000Z","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"ls"}}]}}
{"type":"assistant","cwd":"/work","timestamp":"2026-06-16T10:00:02.000Z","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"IMGPATH"}}]}}
{"type":"assistant","cwd":"/work","timestamp":"2026-06-16T10:00:03.000Z","message":{"content":[{"type":"tool_use","name":"Write","input":{"file_path":"DOTD2"}}]}}
{"type":"assistant","cwd":"/work","timestamp":"2026-06-16T10:00:04.000Z","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"IMGPATH"}}]}}
```

- [ ] **Step 2: Write the failing backfill tests**

Create `tests/session-backfill.bats`:

```bash
#!/usr/bin/env bats

setup() {
	export CLAUDE_STATUS_DIR="$BATS_TEST_TMPDIR/state"
	export TMUX_PANE="%7"
	export CLAUDE_CODE_SESSION_ID="sess-resume"
	MANIFEST="$CLAUDE_STATUS_DIR/images/7.jsonl"
	OWNER="$CLAUDE_STATUS_DIR/images/7.owner"
	APP="$(dirname "$BATS_TEST_DIRNAME")/adapters/claude-code/plugin/scripts/session-backfill.sh"

	IMG="$BATS_TEST_TMPDIR/pic.png"
	printf 'x' >"$IMG"
	DOTD2="$BATS_TEST_TMPDIR/flow.d2"
	printf 'a -> b\n' >"$DOTD2"

	TRANSCRIPT="$BATS_TEST_TMPDIR/transcript.jsonl"
	sed -e "s#IMGPATH#$IMG#g" -e "s#DOTD2#$DOTD2#g" \
		"$BATS_TEST_DIRNAME/fixtures/transcript-basic.jsonl" >"$TRANSCRIPT"

	# Stub d2/resvg so .d2 backfill renders hermetically.
	STUB="$BATS_TEST_TMPDIR/bin"
	mkdir -p "$STUB"
	printf '#!/usr/bin/env bash\nprintf "<svg/>" >"${@: -1}"\n' >"$STUB/d2"
	printf '#!/usr/bin/env bash\nprintf "PNG" >"${@: -1}"\n' >"$STUB/resvg"
	chmod +x "$STUB/d2" "$STUB/resvg"
	export PATH="$STUB:$PATH"
}

run_app() { # $1 = source
	jq -nc --arg s "$1" --arg t "$TRANSCRIPT" '{source:$s,transcript_path:$t}' | bash "$APP"
}

@test "resume backfills the image (deduped to one line)" {
	run_app resume
	[ -f "$MANIFEST" ]
	run grep -c "\"path\":\"$IMG\"" "$MANIFEST"
	[ "$output" -eq 1 ]
}

@test "resume backfills the rendered diagram" {
	run_app resume
	run jq -rc 'select(.source=="d2") | .path' "$MANIFEST"
	[[ $output == *"/diagrams/"*.png ]]
	[ -f "$output" ]
}

@test "ts is taken from the transcript (chronological)" {
	run_app resume
	run jq -r 'select(.path=="'"$IMG"'") | .ts' "$MANIFEST"
	[ "$output" = "2026-06-16T10:00:02.000Z" ]
}

@test "resume claims the manifest via the owner sidecar" {
	run_app resume
	run cat "$OWNER"
	[ "$output" = "sess-resume" ]
}

@test "dedup against a pre-seeded manifest -> no double entry" {
	mkdir -p "$CLAUDE_STATUS_DIR/images"
	printf '{"type":"image","path":"%s","source":"Read","ts":"old","mtime":0}\n' "$IMG" >"$MANIFEST"
	run_app resume
	run grep -c "\"path\":\"$IMG\"" "$MANIFEST"
	[ "$output" -eq 1 ]
}

@test "non-resume source is a no-op" {
	run_app startup
	[ ! -f "$MANIFEST" ]
}

@test "missing transcript_path -> clean exit 0, no manifest" {
	run bash -c 'printf "%s" "{\"source\":\"resume\"}" | bash "'"$APP"'"'
	[ "$status" -eq 0 ]
	[ ! -f "$MANIFEST" ]
}

@test "backfill does not open the carousel" {
	# A tmux-claude-images stub would record calls; assert it is never invoked.
	printf '#!/usr/bin/env bash\necho called >>"%s"\n' "$BATS_TEST_TMPDIR/toggle.log" >"$STUB/tmux-claude-images"
	chmod +x "$STUB/tmux-claude-images"
	run_app resume
	[ ! -f "$BATS_TEST_TMPDIR/toggle.log" ]
}
```

- [ ] **Step 3: Run to verify failure**

Run: `bats tests/session-backfill.bats`
Expected: FAIL — `session-backfill.sh` does not exist.

- [ ] **Step 4: Implement `session-backfill.sh`**

Create `adapters/claude-code/plugin/scripts/session-backfill.sh`:

```bash
#!/usr/bin/env bash
# SessionStart(resume) hook: rebuild this pane/session's image manifest from the
# session transcript, so the carousel is populated after `claude --resume` instead
# of empty. Replays each image/diagram-bearing transcript line as a synthetic hook
# payload through the shared extractors. Reads the hook JSON on stdin.
set -euo pipefail

payload="$(cat)"
[[ -n $payload ]] || exit 0
[[ "$(jq -r '.source // empty' <<<"$payload" 2>/dev/null)" == resume ]] || exit 0

transcript="$(jq -r '.transcript_path // empty' <<<"$payload" 2>/dev/null)"
[[ -n $transcript && -r $transcript ]] || exit 0

STATE_DIR="${AEYE_DIR:-${CLAUDE_STATUS_DIR:-/tmp/claude-status}}"
IMAGES_DIR="$STATE_DIR/images"
DIAGRAMS_DIR="$IMAGES_DIR/diagrams"

pane_id="${TMUX_PANE:-${CLAUDE_CODE_SESSION_ID:-}}"
[[ -n $pane_id ]] || exit 0
pane_file="${pane_id#%}"
[[ $pane_file =~ ^[A-Za-z0-9_@:.-]+$ ]] || exit 0

# shellcheck source=lib/manifest-extract.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/manifest-extract.sh"

manifest="$IMAGES_DIR/$pane_file.jsonl"
mkdir -p "$IMAGES_DIR"

# Seed the seen-set with paths already in the manifest, then track within-run
# appends, so a kept-manifest resume and a repeated transcript path never double.
declare -A seen=()
if [[ -f $manifest ]]; then
	while IFS= read -r p; do [[ -n $p ]] && seen["$p"]=1; done \
		< <(jq -r '.path // empty' "$manifest" 2>/dev/null)
fi

append_image() { # $1 path  $2 source  $3 ts
	[[ -n ${seen["$1"]:-} ]] && return 0
	seen["$1"]=1
	local mtime
	mtime="$(stat -c %Y "$1" 2>/dev/null || echo 0)"
	jq -nc --arg path "$1" --arg source "$2" --arg ts "$3" --argjson mtime "$mtime" \
		'{type:"image", path:$path, source:$source, ts:$ts, mtime:$mtime}' >>"$manifest"
}

append_diagram() { # $1 png  $2 svg  $3 ts
	[[ -n ${seen["$1"]:-} ]] && return 0
	seen["$1"]=1
	local mtime
	mtime="$(stat -c %Y "$1" 2>/dev/null || echo 0)"
	jq -nc --arg path "$1" --arg vector "$2" --arg source "d2" --arg ts "$3" --argjson mtime "$mtime" \
		'{type:"image", path:$path, vector:$vector, source:$source, ts:$ts, mtime:$mtime}' >>"$manifest"
}

# Only image/diagram-bearing lines reach jq (raw grep fast-bail, like images.sh).
while IFS= read -r line; do
	cwd="$(jq -r '.cwd // empty' <<<"$line" 2>/dev/null)"
	ts="$(jq -r '.timestamp // empty' <<<"$line" 2>/dev/null)"

	# An assistant tool_use line -> synthetic {tool_name, tool_input, cwd}.
	while IFS= read -r tu; do
		[[ -n $tu ]] || continue
		synth="$(jq -nc --argjson tu "$tu" --arg cwd "$cwd" \
			'{tool_name:$tu.name, tool_input:$tu.input, tool_response:{}, cwd:$cwd}')"
		img="$(extract_image_path "$synth")"
		if [[ -n $img ]]; then
			append_image "$img" "$(jq -r '.name' <<<"$tu")" "$ts"
			continue
		fi
		d2="$(extract_d2_path "$synth")"
		if [[ -n $d2 ]]; then
			png="$(d2_render "$d2" "$DIAGRAMS_DIR")" || continue
			append_diagram "$png" "${png%.png}.svg" "$ts"
		fi
	done < <(jq -c '.message.content[]? | select(.type=="tool_use")' <<<"$line" 2>/dev/null)

	# A user tool_result line -> synthetic {tool_response, cwd} for screenshot paths.
	while IFS= read -r tr; do
		[[ -n $tr ]] || continue
		synth="$(jq -nc --argjson tr "$tr" --arg cwd "$cwd" \
			'{tool_name:"?", tool_input:{}, tool_response:$tr, cwd:$cwd}')"
		img="$(extract_image_path "$synth")"
		[[ -n $img ]] && append_image "$img" "screenshot" "$ts"
	done < <(jq -c '.message.content[]? | select(.type=="tool_result") | .content' <<<"$line" 2>/dev/null)
done < <(grep -nE '\.(png|jpe?g|gif|webp|bmp|d2)' "$transcript" | cut -d: -f2-)

# Claim the rebuilt manifest so the live hooks' owner self-heal does not drop it.
if [[ -f $manifest && -n ${CLAUDE_CODE_SESSION_ID:-} ]]; then
	printf '%s' "$CLAUDE_CODE_SESSION_ID" >"$IMAGES_DIR/$pane_file.owner"
fi
```

- [ ] **Step 5: Run the backfill tests to verify pass**

Run: `bats tests/session-backfill.bats`
Expected: PASS (8 tests).

- [ ] **Step 6: shellcheck**

Run: `shellcheck adapters/claude-code/plugin/scripts/session-backfill.sh`
Expected: no warnings. (If SC2030/SC2031 fire on the `seen` map inside subshells, they will not — appends run in the main shell, not a pipe; the `while` reads from process substitution.)

- [ ] **Step 7: Commit**

```bash
git add adapters/claude-code/plugin/scripts/session-backfill.sh tests/session-backfill.bats tests/fixtures/transcript-basic.jsonl
git commit -m "feat(hooks): backfill manifest from transcript on resume (#44)"
```

---

### Task 4: Register the hook and assert it

**Files:**
- Modify: `adapters/claude-code/plugin/hooks/hooks.json`
- Modify: `tests/hooks-json.bats`

- [ ] **Step 1: Add the failing assertion**

Append to `tests/hooks-json.bats`:

```bash
@test "SessionStart runs session-backfill.sh" {
	run jq -e '[.hooks.SessionStart[].hooks[].command] | any(test("session-backfill.sh"))' "$HOOKS"
	[ "$status" -eq 0 ]
}
```

- [ ] **Step 2: Run to verify failure**

Run: `bats tests/hooks-json.bats`
Expected: FAIL on the new test.

- [ ] **Step 3: Register the hook**

In `hooks.json`, add a third SessionStart entry after `session-reset.sh`:

```json
      {
        "hooks": [
          {"type": "command", "command": "\"${CLAUDE_PLUGIN_ROOT}\"/scripts/session-backfill.sh"}
        ]
      }
```

- [ ] **Step 4: Run to verify pass**

Run: `bats tests/hooks-json.bats`
Expected: PASS (all tests, including JSON-validity).

- [ ] **Step 5: Commit**

```bash
git add adapters/claude-code/plugin/hooks/hooks.json tests/hooks-json.bats
git commit -m "feat(hooks): register session-backfill SessionStart hook"
```

---

### Task 5: Full suite + PR

- [ ] **Step 1: Run the entire bats suite**

Run: `bats tests/`
Expected: PASS across all files (adapter, diagrams, d2-*, session-reset, session-backfill, manifest-extract, hooks-json, guidance, toggle, skill-examples-render).

- [ ] **Step 2: shellcheck every touched script**

Run: `shellcheck adapters/claude-code/plugin/scripts/*.sh adapters/claude-code/plugin/scripts/lib/*.sh`
Expected: no warnings.

- [ ] **Step 3: Open the PR**

```bash
git push -u origin feat/44-backfill-manifest-on-resume
gh pr create --assignee @me --title "feat(hooks): backfill image/diagram manifest on resume (#44)" --body "Closes #44. Rebuilds the per-pane manifest from the session transcript on \`claude --resume\` so the carousel is populated instead of empty. Extraction + d2 render are factored into a shared lib reused by the live hooks; backfill claims the manifest via the owner sidecar and preserves transcript ts ordering. Known limitation: screenshots whose path is not in tool_input and not persisted as text in the tool_result are not recovered."
```

---

## Self-review notes

- **Spec coverage:** lib extraction (Tasks 1-2), resume-only gating + transcript replay + ts ordering + dedup + owner claim (Task 3), hook registration (Task 4), existing-suite-stays-green (Steps in 1/2 + Task 5). Known-limitation documented in the PR body. All spec sections map to a task.
- **Type/name consistency:** `extract_image_path`, `extract_d2_path`, `d2_png_for`, `d2_render` are used with the same signatures in the lib, `images.sh`, `diagrams.sh`, and `session-backfill.sh`.
- **No placeholders:** every code step shows full content; every run step states the command and expected result.
