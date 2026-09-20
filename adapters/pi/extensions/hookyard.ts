// hookyard's Pi bridge. Pi exposes no subprocess hook protocol at all: its hook
// surface is the extension API, so the hook *is* this file — JavaScript pi
// loads into its own process and awaits.
//
// One fixed template plus exactly one spliced value. Every install-specific
// string — router path, state directory, event names, matchers — arrives inside
// DATA as json.Marshal output, so a hostile handler string lands as JSON data
// and can never become code. Nothing else is ever concatenated into this
// source (§8).
//
// Plain ESM JavaScript under a .ts name, and both halves are load-bearing:
// `node --check` can parse plain JS, which is the only gate in this repo that
// looks at this file at all, and pi loads it through jiti regardless of the
// extension. No type annotations, no type-only imports.
//
// Node builtins only. The bridge sits in pi's config directory with no
// node_modules beside it, so a value import from the pi package would throw at
// load — and an extension that throws at load takes down every extension pi
// would have loaded after it.

// execFile for a router — it buffers the reply and the bridge waits for it —
// and spawn for a command's exec, which is neither buffered nor waited for.
import { execFile, spawn } from "node:child_process";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const DATA = {"timeout_ms":5000,"pi_version":"","root":"..","entries":[{"event":"session_start","matcher":"","bin":"bin/hookyard","args":["route","--registered-for","pi","--event","session_start"]},{"event":"tool_result","matcher":"","bin":"bin/hookyard","args":["route","--registered-for","pi","--event","post_tool"]}],"commands":[{"name":"aeye","description":"Open the aeye image carousel for this session","bin":"commands/aeye.sh","args":[]}]};

// A built package must name no install in its bytes, so it ships its root as a
// path relative to this file and resolves it here; the yard bridge's own paths
// are already absolute and DATA.root is null. One code path, selected by data.
const base = DATA.root === null ? null : resolve(dirname(fileURLToPath(import.meta.url)), DATA.root);

// Pi exposes no version to an extension. Yard mode splices in the install-time
// value; a built package has none to splice, so it reads Pi's own package.json
// beside the interpreter (the one node-builtins source there is). The name
// check is the whole of it: without it an unrelated package.json sitting next
// to some other interpreter would stamp its version on every record as Pi's.
const PI_PACKAGE_NAME = "@earendil-works/pi-coding-agent";

function installedPiVersion() {
  try {
    const pkg = JSON.parse(readFileSync(resolve(dirname(process.execPath), "package.json"), "utf8"));
    return pkg.name === PI_PACKAGE_NAME ? pkg.version : undefined;
  } catch {
    return undefined;
  }
}

// "unknown" rather than an absent key: envelope.Detect keys on this key's
// presence, JSON.stringify drops an undefined value, and a dropped key would
// make every Pi hook a silent no-op no Go test can see.
const PI_VERSION = DATA.pi_version || installedPiVersion() || "unknown";

// Pi imposes no timeout of its own — it awaits the handler's promise — so a
// wedged router would stall the turn forever. On the other three engines the
// engine owns the timeout, which makes this the one place hookyard can leak an
// orphaned router per tool call; the child is killed, not merely abandoned.
//
// Resolves, never rejects. A missing or non-executable binary, a non-zero exit
// and the timeout kill all arrive as `error`, and §5 spends every one of them
// on allow.
//
// `settle` rather than `resolve`, which node:path already owns here.
function askRouter(bin, args, payload) {
  return new Promise((settle) => {
    // argv arrives pre-split, so nothing here parses a path and execFile takes
    // it directly: no shell ever sees any of it, and no PATH lookup happens.
    // A package appends its own resolved root rather than shipping one.
    const child = execFile(
      base === null ? bin : resolve(base, bin),
      base === null ? args : [...args, "--plugin-root", base],
      { timeout: DATA.timeout_ms },
      (error, stdout) => settle(error ? "" : stdout),
    );
    // A router that exits before draining its stdin breaks the pipe under us,
    // and a payload larger than the OS pipe buffer — a tool_result carrying
    // file contents clears 64KB easily — is still being written when that
    // happens. The resulting EPIPE arrives as an 'error' event on the stdin
    // stream, outside the promise and outside the handler's try/catch, so with
    // no listener node treats it as unhandled and kills pi's whole process,
    // taking every later hook with it — the exact inversion of the fail-open
    // guarantee above. Swallowing it here leaves the execFile callback to
    // resolve the call the way it resolves every other router failure. The
    // child itself needs no such listener: execFile registers its own 'error'
    // handler and routes a failed spawn into that callback.
    child.stdin.on("error", () => {});
    child.stdin.end(payload);
  });
}

// Blocks only on a well-formed deny. Empty stdout is how the router abstains,
// and unparsable stdout or a reply carrying no decision are the router's own
// failures — all three land here as allow.
function decision(stdout) {
  let reply;
  try {
    reply = JSON.parse(stdout);
  } catch {
    return undefined;
  }
  if (!reply || reply.block !== true) return undefined;
  return { block: true, reason: typeof reply.reason === "string" ? reply.reason : "Blocked by hookyard" };
}

