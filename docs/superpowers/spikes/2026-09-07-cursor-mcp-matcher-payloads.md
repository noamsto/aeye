# Spike: Cursor MCP tool-matcher payloads for screenshot capture (aeye #172)

Date: 2026-09-07 (Q3/Q4/Q5); updated 2026-09-08 (Q1/Q2)
Version tested: `cursor-agent` / Cursor `2026.09.02-c22c1a3` (both dates — same build)

## GATE: PASS — Q1-Q5 all answered with real evidence

**2026-09-08 update: Q1 and Q2 are now answered on a work-profile host with an
authenticated `cursor-agent` session** (`cursor-agent status` →
`✓ Logged in as noam@factify.com`). See "Q1/Q2 evidence (2026-09-08, resolved)"
below for the verbatim payloads. Short answer: the issue's exact literal
`MCP:*` **does** fire for every MCP tool call, so a single wildcard-shaped
`postToolUse` entry is sufficient — no per-tool matcher list needed. `tool_name`
for an MCP call is spelled `MCP:<bare_tool_name>` (e.g. `MCP:browser_navigate`,
`MCP:browser_take_screenshot`) — neither the fully server-qualified nor the
fully bare form the issue anticipated, but a third shape: a literal `MCP:`
prefix on the bare tool name. See the "Recap" section at the bottom for how
this changes the follow-up call.

The paragraph below is the original 2026-09-07 record of why Q1/Q2 were
blocked on that day's host; it is kept for history and is superseded by the
2026-09-08 update above — do not read it as still current.

This host has no authenticated `cursor-agent` session (personal-profile
machine; Cursor CLI auth is work-profile-only here) and none of the standard
credential locations hold a token — see the "Blocked" section below and the
Method log. `cursor-agent login` requires an interactive browser flow, which
is both unavailable and out of scope for an unattended probe session, and was
correctly refused as an account-affecting action. **Do not read this as "MCP
hooks don't work on Cursor" — it is purely "this host can't drive
`cursor-agent`."** Q1 (does an `MCP:*`-shaped matcher fire) and Q2 (exact
`tool_name` spelling for an MCP call) need a live, authenticated
`cursor-agent` run and remain open; a precise 10-minute recipe to finish them
on a suitable host is below.

Q3, Q4 (the non-Cursor-specific half), and Q5 do **not** require `cursor-agent`
at all — they were answered directly against the playwright-mcp server over
its own MCP stdio JSON-RPC protocol (bypassing `cursor-agent` entirely) and by
running the repo's real `scan_response_image_path` / `cursor_extract_touched_paths`
functions against synthetic-but-realistic payloads built from that server's
actual responses. This surfaced a genuine regex bug in `scan_response_image_path`
(see "Field notes for the adapter") — **filed and fixed separately as #213**
(closed) since this doc was first written; not touched by this probe. Payloads
built by wrapping a real MCP response as a Cursor-shaped `tool_output` string
are marked **SYNTHETIC** below — the response content itself is real and
observed; only the Cursor envelope around it (and the `tool_name` value) was
an assumption at the time, pending Q2. **2026-09-08 update: Q2 is now answered
(see below) — the assumed `tool_name` value used in these synthetic payloads,
`mcp_playwright_browser_take_screenshot`, turned out to be wrong.** The real
form is `MCP:browser_take_screenshot`. This does not change the Payload A/B/C
*path-capture* conclusions (the regex bug reproduces on the path string
regardless of `tool_name`), but it does change the `cursor_extract_touched_paths`
branch call for Payload C: that function's `tool_input`-reading branch only
fires when `tool_name` is exactly `Read` or `Write` (see "Field notes for the
adapter" below) — `MCP:browser_take_screenshot` still isn't `Read`/`Write`, so
Payload C's conclusion (succeeds via the response-scanner half, not the
`tool_input` branch) still holds with the corrected `tool_name`.

## Verbatim evidence

### `cursor-agent` auth blocker

**2026-09-08: resolved on a work-profile host — see "Q1/Q2 evidence
(2026-09-08, resolved)" below.** This subsection is the original 2026-09-07
record of that day's host being unauthenticated; kept as-is for history.

```
$ cursor-agent status
Not logged in

$ cursor-agent -p -f "Call the context7 tool resolve-library-id with libraryName set to react. …"
Error: Authentication required. Please run 'agent login' first, or set CURSOR_API_KEY environment variable.
```

`cursor-agent mcp list` (no auth required) confirms all three user-scoped
servers were visible from the scratch dir, ruling out "server never loaded"
as an alternative explanation:

```
$ cursor-agent mcp list
context7: ready
firefox-devtools: ready
playwright: ready
```

### Direct MCP protocol: server's own advertised tool name

