# Plan: cursor resume backfill from agent transcripts (#171)

## Context already established (do not re-derive)

- Reference implementation to mirror: `adapters/codex/plugin/scripts/session-backfill.sh`
  (rebuilds the pane manifest from a transcript, authoritative rebuild, sole
  writer of a resumed pane's manifest; `session-reset.sh` defers to it).
- Cursor's `session-backfill.sh` is currently a 6-line no-op.
- Proven `sessionStart` contract (spike
  `docs/superpowers/spikes/2026-07-28-cursor-hook-contract.md`): `conversation_id`
  == `session_id` == `generation_id`; no `source` field; `workspace_roots[0]`
  instead of `cwd`; `transcript_path` null at sessionStart but set on
  `postToolUse` as `~/.cursor/projects/<slug>/agent-transcripts/<id>/<id>.jsonl`.
- `adapters/cursor/scripts/lib/shim.sh` already has `cursor_session_id`
  (`.conversation_id // .session_id`), `cursor_effective_cwd`
  (`.cwd` else `.workspace_roots[0]`), and `cursor_extract_touched_paths`
  (dispatches on `.tool_name` for Read/Write via `.tool_input.file_path` /
  `.path`, plus a `scan_response_image_path` pass over `.tool_output`,
  JSON-string-encoded, for embedded screenshot paths — Shell). This function
  returns BOTH image and `.d2` paths.
- `adapters/core/manifest-extract.sh` / `adapters/core/manifest-lifecycle.sh`
  are the canonical shared files; `adapters/cursor/scripts/core/` and
  `adapters/codex/plugin/scripts/core/` are synced copies
  (`just sync-cursor-core` / `just sync-codex-core`, justfile:82-89). This
  plan does not touch shared core logic, so no re-sync is needed.
- `adapters/cursor/scripts/session-reset.sh` has no resume branch today —
  every `sessionStart` clears + stamps the pane manifest unconditionally.
- Cursor config home is already overridable via `AEYE_CURSOR_HOME` (default
  `$HOME/.cursor`), used today by `install.sh`. The `projects/` dir that
  holds `agent-transcripts/` lives under this same root.

## Load-bearing assumptions (both unprovable in this environment — see below)

1. **`cursor-agent --resume` reuses the original `conversation_id`.** Live
   confirmation was blocked: this sandboxed worker has no `CURSOR_API_KEY`
   and no interactive browser login available, so `cursor-agent -p` cannot
   authenticate to produce a real payload to diff. Convergent secondary
   evidence supports the assumption holding (official CLI docs describe
   `--resume` as continuing "an existing thread"; the installed CLI's own
   `--new-session-id` flag is rejected when combined with `--resume`/
   `--continue`, per `src/state/requested-session-id.ts` in the bundle,
   implying resume never mints a fresh id; the official changelog and the
   Cursor TS SDK both frame resume as continuing the same entity/id). No
   source found contradicts it. Risk is bounded: if false, the sessionStart
   glob (below) simply finds zero matches and the hook exits 0 — identical
   to today's shipped no-op, not a regression or data-corruption risk.
2. **Per-conversation transcript lines are already shaped like the proven
   `postToolUse` payload** (`tool_name`, `tool_input`, `tool_output`,
   `cwd`/`workspace_roots`), rather than a raw per-provider API transport
   (unlike Codex's rollout, which the reference script has to unwrap). No
   live sample and no public docs describe this file's schema. This is the
   most defensible assumption available (Cursor's local hook contract is the
   only proven normalized tool-call shape it has, and a per-conversation log
   used "for citation" is a natural place to persist exactly that stream)
   but it is inferred, not confirmed. Chosen design consequence: reuse
   `cursor_extract_touched_paths` directly against each transcript line
   (no separate "unwrap" step, unlike Codex) — if the real schema differs,
   every line fails the `tool_name`/`tool_input.file_path` lookup and the
   backfill degrades to an empty manifest (today's no-op), not a crash.

Both assumptions must be stated verbatim as a code comment at the top of the
new `session-backfill.sh` (not just this plan) so a future reader / bug
report can find the residual risk without spelunking history.

**Direction check on assumption 1 (both ways, not just the safe one).** The
false-negative direction (resume doesn't reuse the id) fails safe as above.
The false-*positive* direction — a brand-new conversation's sessionStart
firing while a non-empty transcript already exists under its fresh
`conversation_id` — is not actually reachable: `sessionStart` fires before
any message is exchanged, so a genuinely new conversation has nothing to
have written yet, and `-s` (non-empty) additionally rules out a merely
pre-created-but-empty file. If it somehow did happen anyway (id reuse,
clock/filesystem weirdness), the design still recovers: `session-reset.sh`'s
defer leaves the pane's manifest/owner untouched, `session-backfill.sh`'s
unconditional `rm -f "$manifest"` + rebuild replaces whatever was there, and
`owner_selfheal` (`images.sh:59`, fired on the first real tool call) re-stamps
ownership. State this explicitly in the header comment, not just here.

**Probe consistency (`session-reset.sh` vs. `session-backfill.sh`).** Codex's
two hooks can't disagree about resume-vs-fresh: both switch on `.source`, a
field in the one payload delivered to both processes. Cursor's substitute —
"does a non-empty transcript exist for this id" — is a filesystem predicate
evaluated twice, at two different instants, by two different processes; it
can disagree with itself between the two calls. Of the four combinations
(reset's probe × backfill's probe), three converge trivially
(empty/empty → reset clears, backfill exits; found/found → reset defers,
backfill rebuilds; empty/found → backfill's authoritative rebuild supersedes
whatever reset did). The fourth — reset sees `found` and defers, backfill's
own probe comes back empty-or-unreadable a moment later — is the one that
needs a real answer, not just "rare": step 2 item 9's locked drop closes it
(see that step), so nobody has to rely solely on `owner_selfheal`'s next-
tool-call self-heal to clean up a foreign manifest in that window.

## Step 1 — add the shared resume-transcript probe to `shim.sh`

**File:** `adapters/cursor/scripts/lib/shim.sh`

Add one function (used by both scripts in steps 2 and 3):

```sh
# cursor_resume_transcript SESSION_ID -> echoes the path to this conversation's
# agent-transcript file iff exactly one exists and is non-empty, else nothing.
# The existence of a non-empty transcript IS the resume signal (Cursor's
# sessionStart payload carries no `source` field to switch on) — a brand-new
# conversation has no transcript file yet. Glob on the id instead of deriving
# <slug> from workspace_roots: the one observed slug rule (`/`->`-`) is not
# guaranteed stable, the id is.
cursor_resume_transcript() {
	local sid="$1" root matches
	[[ $sid =~ ^[A-Za-z0-9_-]+$ ]] || return 0
	root="${AEYE_CURSOR_HOME:-$HOME/.cursor}"
	matches=("$root"/projects/*/agent-transcripts/"$sid"/"$sid".jsonl)
	[[ ${#matches[@]} -eq 1 && -s ${matches[0]} ]] && printf '%s' "${matches[0]}"
	return 0
}
```

Notes:

- The `^[A-Za-z0-9_-]+$` guard is self-contained (no dependency on
  `valid_pane_file`, which lives in `manifest-lifecycle.sh` and is sourced
  *after* `shim.sh` by both callers) and blocks glob metacharacters / path
  traversal via a crafted `conversation_id`/`session_id`.
- No `nullglob` needed: an unmatched glob leaves the literal pattern string
  as the sole array element, and `-s` on that literal path is always false.
- `-s` (non-empty), not just `-f`: guards the "file created but no tool call
  logged yet" edge the task calls out explicitly.
- `implement: default` (plain, no concurrency/security-sensitive logic).

**Unit tests — `tests/cursor/extract.bats` (extend, same step).** This file
is the established home for direct `shim.sh` function tests (it already
sources the file and drives functions in isolation); a new shim function's
tests belong here, not only exercised indirectly through the two integration
suites in step 4. Add `export AEYE_CURSOR_HOME="$BATS_TEST_TMPDIR/cursorhome"`
to this file's `setup()` (it has none today) and add a
`cursor_resume_transcript` block covering, at minimum:

- exactly one match, non-empty → echoes that path
- zero matches (no `agent-transcripts/<id>/` dir at all) → empty
- the file exists but is empty (0 bytes) → empty (the `-s` guard)
- two matches (two project slugs somehow both have a dir for the same id) →
  empty, not either path (ambiguous ⇒ treat as "can't resolve")
- an id containing a glob metacharacter (`*`, `?`, `[`) or a path-traversal
  attempt (`../x`) → empty, and provably does not read outside
  `AEYE_CURSOR_HOME` (e.g. plant a file at a `..`-escaping location the
  crafted id targets and assert it's never surfaced)
- `AEYE_CURSOR_HOME` unset → falls back to `$HOME`; test this by also
  exporting `HOME="$BATS_TEST_TMPDIR/fakehome"` for that one case (never let
  a test exercise the real `$HOME/.cursor`)

**Acceptance:** function is self-contained, pure (no manifest writes), matches
the file's existing style/doc-comment conventions; the above cases pass in
`tests/cursor/extract.bats`.

## Step 2 — rewrite `session-backfill.sh`

**File:** `adapters/cursor/scripts/session-backfill.sh` (replace entirely)

Structure (mirrors the Codex reference where the shape actually transfers;
diverges where the assumption above collapses the "unwrap raw transport"
step down to direct reuse of `cursor_extract_touched_paths`):

1. Header comment: what this hook does, and the two numbered assumptions
   above (including the direction-check paragraph), verbatim enough to be
   greppable.
2. `set -euo pipefail`; read payload from stdin; empty → exit 0.
3. `PLUGIN_ROOT="${PLUGIN_ROOT:-$(dirname "${BASH_SOURCE[0]}")/..}"` — same
   line Codex's/Cursor's other hooks use, needed by the two `source`s below
   and by the test harness's `export PLUGIN_ROOT`.
4. `source "$PLUGIN_ROOT/scripts/lib/shim.sh"` then
   `source "$PLUGIN_ROOT/scripts/core/manifest-lifecycle.sh"` (matches
   `images.sh` / `session-reset.sh` sourcing order — shim before lifecycle;
   each with its `# shellcheck source=... disable=SC1091` comment).
5. `resolve_state_dirs`.
6. `session="$(cursor_session_id "$payload")"` (no early exit here — matches
   Codex, which doesn't gate on session emptiness either; see item 7).
7. `pane_file="$(resolve_pane_key "$session")"`; empty → exit 0 — the single
   "no key at all" guard (matches Codex's `session-backfill.sh` exactly: no
   separate session-emptiness check exists there, because `resolve_pane_key`
   already folds an empty session into an empty `pane_file` when there's
   also no `TMUX_PANE`). `valid_pane_file "$pane_file"` → exit 0 if invalid.
8. `manifest_paths "$pane_file"`; bridge the uppercase globals to lowercase
   locals exactly like both reference scripts do, each with its
   `# shellcheck disable=SC2153` comment:
   ```sh
   manifest="$MANIFEST"
   owner_file="$OWNER_FILE"
   mkdir -p "$IMAGES_DIR"
   ```
9. `transcript="$(cursor_resume_transcript "$session")"`. Unlike Codex —
   which reaches its analogous "no transcript" branch only past a `.source
   == resume` gate, meaning `session-reset.sh` has *already deferred* by the
   time Codex's backfill runs — Cursor's `session-backfill.sh` runs on
   *every* sessionStart, so an empty `transcript` here is the ordinary
   new-conversation path, running **concurrently** with `session-reset.sh`'s
   clear+stamp (step 3) and possibly a live `images.sh` append. Naively
   dropping a foreign manifest here (Codex's pattern) would race those
   writers — *unless it happens under the same lock they also take*, since
   `manifest_paths` sets `LOCK_FILE` to the identical path `session-reset.sh`
   and `images.sh` lock (`adapters/core/manifest-lifecycle.sh:59` vs.
   `session-reset.sh:37` / `images.sh:53`), which makes a locked
   read-owner-then-drop atomic against both — whichever writer runs first,
   the other observes a consistent, already-resolved state. So still take
   the lock and still drop a manifest this session can't prove it owns, on
   *both* the empty-transcript and unreadable-transcript paths — the branch
   below only has to notice *when* there's nothing to compare (fold `-z` and
   `! -r` into one check):
   ```sh
   _manifest_lock "$LOCK_FILE"

   # No transcript (fresh session) or an unreadable one (permission bits, or
   # removed since the glob) both need the same treatment: drop a manifest
   # this session can't prove it owns, keep one it does. Locked against
   # session-reset.sh/images.sh's same-path lock, so whichever of us or
   # session-reset.sh runs first, the other converges onto a consistent
   # result — see the "probe consistency" note above (this closes the
   # found/empty race cell: without the lock+drop here, a resume the reset
   # hook saw but this probe didn't would leave a foreign manifest behind
   # until the first live tool call's owner_selfheal cleans it up).
   if [[ -z $transcript || ! -r $transcript ]]; then
       owner=""
       [[ -f $owner_file ]] && owner="$(<"$owner_file")"
       [[ -f $manifest && (-z $owner || $owner != "$session") ]] && rm -f "$manifest" "$owner_file"
       exit 0
   fi
   ```
10. Only past that exit do we know a resume was detected and this script is
    the pane's sole writer for the rest of the run.
11. Authoritative rebuild: `rm -f "$manifest"`; `declare -A seen=()`.
12. `append_image` / `append_diagram` closures — copy verbatim from the
    Codex script (same dedup + `is_d2_render_artifact` skip semantics; note
    the argument-order swap in the `append_diagram` wrapper —
    `append_diagram_line` takes `NAME` before `TS`, so the wrapper calls it
    `"$manifest" "$1" "$2" "$4" "$3"` — carry that swap over unchanged).
13. Parse loop:
    ```sh
    while IFS= read -r line; do
        [[ -n $line ]] || continue
        ts="$(jq -r '.timestamp // empty' <<<"$line" 2>/dev/null)" || ts=""
        tool_name="$(jq -r '.tool_name // "?"' <<<"$line" 2>/dev/null)" || tool_name="?"
        while IFS= read -r p; do
            [[ -n $p ]] || continue
            if [[ ${p,,} == *.d2 ]]; then
                png="$(d2_render "$p" "$DIAGRAMS_DIR")" || continue
                append_diagram "$png" "${png%.png}.svg" "$ts" "$(basename "$p" .d2)"
            else
                append_image "$p" "$tool_name" "$ts"
            fi
        done < <(cursor_extract_touched_paths "$line")
    done < <(grep -E '\.(png|jpe?g|gif|webp|bmp|d2)' "$transcript")
    ```
    The `grep` fast-bail before `jq`/the extractor mirrors `images.sh` and
    the Claude backfill; a truncated/non-JSON final line yields empty from
    every `jq` call under `2>/dev/null` and simply produces no paths (no
    `set -e` abort, matching the Codex/Claude tests for this exact case).
14. Claim: `[[ -f $manifest && -n $session ]] && printf '%s' "$session" >"$owner_file"`.

**Acceptance:** `shellcheck adapters/cursor/scripts/session-backfill.sh` clean;
behavior matches the bats file in step 4.

`implement: default`.

## Step 3 — teach `session-reset.sh` to defer on a detected resume

**File:** `adapters/cursor/scripts/session-reset.sh`

- Update the header comment: replace "Cursor has no source field... always
  apply startup semantics" with the Codex-style framing — a detected resume
  defers to `session-backfill.sh`, which is the sole writer of a resumed
  pane's manifest (SessionStart hooks run in parallel, no ordered turn).
- In the "This pane's manifest" block, branch on
  `cursor_resume_transcript "$session"` before doing anything:
  ```sh
  if [[ -n $pane_file ]] && valid_pane_file "$pane_file"; then
      if [[ -z $(cursor_resume_transcript "$session") ]]; then
          _manifest_lock "$IMAGES_DIR/$pane_file.lock"
          owner_file="$IMAGES_DIR/$pane_file.owner"
          clear_pane "$pane_file"
          [[ -n $session ]] && printf '%s' "$session" >"$owner_file"
      fi
      # else: resume detected — session-backfill.sh owns this manifest.
  fi
  ```
  Deliberate divergence from Codex's `session-reset.sh` (which takes
  `_manifest_lock` unconditionally, before its `case`): here the lock moves
  *inside* the non-resume branch because the resume branch does nothing at
  all — nothing to serialize against when the block's only content is a
  no-op comment. Say this explicitly in an inline comment above the `if`, so
  a reviewer comparing the two files against the "mirrors Codex" framing
  doesn't read it as an oversight.
- `shim.sh` is already sourced here, so no new import.
- GC section is untouched — it doesn't touch the current pane's manifest.
- **README deliverable (moved in from "Out of scope" — see note there):**
  update `adapters/cursor/README.md:55-56`, the "Resume backfill deferred"
  bullet under Limitations, to describe the shipped behavior instead of the
  no-op it currently documents. Also fold in the `AEYE_CURSOR_HOME` runtime
  caveat: today that variable is read only by `install.sh` at install time;
  after this change it is also read live by the `session-backfill.sh` /
  `session-reset.sh` hook processes `cursor-agent` spawns. A user who
  installed to a custom `AEYE_CURSOR_HOME` and doesn't also export it in
  their shell environment will silently fall back to `$HOME/.cursor` at hook
  time (the glob just finds nothing and both hooks behave as if this were
  a fresh session — no error, no backfill). Add one sentence to the existing
  `AEYE_CURSOR_HOME` paragraph (README.md:34-35) noting it must be exported
  in the environment `cursor-agent` runs in, not just present at install
  time, for resume backfill to find the right `projects/` tree.

**Acceptance:** `shellcheck` clean; existing session-reset tests whose payload
has no matching transcript are unaffected (they still hit the clear+stamp
branch — glob finds nothing under the test's `AEYE_CURSOR_HOME`).

`implement: default`.

## Step 4 — tests

**Files:**
- `tests/fixtures/cursor/transcript-basic.jsonl` (new)
- `tests/cursor/backfill.bats` (new)
- `tests/cursor/session-reset.bats` (extend)

### Fixture

One JSONL file, each line a `postToolUse`-shaped record per the schema
assumption in step 2, using `WORKDIR`/`PNGPATH`/`SHOTPATH`/`D2ARTPATH`
placeholder tokens substituted the same way `tests/codex/backfill.bats` does
(`sed -e "s#TOKEN#$value#g" ...`). Cover, in this order:

1. a `Write` of a `.d2` file (`$WORKDIR/diagram.d2`) → rendered diagram
2. a `Read` of a real `.png` (`PNGPATH` → `$WORKDIR/sample.png`) → image,
   `source` = `Read`
3. a `Shell` call whose JSON-string `tool_output` embeds a screenshot path
   (`SHOTPATH` → `$WORKDIR/shot2.png`) under the workspace root → image,
   `source` = `Shell`
4. a `Read` of `D2ARTPATH` — a path under the diagrams dir shaped like a
   *generated* d2 theme variant (`<16-hex>-light.png`) — must NOT appear as
   a second plain image (`is_d2_render_artifact` skip). Unlike Codex's test
   (which creates this file inline inside one `@test` and re-seds the
   fixture just for that test), here it's a permanent fixture line: the test
   harness's `setup()` creates the `D2ARTPATH` file (under
   `$AEYE_DIR/images/diagrams/0123456789abcdef-light.png`) unconditionally,
   before ever building the transcript, so `cursor_extract_touched_paths`'s
   `[[ -f $q ]]` existence check passes and the skip logic actually gets
   exercised (a nonexistent path would make this test pass vacuously).
5. a line with an unrelated/unknown `tool_name` (e.g. `"Terminal"`) → no-op
6. a final truncated/non-JSON line → does not abort the backfill

All lines carry a distinct `timestamp` field to test chronological `ts`
passthrough. Expected total manifest lines after a full backfill run:
**3** (one diagram entry from line 1, one image entry each from lines 2 and
3; lines 4-6 contribute nothing).

**`tool_output` convention — follow `tests/cursor/images.bats:19-44` exactly,
don't invent a shape.** `tool_output` is always a JSON-encoded *string*, on
every line, including `Write`/`Read`. For the `Write`/`Read` lines (1, 2, 4),
keep `tool_output` a harmless string with no path in it (e.g. `"{\"success\":true}"`)
— `scan_response_image_path` also scans `tool_output` regardless of
`tool_name`, so a path-bearing `tool_output` on a Write/Read line would
double-emit the same file. For the `Shell` line (3), build `tool_output` the
way `shell_payload()` does: an inner JSON object `{output:"saved to
<path>\n",exitCode:0}`, itself embedded as a JSON string value (`jq
--arg to "$inner_json"`), so the extractor's `fromjson?` unwrap has something
real to unwrap.

### `tests/cursor/backfill.bats`

Mirror `tests/codex/backfill.bats`'s structure and naming exactly, adapted:

- `PLUGIN_ROOT="$ROOT/adapters/cursor"` (no `plugin/` path segment, unlike
  Codex/Claude-code).
- `export AEYE_CURSOR_HOME="$BATS_TEST_TMPDIR/cursorhome"` in `setup()` —
  hermetic; never touches the developer's real `~/.cursor`.
- Build the fixture transcript at
  `"$AEYE_CURSOR_HOME/projects/some-slug/agent-transcripts/$CONV_ID/$CONV_ID.jsonl"`
  with a fixed `CONV_ID="fixture-conv-1"`.
- Stub `aeye render-diagram` exactly like the Codex test.
- `run_app() { jq -nc --arg c "$CONV_ID" '{conversation_id:$c,session_id:$c}' | bash "$APP"; }`
  — no `source`/`transcript_path` fields; the resume trigger is purely
  "does a transcript exist for this id", which the fixture setup already
  guarantees.

Test cases (one bats `@test` each):

1. backfills the `.d2` `Write` as a rendered diagram
2. backfills the `Read` png path with `source="Read"`
3. backfills the `Shell` screenshot path with `source="Shell"`
4. does not backfill the generated d2 theme-variant path as a plain image
   (`D2ARTPATH`, created in `setup()` per the fixture note above)
5. produces exactly **3** expected manifest lines (`wc -l`), plus explicit
   `grep -c` zero-checks that neither the decoy `tool_name` line's marker nor
   the truncated line's partial content appear anywhere in the manifest —
   mirrors `tests/codex/backfill.bats:85-97`
6. a truncated/non-JSON final line does not abort the backfill (`status -eq 0`)
7. `ts` on a manifest line matches the transcript record's `timestamp`
8. claims the manifest via the owner sidecar (`cursor_session_id` value)
9. dedup against a pre-seeded manifest entry → no double entry
10. drops a foreign entry not in the transcript (reused-pane bleed, pre-seed
    manifest + mismatched owner file)
11. **zero-match glob** (no transcript directory at all for this conversation
    id under `AEYE_CURSOR_HOME`) → clean exit 0, no manifest created — this
    is the Cursor-specific replacement for Codex's "non-resume source" /
    "missing transcript_path" tests, since Cursor's trigger is structurally
    different (existence, not a `source` field).
12. **empty payload** (empty stdin) → clean exit 0, **and a pre-seeded
    manifest is left byte-for-byte unchanged** (pre-seed it in this test the
    way `tests/cursor/session-reset.bats:29` does in `setup()` — asserting
    only "no manifest was created" is vacuous here since nothing in this
    test would have created one anyway; asserting an existing one survives
    untouched actually proves the empty-payload guard fired).
13. **no key at all** — `unset TMUX_PANE` and a payload with no
    `conversation_id`/`session_id` → clean exit 0, no crash. This exercises
    item 7's `[[ -n $pane_file ]] || exit 0` (the single "no key" guard,
    post-merge with item 6 above) — not a separate session-emptiness check,
    since there isn't one.
14. **outside tmux, session-id-keyed path** — `unset TMUX_PANE`, run with a
    real-shaped `CONV_ID`, and assert the manifest lands at
    `$AEYE_DIR/images/$CONV_ID.jsonl` (via `resolve_pane_key`'s fallback,
    `adapters/core/manifest-lifecycle.sh:50`) rather than the tmux-pane-keyed
    path — mirrors `tests/cursor/session-reset.bats:65-72`'s analogue.

### `tests/cursor/session-reset.bats` (extend)

- Add `export AEYE_CURSOR_HOME="$BATS_TEST_TMPDIR/cursorhome"` to `setup()`
  so no existing test can coincidentally observe the real machine's
  `~/.cursor/projects` tree.
- Add one new `@test`: seed a non-empty transcript file for a given
  conversation id under `$AEYE_CURSOR_HOME`, run the hook with that id as
  `conversation_id`, and assert the manifest is **not** cleared (defers to
  backfill) — the mirror image of the existing "sessionStart removes the
  manifest" test.
- Update the comment on the existing "GC sweeps manifests for tmux panes
  that no longer exist" test (currently: "Current pane always cleared
  (startup semantics; no resume branch)") to say "no resume detected for
  this conversation id" instead of "no resume branch" (the branch now
  exists; this test's payload just doesn't match a transcript).

**Acceptance:** `bats tests/cursor/backfill.bats tests/cursor/session-reset.bats`
green.

`implement: default` for all of step 4.

## Step 5 — shellcheck + full cursor bats suite

Run `shellcheck` on every `.sh` file touched (`shim.sh`, `session-backfill.sh`,
`session-reset.sh`) and fix all findings. Run the full `tests/cursor/` bats
suite (not just the new/changed files) to catch any regression from the
`AEYE_CURSOR_HOME` addition or the shared `shim.sh` change.

`implement: default`.

## Out of scope

- No changes to `adapters/core/*` — nothing shared needed changing.
- No changes to Claude/Codex adapters.

(The README update is in-scope — see the "README deliverable" bullet under
step 3, not here.)