// decision()'s mirror on the other reply path, and it owes the same fail-open:
// unparsable stdout, the empty stdout the router abstains with, a reply with no
// advisory, a non-string advisory and "" all yield nothing to deliver. Reading
// an advisory out of a shape this does not understand would put the router's
// own failure in front of the model as hookyard's advice.
function advisory(stdout) {
  let reply;
  try {
    reply = JSON.parse(stdout);
  } catch {
    return undefined;
  }
  if (!reply || typeof reply.advisory !== "string" || reply.advisory === "") return undefined;
  return reply.advisory;
}

// An appended block reaches the model as the tool's own bytes. Shown one with
// nothing naming its source, the probe model called it "exactly the shape of an
// injection attempt" and disregarded it — and advice nobody acts on is the same
// loss as advice never delivered. So both deliveries say whose they are in
// their own text; the injected message also carries the customType pi stores
// beside it, which is metadata rather than a second attribution — whether pi
// puts it in front of the model is unverified, so nothing rests on it. Fixed
// strings, never spliced — a per-install label would be a second value reaching
// this source by concatenation (§8).
const ATTRIBUTION = "[hookyard advisory] ";
const ADVISORY_CUSTOM_TYPE = "hookyard";

// session_start's own return value reaches nothing, so its advisory waits here
// for the next before_agent_start. One slot, latest wins, and that is not lossy:
// buildPlan collapses every handler for one (engine, event) into a single entry,
// so a session_start yields one registration, one router call and one reply —
// whose advice the router has already folded together.
//
// A module-level slot is enough because pi binds one extension instance per
// session: /new, /resume and /fork emit session_shutdown for the old instance
// and reload the extensions for the new session, so no two sessions ever share
// this one.
let queuedAdvisory;

// Pi documents a tool_result handler as chaining middleware whose omitted fields
// keep their current value, so returning content alone is a patch rather than a
// replacement — and the patch appends, because the tool's own output is what the
// model is waiting on. A content that is not an array is a shape this bridge
// does not understand, and it declines rather than fabricating one: losing the
// tool's output costs more than the advisory is worth.
function appendAdvisory(event, advice) {
  if (advice === undefined || !Array.isArray(event.content)) return undefined;
  return { content: [...event.content, { type: "text", text: ATTRIBUTION + advice }] };
}

// For Pi, and only for Pi, hookyard authors the inbound payload — pi sends none
// — so its shape is pinned by the committed pi-*.json fixtures rather than by
// the engine. A key added here that no re-captured fixture carries does not
// exist.
const extras = {
  session_start: (event) => ({ reason: event.reason }),
  input: (event) => ({ prompt: event.text, source: event.source }),
  tool_call: (event) => ({
    tool_name: event.toolName,
    tool_input: event.input,
    tool_use_id: event.toolCallId,
  }),
  tool_result: (event) => ({
    tool_name: event.toolName,
    tool_input: event.input,
    tool_use_id: event.toolCallId,
    tool_response: { content: event.content, is_error: event.isError },
  }),
  turn_end: (event) => ({ turn_index: event.turnIndex }),
  session_shutdown: (event) => ({ reason: event.reason }),
};

// pi takes `--api-key <value>` on its own command line, so the invoking argv can
// carry a live credential — and `argv` travels to the router and into whatever a
// handler writes down. Matching on the flag *name* containing one of these
// words — in any dash spelling — rather than on an exact flag list, is what
// makes that fail closed: a secret-bearing flag pi adds in a later version is
// dropped by a bridge written before it existed.
//
// The same words gate sanitizeEnv below: pi's own process env can carry live
// LLM API keys, and a commands[] exec is a less-audited surface than a hook
// handler, so it gets the same fail-closed name match rather than an
// allowlist a legitimate command could fall outside of.
const SECRET_FLAG_WORDS = ["key", "token", "secret", "password"];

function sanitizeArgv(argv) {
  const kept = [];
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    const eq = arg.indexOf("=");
    // Leading dashes are stripped rather than required to be exactly two, so a
    // short or single-dash alias fails closed the same way its long form does.
    // A positional is not a flag and is never matched on, and a bare "-"
    // strips to nothing, which matches no word.
    const name = arg.startsWith("-") ? (eq === -1 ? arg : arg.slice(0, eq)).replace(/^-+/, "") : "";
    if (name === "" || !SECRET_FLAG_WORDS.some((word) => name.toLowerCase().includes(word))) {
      kept.push(arg);
      continue;
    }
    // `--flag=value` carries its value inside the element that was just
    // dropped; a bare `--flag` takes the element after it, whatever that is.
    if (eq === -1) i++;
  }
  return kept;
}