`tools/list` against the headless `playwright-mcp` binary (spawned directly,
no Cursor involved) returns the bare tool name, matching the issue's "bare"
option for the underlying server identity — Cursor's own prefixing convention
on top of this remains unverified:

```json
{"name":"browser_take_screenshot","description":"Take a screenshot of the current page. …","inputSchema":{"type":"object","properties":{"filename":{"description":"File name to save the screenshot to. Defaults to `page-{timestamp}.{png|jpeg|webp}` if not specified. Prefer relative file names to stay within the output directory.","type":"string"}, …},"required":["scale"]},"serverInfo":{"name":"Playwright","version":"1.63.0-alpha-2026-08-31"}}
```

### No `filename`, `--output-dir` set explicitly

Server cwd: `/tmp/aeye-mcp-probe` (parent of the scratch workspace).
Command: `playwright-mcp --headless --executable-path <chromium> --output-dir /tmp/aeye-mcp-probe/work/pw-output`.

```json
{
  "result": {
    "content": [
      {"type": "text", "text": "### Result\n- [Screenshot of viewport](work/pw-output/page-2026-09-07T05-53-54-492Z.png)\n### Ran Playwright code\n```js\n// Screenshot viewport and save it as work/pw-output/page-2026-09-07T05-53-54-492Z.png\nawait page.screenshot({\n  path: 'work/pw-output/page-2026-09-07T05-53-54-492Z.png',\n  scale: 'css',\n  type: 'png'\n});\n```"},
      {"type": "image", "data": "<6332 bytes elided, base64>", "mimeType": "image/png"}
    ]
  },
  "id": 4
}
```

Confirms the given fact: with no `filename`, the server **both** writes a
file to disk (`find` located it at
`/tmp/aeye-mcp-probe/work/pw-output/page-2026-09-07T05-53-54-492Z.png`,
exactly as the text block says, relative to the server's own process cwd —
not the workspace root I ran the client from) **and** embeds base64 in the
same response (`tool_output`, in Cursor's case). If a scanner walks
`tool_output` looking for the first path-looking string, the `text` block
appears before the `image` block in `content[]`, so the path string is found
first — the base64 blob would never even need to be reached to answer "is
there a path."

### No `filename`, no `--output-dir` (server default)

Same call, no `--output-dir` flag, server cwd =
`/tmp/aeye-mcp-probe/work` (the scratch workspace root, standing in for a
real `workspace_roots[0]`):

```json
{
  "result": {
    "content": [
      {"type": "text", "text": "### Result\n- [Screenshot of viewport](.playwright-mcp/page-2026-09-07T05-55-22-156Z.png)\n### Ran Playwright code\n```js\n// Screenshot viewport and save it as .playwright-mcp/page-2026-09-07T05-55-22-156Z.png\nawait page.screenshot({\n  path: '.playwright-mcp/page-2026-09-07T05-55-22-156Z.png',\n  scale: 'css',\n  type: 'png'\n});\n```"},
      {"type": "image", "data": "<elided, base64>", "mimeType": "image/png"}
    ]
  },
  "id": 4
}
```

File landed at `/tmp/aeye-mcp-probe/work/.playwright-mcp/page-<ts>.png` —
confirms the upstream default (`<server cwd>/.playwright-mcp/`) documented in
the given facts, empirically, with `workspace_roots[0]`-equivalent cwd. Note
the path text is `**.playwright-mcp/page-…**` — no leading `/` or `./`.

### Explicit `filename` (absolute path under the scratch workspace)

Call: `browser_take_screenshot({scale:"css", filename:"/tmp/aeye-mcp-probe/work/explicit-shot.png"})`.

```json
{
  "result": {
    "content": [
      {"type": "text", "text": "### Result\n- [Screenshot of viewport](./explicit-shot.png)\n### Ran Playwright code\n```js\n// Screenshot viewport and save it as ./explicit-shot.png\nawait page.screenshot({\n  path: './explicit-shot.png',\n  scale: 'css',\n  type: 'png'\n});\n```"}
    ]
  },
  "id": 4
}
```

Two things change versus the no-`filename` cases: (1) **no `image` content
block at all** — giving an explicit `filename` fully suppresses the inline
base64, so `tool_output` in this case would contain only text, no image data
to elide; (2) the file lands exactly at the absolute path given
(`/tmp/aeye-mcp-probe/work/explicit-shot.png`, confirmed on disk), and the
confirmation text relativizes it against cwd with an explicit **`./`**
prefix — unlike the two no-`filename` cases above, which had no `./` prefix.
This difference in prefix is the crux of the regex finding below.

### SYNTHETIC Cursor-envelope replays through the real scanner functions

Built by wrapping the MCP `content[]` JSON above as the JSON-*encoded string*
`tool_output`, per the confirmed Cursor convention from the 2026-07-28 spike
(`tool_output` is a string requiring `fromjson`), with `tool_name` set to a
plausible (unconfirmed) `mcp_playwright_browser_take_screenshot`:

**Payload A** — output-dir run, `cwd="/tmp/aeye-mcp-probe"` (text path
`work/pw-output/page-…png`, no leading `/` or `./`):

```
$ jq (core scan_response_image_path's own capture expression) on tool_output
-> "/pw-output/page-2026-09-07T05-53-54-492Z.png"     # truncated! "work/" prefix silently dropped
$ scan_response_image_path "$payload_a"
-> (empty)   # resolve() sees a leading "/" so does NOT prepend cwd; result isn't under cwd; file doesn't exist there either
```

**Payload B** — default `.playwright-mcp` run, `cwd="/tmp/aeye-mcp-probe/work"`
(text path `.playwright-mcp/page-…png`, no leading `./`):

```
$ jq capture expression
-> "/page-2026-09-07T05-55-22-156Z.png"    # truncated! ".playwright-mcp" prefix dropped, leaving a bogus root-relative path
$ scan_response_image_path "$payload_b"
-> (empty)   # same failure mode as Payload A
```

**Payload C** — explicit-`filename` run, `cwd="/tmp/aeye-mcp-probe/work"`,
`workspace_roots=["/tmp/aeye-mcp-probe/work"]`, `tool_input.filename` set to
the absolute path, `tool_output` text path `./explicit-shot.png`:

```
$ jq capture expression
-> "./explicit-shot.png"    # captured whole, because it starts exactly with "./"
$ scan_response_image_path "$payload_c"
-> "/tmp/aeye-mcp-probe/work/./explicit-shot.png"   # resolves under cwd, passes containment, file exists -> SUCCEEDS
$ cursor_extract_touched_paths "$payload_c"   (tool_name = mcp_playwright_…, so the Read|Write tool_input branch is skipped)
-> "/tmp/aeye-mcp-probe/work/./explicit-shot.png"   # still succeeds, purely via the response-scanner half
```

## Blocked (2026-09-07) — resolved 2026-09-08, see below

**Resolved 2026-09-08 — both halves of the static analysis below were
confirmed empirically; jump to "Q1/Q2 evidence (2026-09-08, resolved)" for
the verbatim payloads.** This whole section is the original 2026-09-07
"unresolved on this host" record and the static regex analysis written before
any live run; it is superseded, kept for history.

**Q1 (does an `MCP:*`-shaped matcher fire?) and Q2 (exact `tool_name` spelling
for an MCP call) are unresolved on this host.** Nothing fired end-to-end for
any matcher shape — the `control.jsonl` positive control (`Read|Write|Shell`)
stayed 0 bytes right alongside `mcp-colon-dotstar.jsonl` (`MCP:.*`),
`mcp-literal-wildcard.jsonl` (`MCP:*`), `catch-all.jsonl` (`.*`), and
`no-matcher.jsonl` — because no `cursor-agent` invocation ever got past the
authentication error to execute a single tool call. This is a
harness-availability blocker, not a matcher result: it says nothing about
whether `MCP:*` or `MCP:.*` actually match a real `postToolUse` event, only
that this run couldn't observe one either way. Do not treat the empty control
log as evidence about Cursor hooks in general — the 2026-07-28 spike already
proved `Read|Write|Shell` fires reliably on an authenticated host; it is only
*this host's* auth that's missing.

Static regex analysis only (not empirically tested here): Cursor's docs
describe `postToolUse` matchers as regex. Read literally, `MCP:*` means "the
string `MCP` followed by zero or more `:`" — it would not match a
`tool_name` like `mcp_playwright_browser_take_screenshot` or
`mcp__playwright__browser_take_screenshot` at all, with or without a colon.
`MCP:.*` (colon-dot-star) is the form that would actually match a
colon-delimited `MCP:<tool_name>` tool name if Cursor produces one. Whether
Cursor's real `tool_name` for an MCP call contains a colon at all (vs.
underscores, as `mcp_<server>_<tool>`) is exactly Q2 and is unresolved here.

**2026-09-08: this static analysis turned out to be exactly right about the
mechanism, and the empirical result confirms it** — Cursor's real `tool_name`
for an MCP call is `MCP:<bare_tool_name>` (colon-delimited, matching the
"if Cursor produces one" hedge above), which is why `MCP:.*` fires. `MCP:*`
*also* fires empirically, but for a subtler reason than "the docs are wrong
about it being regex": an unanchored regex search for "`MCP` followed by
zero-or-more literal `:`" still finds a match *within* `MCP:browser_navigate`
at the substring `MCP:` itself (one colon satisfies "zero or more") — it
doesn't need to match the whole `tool_name`. So the issue's exact literal
works, but as an accident of unanchored substring matching against a
`tool_name` that happens to start with `MCP:`, not because `*` means "any
tool" the way the issue's shorthand implies.

Cross-adapter comparison (context, not a Cursor result): the Codex spike
(`2026-07-12-codex-hook-contract.md`) proposes `mcp__*` in its "Consequences"
prose as the matcher a future Codex `hooks.json` should use, but that form
was never actually checked into `adapters/codex/plugin/hooks/hooks.json`
(which has only ever matched `apply_patch|view_image|Bash`) and the spike's
own proven-payload table has no MCP row — by that spike's own account,
`mcp__*` is a **recommendation that was never implemented or exercised**,
not an observed live matcher. Cite it as a cross-adapter precedent for the
*shape* of an MCP matcher, not as proof any particular syntax fires or ships.
**2026-09-08: Cursor's own `MCP:<tool>` form is unrelated to Codex's proposed
`mcp__<tool>` form** — different delimiter, different case convention, and
Cursor's is now confirmed live while Codex's remains unimplemented.

### Recipe used to finish Q1/Q2 (executed 2026-09-08)

This is the original 2026-09-07 recipe, left as written (imperative, "you")
since it was followed almost exactly as specified. **Its own closing
instruction in step 8 — "append the verbatim results to this doc … not an
edit to the paragraphs above" — is superseded here**: aeye #172's actual
deliverable requires updating this doc in place (flip GATE, reconcile every
section that asserted "unresolved"), which is what this 2026-09-08 pass does
instead. See "Q1/Q2 evidence (2026-09-08, resolved)" below for what was
actually observed, and the Method log for the exact commands run (which
deviated from this recipe in a few small ways, noted there: real script
files instead of inline `sh -c`, a navigate-first prompt, and a
`playwright: ready` check via `cursor-agent mcp list` before trusting the run).

Prerequisite: `cursor-agent status` reports a logged-in account (run
`cursor-agent login` first if not — that step needs a human at a browser, do
it once outside this recipe).

1. `mkdir -p /tmp/aeye-mcp-probe/work && cd /tmp/aeye-mcp-probe/work`
2. Back up the real hooks file: `cp ~/.cursor/hooks.json ~/.cursor/hooks.json.probe-bak`
3. Merge probe entries into `~/.cursor/hooks.json` with `jq` (don't hand-edit —
   preserve the existing `images.sh`/`diagrams.sh`/`session-*` entries
   already there). Add these `postToolUse` entries (mirror each on
   `postToolUseFailure` too — a failed MCP call routes there instead, per the
   2026-07-28 spike), each command being `sh -c 'cat >> /tmp/aeye-mcp-probe/logs/<name>.jsonl'`:
   - `"matcher": "Read|Write|Shell"` → `control.jsonl` (positive control —
     if this stays empty after step 5, the harness itself is broken, stop
     and fix before trusting anything else)
   - `"matcher": "MCP:.*"` → `mcp-colon-dotstar.jsonl`
   - `"matcher": "MCP:*"` → `mcp-literal-wildcard.jsonl` (the issue's exact
     literal, malformed-looking regex and all)
   - `"matcher": ".*"` → `catch-all.jsonl` (fallback net — guaranteed to
     capture the real `tool_name` and full payload regardless of whether the
     other three shapes match)
   - no `matcher` key at all → `no-matcher.jsonl`
4. `mkdir -p /tmp/aeye-mcp-probe/logs`
5. `cursor-agent -p -f "Read the file /etc/hostname"` — exercises the
   `control.jsonl` positive control. Confirm it's non-empty before proceeding.
6. `cursor-agent -p -f "Call the playwright browser_take_screenshot tool on about:blank with no filename argument"`
7. Inspect every log under `/tmp/aeye-mcp-probe/logs/`. Whichever of
   `mcp-colon-dotstar.jsonl` / `mcp-literal-wildcard.jsonl` / `catch-all.jsonl`
   is non-empty answers Q1 directly; `jq -r '.tool_name' <that file>`
   answers Q2 directly. If only `catch-all.jsonl` has content, neither
   `MCP:.*` nor `MCP:*` matched — record the real `tool_name` from it and
   try further candidate matcher strings against that value.
8. Restore `~/.cursor/hooks.json` from the `.probe-bak`, `gtrash put` the
   scratch dir, and append the verbatim results to this doc. **Superseded
   2026-09-08, see above** — the actual 2026-09-08 pass updates this doc in
   place instead of appending a separate section, per aeye #172's deliverable.

### Q1/Q2 evidence (2026-09-08, resolved)

Same host build (`cursor-agent` / Cursor `2026.09.02-c22c1a3`), now
authenticated (`cursor-agent status` → `✓ Logged in as noam@factify.com`).
Ran the recipe above with three deviations (see Method log): probe hooks used
real executable script files (`/tmp/aeye-mcp-probe/scripts/<name>.sh`, each
`cat >> .../logs/<name>.jsonl`) rather than an inline `sh -c '...'` string —
the latter's shell-interpretation by Cursor was never actually verified in
the 2026-07-28 or 2026-09-07 spikes, and this repo's own
`adapters/cursor/hooks.json` only ever uses bare script paths; the screenshot
prompt navigated to `about:blank` first, since `browser_take_screenshot`'s
schema requires `scale` and a bare "no filename" call alone under-specifies
the action; and `cursor-agent mcp list` confirmed `playwright: ready` before
trusting the run at all.

**Positive control** (`cursor-agent -p -f "Read the file /etc/hostname"`) —
`control.jsonl` got two entries attributable to this exact run by
`conversation_id`:

```json
{"tool_name":"Read","hook_event_name":"postToolUse","tool_input":{"file_path":"/etc/hostname"}}
{"tool_name":"Shell","hook_event_name":"postToolUse","tool_input":{"command":"echo \"DISPLAY=$DISPLAY\"; echo \"WAYLAND=$WAYLAND_DISPLAY\"; which Xvfb xvfb-run chromium 2>/dev/null; ls /tmp/.X11-unix 2>/dev/null; echo \"---\"; env | grep -iE 'display|playwright|headless' || true","cwd":"","timeout":30000}}
```

(The agent used both a `Read` and a follow-up `Shell` diagnostic call to
satisfy the one-line prompt — harness behavior, not something this probe
asked for.) Harness confirmed alive before trusting anything below.

**Note on `control.jsonl` cross-contamination**: because `~/.cursor/hooks.json`
is a single global per-user file (not scoped to a repo, cwd, or session),
`control.jsonl` also picked up several `Read`/`Shell` events from a *different*,
concurrently-running `cursor-agent` session on this same machine (a different
`conversation_id`, working in an unrelated worktree) — those extra entries are
not reproduced here as they belong to unrelated, in-flight work. **Field note
for anyone repeating this recipe**: expect and filter for this by
`conversation_id`/`session_id` if another `cursor-agent` session might be
running concurrently; it does not affect the MCP-specific logs below, which
only ever contained this run's own events.

**MCP screenshot call** (`cursor-agent -p -f "Navigate to about:blank using
the playwright browser, then call the playwright browser_take_screenshot
tool with no filename argument"`) — this host has no X server, so the
Playwright browser launch itself failed (`Missing X server or $DISPLAY`);
irrelevant to Q1/Q2, which only need the tool call to have been attempted and
observed by the hook system, not to have succeeded. **Both**
`mcp-colon-dotstar.jsonl` (`MCP:.*`) **and** `mcp-literal-wildcard.jsonl`
(`MCP:*`) captured all three MCP tool-call events, byte-for-byte identical
between the two files:

```json
{"tool_name":"MCP:browser_navigate","hook_event_name":"postToolUse","tool_input":{"url":"about:blank"},"cursor_version":"2026.09.02-c22c1a3","workspace_roots":["/tmp/aeye-mcp-probe/work"],"transcript_path":"/home/noams/.cursor/projects/tmp-aeye-mcp-probe-work/agent-transcripts/053710b6-de16-42a9-9365-131bc73d7a3c/053710b6-de16-42a9-9365-131bc73d7a3c.jsonl"}
{"tool_name":"MCP:browser_navigate","hook_event_name":"postToolUse","tool_input":{"url":"about:blank"}, "...": "(retried once)"}
{"tool_name":"MCP:browser_take_screenshot","hook_event_name":"postToolUse","tool_input":{"scale":"css"},"cursor_version":"2026.09.02-c22c1a3","workspace_roots":["/tmp/aeye-mcp-probe/work"],"transcript_path":"/home/noams/.cursor/projects/tmp-aeye-mcp-probe-work/agent-transcripts/053710b6-de16-42a9-9365-131bc73d7a3c/053710b6-de16-42a9-9365-131bc73d7a3c.jsonl"}
```

`tool_output` (elided here — full text is the Playwright "Missing X server"
error page, real and observed, several KB of Chromium launch-arg dump) starts
identically for both entries:
`"{\"content\":[{\"type\":\"text\",\"text\":\"### Error\\nError: async initializeServer: Target page, context or browser has been closed\\nBrowser logs:\\n...` —
confirms `hook_event_name` stays `postToolUse` even when the underlying tool
call errors (Cursor treats a JSON-RPC-successful-but-error-content MCP
response as a normal `postToolUse`, not a `postToolUseFailure` — the
2026-07-28 spike's claim that "a failed MCP call routes to
`postToolUseFailure`" needs this caveat: it's the *hook delivery* that failed
in that spike's case, not an in-band MCP error response like this one).

`catch-all.jsonl` and `no-matcher.jsonl` (both size-identical, both matching
everything) also captured all of the above (control + MCP events combined),
confirming no matcher-shape surprises at the "match everything" end.
`mcp-literal-toolname.jsonl` (the round-2 fallback for re-probing with the
literal observed `tool_name` as its own matcher, per the accepted plan) was
never populated because it was never needed — round 1 already showed both
`MCP:.*` and `MCP:*` firing, so the "must each tool be named individually"
branch of Q1 does not apply.

**Q1 answer**: the issue's exact literal `MCP:*` **does** fire for a real MCP
tool call, and so does `MCP:.*`. A single wildcard-shaped `postToolUse` entry
(either form) covers every MCP tool observed here — **no per-tool matcher
list is needed**. (Mechanism: see the "2026-09-08" note under the static
regex analysis above — it works because `tool_name` literally starts with the
substring `MCP:`, and an unanchored regex match against "`MCP` + zero-or-more
`:`" is satisfied by that substring alone.)

**Q2 answer**: `tool_name` for an MCP tool call is `MCP:<bare_tool_name>` —
observed as `MCP:browser_navigate` and `MCP:browser_take_screenshot`. Neither
of the issue's two anticipated forms (fully server-qualified
`mcp_playwright_browser_take_screenshot`, or fully bare
`browser_take_screenshot`) is exactly right — it's a third shape: a literal
`MCP:` prefix (no server name embedded) on the server's own bare tool name
(confirmed as `browser_take_screenshot`, matching the direct-protocol
`tools/list` result from the 2026-09-07 half of this doc).

## Field notes for the adapter

- **`tool_output` is still a JSON-encoded string** (per the 2026-07-28
  spike) — `scan_response_image_path`'s `fromjson?` handles this and was
  exercised as-is in the synthetic payloads above.
- **`cwd` rewrite**: `cursor_extract_touched_paths` calls
  `cursor_effective_cwd`, which falls back to `workspace_roots[0]` when
  `.cwd` is empty — this was mirrored in the synthetic payloads by setting
  `cwd` directly to the intended effective value rather than leaving it
  empty, matching the shim's own pre-rewrite before it calls
  `scan_response_image_path`.
- **Regex truncation bug — filed and fixed as #213 (closed).** Not touched by
  this probe (out of scope per the task brief); left exactly as originally
  documented below for the record.
  `scan_response_image_path`'s capture pattern
  `(?<p>(?:/|\./)[^\s]*\.(?:png|jpe?g|gif|webp|bmp))` is unanchored, so on a
  relative path that lacks a leading `/` or `./` (e.g.
  `work/pw-output/page-….png` or `.playwright-mcp/page-….png` — both real,
  observed `playwright-mcp` outputs for the no-`filename` case, whether or
  not `--output-dir` is set) it does **not** fail to match — it matches
  starting at the *first embedded `/`* inside the string, silently
  discarding everything before it. The resulting path is neither the real
  relative path nor a real absolute path; it fails the `$cwd`-prefix
  containment check and the `-f` existence check, so `scan_response_image_path`
  returns empty. **This means a screenshot saved by playwright-mcp's own
  default naming (no `filename` argument) will be dropped by the scanner
  today even if a matcher fires and cwd/workspace_roots are wired up
  correctly** — the bug is independent of the matcher question. It only
  succeeds when the server happens to relativize the path with an explicit
  `./` prefix, which was observed only in the explicit-`filename`-under-cwd
  case (Payload C), not in either no-`filename` case.
- **`tool_input`-only finding: does NOT apply to playwright-mcp 0.0.80.**
  `scan_response_image_path` never reads `tool_input`, and
  `cursor_extract_touched_paths`'s `tool_input`-reading branch only fires for
  `tool_name` `Read`/`Write` — so a payload whose *only* path is in
  `tool_input.filename` (never echoed back in the response text) would need
  a shim change, not just a matcher. Empirically, though, this server always
  echoes the path back in a `text` content block in `tool_output` regardless
  of whether `filename` was given — so for playwright-mcp specifically, the
  cheaper "shim `tool_input`" branch is not the blocker; the regex
  truncation above is. Whether other MCP screenshot servers (e.g.
  firefox-devtools' `screenshot_page`) echo the path the same way was not
  tested — out of scope per the probe brief (Firefox needs an
  already-running instance, unavailable here).

## Method log

| Date | Attempt | Result |
|------|---------|--------|
| 2026-09-07 | Backed up `~/.cursor/hooks.json` → `.probe-bak`, checked it's a regular file (not a nix-store symlink; `~/.cursor/mcp.json` IS a symlink, hooks.json is not) | OK, writable |
| 2026-09-07 | `jq`-merged 5 `postToolUse` + 5 `postToolUseFailure` probe entries (`control`, `MCP:.*`, `MCP:*`, `.*`, no-matcher) alongside the existing real `images.sh`/`diagrams.sh` entries | Valid JSON, installed |
| 2026-09-07 | `cursor-agent mcp list` from `/tmp/aeye-mcp-probe/work` | `context7: ready`, `firefox-devtools: ready`, `playwright: ready` — servers load fine |
| 2026-09-07 | `cursor-agent -p -f "call context7 resolve-library-id …"` | `Error: Authentication required. Please run 'agent login' first, or set CURSOR_API_KEY environment variable.` — zero hook logs produced, including `control.jsonl` |
| 2026-09-07 | Checked `CURSOR_API_KEY` (unset), `~/.config/cursor/cli-config.json` keys (no key/token field), `~/.config/cursor` for cached session, `/run/agenix`, `/run/secrets` for a stashed key | None found — genuine missing session, not a path/env mismatch between this shell and the interactive one |
| 2026-09-07 | `NO_OPEN_BROWSER=1 cursor-agent login` (to inspect the login flow only) | Blocked by this session's own permission classifier as an account-affecting action outside a probe's authorized scope — correctly not attempted further |
| 2026-09-07 | Direct MCP stdio JSON-RPC client (`node`, hand-rolled) → `playwright-mcp --headless` `tools/list` | Server advertises bare `browser_take_screenshot` (Playwright 1.63.0-alpha-2026-08-31); confirms the "bare" half of Q2 for the underlying server, not for Cursor's wrapping |
| 2026-09-07 | Same client: `browser_navigate` (data: URL) + `browser_take_screenshot` (scale only, no filename), `--output-dir` set | File written under output-dir; response has `text`+`image` content blocks; text path has no leading `/`/`./` |
| 2026-09-07 | Same, no `--output-dir` (server default) | File at `<server cwd>/.playwright-mcp/page-<ts>.png`; text path `.playwright-mcp/page-<ts>.png`, no leading `./` |
| 2026-09-07 | Same, explicit absolute `filename` | Only `text` content block (base64 fully suppressed); file at the exact absolute path given; text path relativized with a leading `./` |
| 2026-09-07 | Replayed the two no-`filename` response texts through `scan_response_image_path`'s own jq capture expression, wrapped as synthetic Cursor `tool_output` strings (Payloads A, B) | Regex captures a truncated path starting at the first embedded `/`; fails containment/existence; `scan_response_image_path` returns empty for both |
| 2026-09-07 | Replayed the explicit-`filename` response through `scan_response_image_path` and `cursor_extract_touched_paths` (Payload C) | Full `./explicit-shot.png` captured, resolves under cwd, passes containment and `-f` check; both functions return the correct path |
| 2026-09-07 | Restored `~/.cursor/hooks.json` from backup, verified `postToolUse` back to the original 3 entries, trashed the backup and `/tmp/aeye-mcp-probe` (`gtrash put --home-fallback` — plain `gtrash put` fails on `/tmp`'s separate filesystem without root to create `/.Trash-1000`) | Done |
| 2026-09-08 | `cursor-agent status` on a work-profile host | `✓ Logged in as noam@factify.com` — blocker gone |
| 2026-09-08 | Checked `~/.cursor/hooks.json` is a regular file (not a symlink), `cursor-agent --version` → `2026.09.02-c22c1a3` (same build as 2026-09-07) | OK |
| 2026-09-08 | Wrote 6 real executable probe scripts under `/tmp/aeye-mcp-probe/scripts/` (`control.sh`, `mcp-colon-dotstar.sh`, `mcp-literal-wildcard.sh`, `catch-all.sh`, `no-matcher.sh`, `mcp-literal-toolname.sh`), each `cat >>`-ing its own log file — used real script paths instead of the recipe's inline `sh -c '...'`, since Cursor's shell-interpretation of an inline command string was never actually verified and the repo's own `hooks.json` only ever uses bare script paths | All executable, valid |
| 2026-09-08 | `jq`-merged 5 `postToolUse` + 5 `postToolUseFailure` probe entries (mirroring `timeout: 15` on the existing entries) alongside the real `images.sh`/`diagrams.sh`/`cursor-status-hook`/`session-*` entries; validated with `jq .` | Valid JSON, 8 `postToolUse` / 6 `postToolUseFailure` entries total |
| 2026-09-08 | `cursor-agent mcp list` from `/tmp/aeye-mcp-probe/work` | `playwright: ready` (plus context7/firefox-devtools/posthog ready, several `requires_authentication`) |
| 2026-09-08 | `cursor-agent -p -f "Read the file /etc/hostname"` (positive control) | Succeeded; `control.jsonl` non-empty (2 events attributable to this run's `conversation_id`, plus incidental events from an unrelated concurrent `cursor-agent` session sharing the same global hooks file — see "Note on `control.jsonl` cross-contamination" above) |
| 2026-09-08 | `cursor-agent -p -f "Navigate to about:blank using the playwright browser, then call the playwright browser_take_screenshot tool with no filename argument"` | Both MCP calls attempted; both failed at the browser-launch step (`Missing X server or $DISPLAY` — this host is headless with no Xvfb) — irrelevant to Q1/Q2, which only need the call observed by the hook system, not to succeed |
| 2026-09-08 | Inspected all probe logs (`wc -c`, `jq -c`, `grep -o '"tool_name":"[^"]*"'`) | `mcp-colon-dotstar.jsonl` and `mcp-literal-wildcard.jsonl` both captured all 3 MCP events (`MCP:browser_navigate` ×2, `MCP:browser_take_screenshot` ×1), byte-identical between the two files; `catch-all.jsonl`/`no-matcher.jsonl` captured everything (control + MCP); `control.jsonl` did not capture any MCP event (correct discrimination) — Q1 and Q2 answered directly, round-2 literal-matcher probe (`mcp-literal-toolname.sh`) not needed since a wildcard already fired |
| 2026-09-08 | Copied `/tmp/aeye-mcp-probe/logs/` and `~/.cursor/hooks.json.probe-bak` into the session scratchpad before any cleanup | Evidence preserved |
| 2026-09-08 | Restored `~/.cursor/hooks.json` from `.probe-bak`; `diff` against the backup was empty (back to the original 3 `postToolUse` / 1 `postToolUseFailure` entries); `gtrash put --home-fallback` the backup and `/tmp/aeye-mcp-probe` | Done, verified clean |
| 2026-09-08 | `gh issue view 213` — checked whether the regex-truncation bug from "Field notes for the adapter" was already filed separately | Yes: `#213 fix(core): unanchored capture() truncates relative image paths, silently dropping MCP screenshots` — filed and closed (fixed) independently of this probe |

## Recap: which follow-up branch does the evidence point to?

**2026-09-08 update: Q1 and Q2 are now both answered, and the answer confirms
the call below rather than changing it.** Neither of the issue's two
anticipated branches is quite right as stated, and there's a third, cheaper
shape the issue didn't anticipate:

- **"Small: just add a matcher"** — **Q1 confirms a matcher fires**: the
  issue's exact literal `MCP:*` (or `MCP:.*`) fires for every MCP tool call
  observed, as a single wildcard entry — no per-tool matcher list is needed,
  so the "ever-growing matcher list" concern the issue raised does **not**
  materialize. But the *scanner* will still drop the screenshot in the common
  case regardless: `scan_response_image_path`'s path-capture regex truncates
  any relative path that isn't `/`- or `./`-prefixed, which is exactly what
  `playwright-mcp` emits by default (no `filename` given). So "add a matcher,
  scanner handles it unchanged" is false for the default/no-`filename` path —
  a scanner regex fix is also required. (That regex fix is **already filed
  and closed as #213**, independent of this probe.)
- **"Bigger: decode+write base64 ourselves"** — not needed for
  `playwright-mcp` 0.0.80: it writes a real file to disk in *every* case
  tested, filename or not. The base64 in `tool_output` for the no-`filename`
  case is a redundant read, not the only way to get the picture. Decoding
  it would be strictly more work than fixing the path-capture regex.
- **Third, cheaper shape not in the issue**: fix `scan_response_image_path`'s
  regex so it doesn't require a leading `/` or `./` to start capturing. That
  is a small, self-contained scanner change, independent of and cheaper than
  either issue-anticipated branch. **#213 shipped a fix along these lines**
  (verified in this repo's current `adapters/core/manifest-extract.sh:60`) —
  though not the exact form speculated when this doc was first written: the
  landed fix anchors the capture to a string-start-or-preceding-whitespace
  boundary (`(?:^|(?<=\s))[^\s]*\.(?:png|jpe?g|gif|webp|bmp)`) rather than
  requiring a `/`/`./` prefix at all, which is a more general fix than either
  of the two variants floated here.

**Does Q1's outcome change the call? No — it strengthens it.** The issue
framed Q1 as a fork: a working wildcard keeps the follow-up small, while
per-tool naming would make it "an ever-growing matcher list" and open the
question of whether shipping it is worth the maintenance cost. The wildcard
won. Combined with #213 already fixing the scanner-side regex bug, **the
remaining follow-up for aeye #172 is now genuinely small**: add one
`postToolUse` entry (matcher `MCP:*` or, to avoid relying on the
zero-or-more-colons accident documented above, the more semantically honest
`MCP:.*`) to `adapters/cursor/hooks.json` pointing at the existing
`images.sh`, mirrored on `postToolUseFailure`. No shim change, no base64
decoding, no per-tool matcher list.
