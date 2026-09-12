/**
 * aeye — pi adapter.
 *
 * Captures every image this pi session touches (read / write / edit / bash
 * screenshots) into the same per-pane manifest the aeye carousel already reads,
 * renders D2 diagrams written to the scratch dir, and injects the SessionStart
 * diagram guidance. The capture/render/manifest logic lives in the shell
 * scripts under `scripts/` (shared with the Claude/Codex/Cursor adapters via
 * `adapters/core`); this file only translates pi's lifecycle events into the
 * normalized payload those scripts take on stdin, runs them, and feeds any
 * agent-facing warning back into the tool result.
 *
 * Install as a pi package (`pi install <path-to-aeye>/adapters/pi`) or load the
 * file directly with `pi -e`.
 */

import { spawn } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";

const scriptsDir = resolve(dirname(fileURLToPath(import.meta.url)), "..", "scripts");

/** Tools whose calls can touch an image or a `.d2`. */
const CAPTURE_TOOLS = new Set(["read", "write", "edit", "bash", "shell"]);
const IMAGE_RE = /\.(png|jpe?g|gif|webp|bmp)\b/i;
const D2_RE = /\.d2\b/i;

interface NeutralPayload {
	tool_name: string;
	tool_input: unknown;
	tool_response: { content: unknown[] };
	cwd: string;
	session_id: string;
	ts?: string;
}

/** textBlocks returns only the text blocks of a pi content array. */
function textBlocks(content: unknown): string {
	if (!Array.isArray(content)) return typeof content === "string" ? content : "";
	const parts: string[] = [];
	for (const block of content) {
		if (block && typeof block === "object" && (block as { type?: string }).type === "text") {
			parts.push(String((block as { text?: unknown }).text ?? ""));
		}
	}
	return parts.join("\n");
}

/** runScript runs a bash script with payload on stdin and returns its stdout. */
function runScript(name: string, payload: unknown): Promise<string> {
	return new Promise((done) => {
		let child: ReturnType<typeof spawn>;
		try {
			child = spawn("bash", [join(scriptsDir, name)], { env: process.env });
		} catch {
			done("");
			return;
		}
		let stdout = "";
		let stderr = "";
		child.stdout?.on("data", (chunk) => {
			stdout += chunk;
		});
		child.stderr?.on("data", (chunk) => {
			stderr += chunk;
		});
		child.on("error", () => done(""));
		child.stdin?.on("error", () => {}); // EPIPE if the script exits before we finish writing
		child.on("close", () => {
			if (stderr.trim()) process.stderr.write(`[aeye] ${name}: ${stderr.trim()}\n`);
			done(stdout.trim());
		});
		child.stdin?.end(JSON.stringify(payload));
	});
}

/** additionalContext pulls `.hookSpecificOutput.additionalContext` from script output. */
function additionalContext(stdout: string): string {
	const parts: string[] = [];
	for (const line of stdout.split("\n")) {
		const trimmed = line.trim();
		if (!trimmed.startsWith("{")) continue;
		try {
			const parsed = JSON.parse(trimmed) as {
				hookSpecificOutput?: { additionalContext?: string };
			};
			const ctx = parsed.hookSpecificOutput?.additionalContext;
			if (ctx) parts.push(ctx);
		} catch {
			// Not an agent-facing banner (e.g. a diagram's open call) — ignore.
		}
	}
	return parts.join("\n\n");
}

function sessionId(ctx: ExtensionContext): string {
	try {
		return ctx.sessionManager.getSessionId();
	} catch {
		return "";
	}
}

/** source maps a pi session_start reason onto the adapter's startup contract. */
function sourceFor(reason: string): string {
	switch (reason) {
		case "resume":
		case "fork":
			return "resume";
		case "reload":
			return "reload";
		case "new":
			return "new";
		default:
			return "startup";
	}
}

/**
 * historicalCalls rebuilds normalized payloads from the session branch so the
 * backfill script can replay every image/diagram the session ever touched. The
 * assistant's toolCall blocks carry the inputs; the matching toolResult messages
 * carry bash output where a screenshot path may live. Image content blocks are
 * dropped — the path comes from the input and base64 would only bloat the file.
 */
function historicalCalls(ctx: ExtensionContext): NeutralPayload[] {
	const cwd = ctx.cwd;
	const results = new Map<string, string>();
	const calls: NeutralPayload[] = [];
	let branch: Array<{ type?: string; message?: Record<string, unknown> }> = [];
	try {
		branch = ctx.sessionManager.getBranch() as typeof branch;
	} catch {
		return calls;
	}
	const sid = sessionId(ctx);

	for (const entry of branch) {
		if (entry.type !== "message" || !entry.message) continue;
		const message = entry.message as {
			role?: string;
			toolCallId?: string;
			content?: unknown;
		};
		if (message.role === "toolResult" && message.toolCallId) {
			results.set(message.toolCallId, textBlocks(message.content));
		}
	}

	// Second pass preserves chronological order: assistant tool calls and user
	// bash executions are replayed in the order they happened, so the carousel's
	// "newest entry" selection after a resume matches the session.
	for (const entry of branch) {
		if (entry.type !== "message" || !entry.message) continue;
		const message = entry.message as {
			role?: string;
			content?: unknown;
			command?: string;
			output?: string;
		};
		if (message.role === "bashExecution") {
			calls.push({
				tool_name: "bash",
				tool_input: { command: message.command ?? "" },
				tool_response: { content: [message.output ?? ""] },
				cwd,
				session_id: sid,
			});
			continue;
		}
		if (message.role !== "assistant" || !Array.isArray(message.content)) continue;
		for (const block of message.content) {
			if (!block || typeof block !== "object") continue;
			const call = block as { type?: string; id?: string; name?: string; arguments?: unknown };
			if (call.type !== "toolCall" || !call.name) continue;
			calls.push({
				tool_name: call.name,
				tool_input: call.arguments ?? {},
				tool_response: { content: [call.id ? (results.get(call.id) ?? "") : ""] },
				cwd,
				session_id: sid,
			});
		}
	}
	return calls;
}

