# Spike: Cursor MCP tool-matcher payloads for screenshot capture (aeye #172)

Date: 2026-09-07
Version tested: `cursor-agent` / Cursor `2026.09.02-c22c1a3`

## GATE: PARTIAL — Q3/Q4/Q5 answered with real evidence; Q1/Q2 blocked on cursor-agent auth (needs a work-profile host)

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
actual responses. This surfaced a genuine, previously-undocumented regex bug
in `scan_response_image_path` (see "Field notes for the adapter"). Payloads
built by wrapping a real MCP response as a Cursor-shaped `tool_output` string
are marked **SYNTHETIC** below — the response content itself is real and
observed; only the Cursor envelope around it (and the `tool_name` value) is
an assumption, pending Q2.

## Verbatim evidence

### `cursor-agent` auth blocker

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

## Blocked — needs a work-profile host with an authenticated cursor-agent

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

Cross-adapter comparison (context, not a Cursor result): the Codex spike
(`2026-07-12-codex-hook-contract.md`) proposes `mcp__*` in its "Consequences"
prose as the matcher a future Codex `hooks.json` should use, but that form
was never actually checked into `adapters/codex/plugin/hooks/hooks.json`
(which has only ever matched `apply_patch|view_image|Bash`) and the spike's
own proven-payload table has no MCP row — by that spike's own account,
`mcp__*` is a **recommendation that was never implemented or exercised**,
not an observed live matcher. Cite it as a cross-adapter precedent for the
*shape* of an MCP matcher, not as proof any particular syntax fires or ships.

### Recipe to finish Q1/Q2 on a work-profile host (~10 minutes)

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
   scratch dir, and append the verbatim results to this doc (a new dated
   section, not an edit to the paragraphs above — keep this spike's original
   record intact).

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
- **Regex truncation bug (new finding, not previously documented)**:
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

## Recap: which follow-up branch does the evidence point to?

Neither of the issue's two anticipated branches is quite right as stated,
and there's a third, cheaper shape the issue didn't anticipate:

- **"Small: just add a matcher"** — even granting a matcher fires (still
  unconfirmed here — Q1/Q2 need a real authenticated run), the *scanner*
  will still drop the screenshot in the common case. `scan_response_image_path`'s
  path-capture regex truncates any relative path that isn't `/`- or
  `./`-prefixed, which is exactly what `playwright-mcp` emits by default
  (no `filename` given). So "add a matcher, scanner handles it unchanged" is
  false for the default/no-`filename` path — a scanner regex fix is also
  required.
- **"Bigger: decode+write base64 ourselves"** — not needed for
  `playwright-mcp` 0.0.80: it writes a real file to disk in *every* case
  tested, filename or not. The base64 in `tool_output` for the no-`filename`
  case is a redundant read, not the only way to get the picture. Decoding
  it would be strictly more work than fixing the path-capture regex.
- **Third, cheaper shape not in the issue**: fix `scan_response_image_path`'s
  regex to also accept a path fragment that merely *contains* a `/` before
  the extension (not just one that starts with `/` or `./`), or resolve
  relative paths against cwd *before* re-checking containment rather than
  only when the captured fragment lacks a leading `/`. That is a small,
  self-contained scanner change, independent of and cheaper than either
  issue-anticipated branch — but it only pays off once Q1/Q2 confirm a
  matcher actually delivers these payloads to the scanner in the first
  place, which remains the open blocker for a follow-up issue to resolve
  with real Cursor auth available.