function sanitizeEnv(env) {
  const kept = {};
  for (const [name, value] of Object.entries(env)) {
    if (!SECRET_FLAG_WORDS.some((word) => name.toLowerCase().includes(word))) kept[name] = value;
  }
  return kept;
}

function payload(name, event, ctx) {
  const extra = extras[name];
  return JSON.stringify({
    hook_event_name: name,
    pi_version: PI_VERSION,
    session_id: ctx.sessionManager.getSessionId(),
    // Emitted even when empty: the envelope deliberately invents no cwd
    // fallback for Pi, so a dropped key would leave the record with no working
    // directory at all.
    cwd: ctx.cwd ?? "",
    // Emitted even when empty for cwd's reason, and empty is a real state
    // rather than a failure: getSessionFile() returns undefined under
    // --no-session, where there is no transcript on disk to point at.
    session_file: ctx.sessionManager.getSessionFile() ?? "",
    argv: sanitizeArgv(process.argv.slice(2)),
    ...(extra ? extra(event) : {}),
  });
}

export default function (pi) {
  for (const entry of DATA.entries) {
    pi.on(entry.event, async (event, ctx) => {
      try {
        // An empty matcher means every tool. Filtering here, before anything is
        // spawned, is what keeps a single-tool handler free on every other
        // call.
        if (entry.matcher && !entry.matcher.split("|").includes(event.toolName)) return undefined;

        const stdout = await askRouter(entry.bin, entry.args, payload(entry.event, event, ctx));

        // Only tool_call carries a return channel that can block. session_start
        // and tool_result carry an advisory one instead, and every other event
        // ignores the reply. The spawn still happens whatever the event,
        // because recording it is the router's job either way.
        if (entry.event === "tool_call") return decision(stdout);
        if (entry.event === "tool_result") return appendAdvisory(event, advisory(stdout));
        if (entry.event === "session_start") queuedAdvisory = advisory(stdout);
        return undefined;
      } catch {
        // Nothing may escape a handler: pi turns a throw out of a tool_call
        // handler into a block, so an escaping exception would invert the one
        // guarantee this file owns.
        return undefined;
      }
    });
  }

  // before_agent_start is never a manifest event and the router never speaks
  // for it: it exists here only to flush what session_start queued, so it is
  // registered from the entry list rather than by one. It fires once per prompt
  // rather than once per session, which is why the slot is cleared on the first
  // flush — the advice belongs to the prompt that followed the session_start,
  // not to every later one.
  if (DATA.entries.some((entry) => entry.event === "session_start")) {
    pi.on("before_agent_start", async () => {
      const advice = queuedAdvisory;
      queuedAdvisory = undefined;
      if (advice === undefined) return undefined;
      return { message: { customType: ADVISORY_CUSTOM_TYPE, content: ATTRIBUTION + advice, display: false } };
    });
  }

  // A command's exec is typically a viewer the user leaves open, and pi awaits
  // what a handler returns — so awaiting the child would hold the command open
  // for as long as the viewer lives, which is a hang rather than a slow
  // command. The handler is therefore done the moment the child exists, and
  // nothing here reports an exit status because nothing waits for one.
  //
  // With stdio ignored there is no stdin to put a payload on either, so the
  // whole contract is argv plus environment — and the environment is the point
  // of the surface: a subprocess cannot set a variable inside pi's own process,
  // so these three fixed, hookyard-namespaced keys are the only way pi's
  // session identity reaches the tool. A tool that reads some other name gets a
  // two-line exec that re-exports it, rather than hookyard growing an env-name
  // mapping for one consumer.
  //
  // base is never null here: commands exist only in a built package, and yard
  // mode renders an empty list this loop walks past.
  for (const command of DATA.commands) {
    pi.registerCommand(command.name, {
      description: command.description,
      handler: async (args, ctx) => {
        // Pi hands over everything after the command name as one string, and it
        // stays one argv element. Splitting it on whitespace would be guessing
        // an exec's own argument grammar from outside — the same split this
        // file stopped doing to router paths, in a new place. An exec that
        // wants its arguments split splits them itself.
        const child = spawn(resolve(base, command.bin), [...command.args, args], {
          env: {
            ...sanitizeEnv(process.env),
            HOOKYARD_SESSION_ID: ctx.sessionManager.getSessionId(),
            HOOKYARD_SESSION_FILE: ctx.sessionManager.getSessionFile() ?? "",
            HOOKYARD_CWD: ctx.cwd ?? "",
          },
          detached: true,
          stdio: "ignore",
        });
        // The only failure a handler that waits for nothing can still see, and
        // it is shown rather than thrown: a command the user typed that never
        // started is theirs to hear about, and pi has no more use for a throw
        // out of a command handler than out of a hook.
        child.on("error", (error) => {
          ctx.ui.notify(`hookyard: /${command.name} could not start ${command.bin}: ${error.message}`, "error");
        });
        child.unref();
      },
    });
  }
}