export default function aeye(pi: ExtensionAPI): void {
	let pendingGuidance = "";

	pi.on("session_start", async (event, ctx) => {
		const sid = sessionId(ctx);
		// Expose the id to the shell scripts and to the toggle the agent runs, so
		// capture and viewer agree on the manifest key outside tmux and the viewer
		// can reject a reused pane's stale manifest.
		if (sid) process.env.AEYE_SESSION_ID = sid;

		const source = sourceFor(String(event.reason));
		const base = { session_id: sid, cwd: ctx.cwd, source };

		// Diagram guidance — injected once via before_agent_start below.
		const guidance = additionalContext(await runScript("diagram-guidance.sh", base));
		if (guidance) pendingGuidance = guidance;

		// Rebuild the manifest from history when resuming or forking; the backfill
		// script clears + re-appends so a reused pane id can't bleed.
		if (source === "resume") {
			const calls = historicalCalls(ctx).filter((call) => CAPTURE_TOOLS.has(call.tool_name.toLowerCase()));
			const dir = mkdtempSync(join(tmpdir(), "aeye-pi-"));
			const callsFile = join(dir, "calls.jsonl");
			// Trailing newline so the script's `while read` sees the final call.
			writeFileSync(callsFile, calls.map((call) => JSON.stringify(call)).join("\n") + "\n");
			try {
				await runScript("session-backfill.sh", { ...base, calls_file: callsFile });
			} finally {
				rmSync(dir, { recursive: true, force: true });
			}
		}

		await runScript("session-reset.sh", base);
	});

	// Injects the session's diagram guidance into the first turn as a
	// context-only message (display: false), mirroring Claude/Codex
	// SessionStart additionalContext.
	pi.on("before_agent_start", async () => {
		if (!pendingGuidance) return undefined;
		const content = pendingGuidance;
		pendingGuidance = "";
		return {
			message: { customType: "aeye-diagrams", content, display: false },
		};
	});

	pi.on("tool_result", async (event, ctx) => {
		const name = String(event.toolName ?? "").toLowerCase();
		if (!CAPTURE_TOOLS.has(name)) return undefined;

		const input = event.input ?? {};
		const text = textBlocks(event.content);
		// Fast-bail in-process: never serialize a base64 image block or spawn bash
		// for a tool call that plainly touched neither an image nor a `.d2`.
		const haystack = `${JSON.stringify(input)}\n${text}`;
		const hasImage = IMAGE_RE.test(haystack);
		const hasD2 = D2_RE.test(haystack);
		if (!hasImage && !hasD2) return undefined;

		const payload: NeutralPayload = {
			tool_name: event.toolName,
			tool_input: input,
			tool_response: { content: [text] },
			cwd: ctx.cwd,
			session_id: sessionId(ctx),
		};

		const warnings: string[] = [];
		if (hasImage) {
			const warn = additionalContext(await runScript("images.sh", payload));
			if (warn) warnings.push(warn);
		}
		if (hasD2) {
			const warn = additionalContext(await runScript("diagrams.sh", payload));
			if (warn) warnings.push(warn);
		}
		if (warnings.length === 0) return undefined;

		// Surface the diagram compile/markdown warning to the model, the way the
		// hook adapters do with additionalContext.
		const content = Array.isArray(event.content) ? [...event.content] : [];
		for (const warn of warnings) content.push({ type: "text", text: warn });
		return { content };
	});

	pi.registerCommand("aeye", {
		description: "Open the aeye image carousel for this session",
		handler: async (_args, ctx) => {
			const sid = sessionId(ctx);
			const env = { ...process.env };
			if (sid) {
				env.AEYE_SESSION_ID = sid;
			}
			const toggle = env.AEYE_TOGGLE || "tmux-claude-images";
			try {
				const child = spawn(toggle, [], {
					env,
					detached: true,
					stdio: "ignore",
				});
				child.on("error", () => {
					ctx.ui.notify(`aeye: could not run ${toggle} (is it on PATH?)`, "error");
				});
				child.unref();
				ctx.ui.notify("aeye: opening the image carousel", "info");
			} catch {
				ctx.ui.notify(`aeye: could not run ${toggle} (is it on PATH?)`, "error");
			}
		},
	});
}
