import * as vscode from "vscode";
import crossSpawn from "cross-spawn";
import * as v from "valibot";
import { log } from "../output.js";
import { ManifestSchema, type Manifest } from "./schemas.js";

const DEFAULT_COMMAND: readonly string[] = ["gaffer"];

/** Subset of the telemetry facade the spawn sites need: identity for
 * the `--invoker-id` flag, and the opt-out signal for the env
 * override. Structural so tests can pass `{ invokerId: () => ...,
 * isOptedOut: () => ... }` without building a full facade. */
export interface SpawnTelemetry {
	invokerId(): string | null;
	isOptedOut(): boolean;
}

/** Spawn surfaces the extension drives the CLI from. Maps 1:1 to the
 * wire `#InvokedVia` enum's editor-relevant variants. */
export type InvokedVia = "code_lens" | "command_palette" | "mcp_provider";

export interface Invocation {
	/** Per-install extension emitter_id, or null when opted out. */
	invokerId: string | null;
	/** Surface enum; omitted for extension-internal spawns (manifest, LSP). */
	invokedVia?: InvokedVia;
}

/**
 * Env to hand to a spawned gaffer process. Returns `undefined` (no
 * override; child inherits the extension host's `process.env`
 * unchanged) when the extension is consenting. When the extension is
 * opted out, returns a copy of `process.env` with
 * `GAFFER_TELEMETRY_OPTOUT=1` injected so the CLI's own opt-out
 * cascade silences it - opt-out in the extension propagates to its
 * spawned children.
 *
 * Gated on explicit opt-out, not on the absence of an `invokerId`:
 * the latter is also null when telemetry init fails (noop fallback),
 * where the user hasn't actually chosen to opt out.
 */
// Set once at activation from SecretStorage; injected into every gaffer spawn
// as GAFFER_KEYRING_PASSWORD so the encrypted-file token store unlocks without a
// prompt. gaffer ignores it when an OS keyring is available.
let keyringPassword: string | undefined;

export function setKeyringPassword(pw: string | undefined): void {
	keyringPassword = pw;
}

export function gafferSpawnEnv(
	optedOut: boolean,
): NodeJS.ProcessEnv | undefined {
	if (!optedOut) return undefined;
	return { ...process.env, GAFFER_TELEMETRY_OPTOUT: "1" };
}

/** Like `gafferSpawnEnv` but also carries GAFFER_KEYRING_PASSWORD, for spawns
 * that connect to KurrentDB and so touch the OAuth token store (dev/debug runs
 * and the sign-in terminal). Kept off the lsp/manifest/scaffold spawns, which
 * never authenticate, so the passphrase isn't handed to processes that don't
 * need it. */
export function gafferRunEnv(optedOut: boolean): NodeJS.ProcessEnv | undefined {
	if (!optedOut && !keyringPassword) return undefined;
	return {
		...process.env,
		...(optedOut ? { GAFFER_TELEMETRY_OPTOUT: "1" } : {}),
		...(keyringPassword ? { GAFFER_KEYRING_PASSWORD: keyringPassword } : {}),
	};
}

/** Same intent as `gafferRunEnv` but in the additive shape VS Code's
 * `McpStdioServerDefinition.env` expects: keys merged onto the parent
 * env at spawn time. The MCP server connects to KurrentDB for its live
 * tools, so it carries the keyring passphrase too. */
export function gafferMcpEnv(optedOut: boolean): Record<string, string> {
	return {
		...(optedOut ? { GAFFER_TELEMETRY_OPTOUT: "1" } : {}),
		...(keyringPassword ? { GAFFER_KEYRING_PASSWORD: keyringPassword } : {}),
	};
}

/**
 * Build the argv to invoke gaffer with the given subcommand args.
 *
 * Reads `gaffer.command` only from User scope - workspace and folder scope
 * are ignored as a defense against hostile workspaces overriding the
 * binary path. Falls back to `["gaffer"]` (resolved from PATH).
 *
 * Linkage flags are inserted between the binary prefix and the
 * subcommand args (root-level position) so a future passthrough-style
 * subcommand can't swallow them.
 */
export function buildGafferArgv(
	args: string[],
	invocation?: Invocation,
): string[] {
	const inspected = vscode.workspace
		.getConfiguration("gaffer")
		.inspect<string[]>("command");
	const userValue = inspected?.globalValue;
	const prefix =
		Array.isArray(userValue) && userValue.length > 0
			? userValue
			: [...DEFAULT_COMMAND];
	const flags: string[] = [];
	if (invocation && invocation.invokerId !== null) {
		flags.push(`--invoker-id=${invocation.invokerId}`, "--invoked-by=vscode");
		if (invocation.invokedVia !== undefined) {
			flags.push(`--invoked-via=${invocation.invokedVia}`);
		}
	}
	return [...prefix, ...flags, ...args];
}

/**
 * Fetch and parse the gaffer CLI manifest. Returns `null` rather than
 * throwing - the manifest can legitimately fail to load (CLI not
 * installed, workspace untrusted, bad config) and the rest of the
 * extension is built to handle a null manifest. `onError` fires for
 * actual fetch failures (CLI missing, parse errors); trust-skip is
 * silent.
 *
 * `invokerId` is appended as `--invoker-id` when non-null;
 * `--invoked-via` is never sent for the manifest fetch.
 */
export async function tryFetchManifest(
	cwd: string | undefined,
	telemetry: SpawnTelemetry,
	onError?: (err: unknown) => void,
): Promise<Manifest | null> {
	if (!vscode.workspace.isTrusted) {
		log("workspace untrusted, skipping manifest fetch");
		return null;
	}
	const argv = buildGafferArgv(["manifest"], {
		invokerId: telemetry.invokerId(),
	});
	try {
		const opts: ExecOpts = {};
		if (cwd !== undefined) opts.cwd = cwd;
		const env = gafferSpawnEnv(telemetry.isOptedOut());
		if (env !== undefined) opts.env = env;
		const output = await execFileAsync(argv, opts);
		const raw: unknown = JSON.parse(output);
		const parsed = v.safeParse(ManifestSchema, raw);
		if (!parsed.success) {
			throw new Error(
				`malformed manifest: ${parsed.issues.map((i) => i.message).join("; ")}`,
			);
		}
		log(`Manifest loaded (v${parsed.output.version})`);
		return parsed.output;
	} catch (err) {
		const msg = err instanceof Error ? err.message : String(err);
		log(`Manifest fetch failed: ${msg}`);
		onError?.(err);
		return null;
	}
}

/**
 * Run a one-shot gaffer subcommand whose output isn't JSON we parse.
 * Returns the CLI's stdout on success, or the rejection (with stderr
 * attached on err.cause.stderr, same shape as tryFetchManifest's
 * failure path) so each caller can shape its own error UX.
 *
 * Precondition: workspace must be trusted. Callers run the trust gate
 * themselves so they can pair it with the right user-facing toast
 * (`showTrustWarning` for command surfaces, silent skip for activation
 * paths). This helper does not re-check - it would either duplicate
 * the upstream message or fabricate an "init failed: workspace not
 * trusted" error that misclassifies the cause.
 *
 * Forwards --invoker-id / --invoked-by / --invoked-via to the CLI so
 * its own telemetry event for the subcommand links to the extension
 * identity. The linkage flags are omitted when `telemetry.invokerId()`
 * is null - the user is opted out (or telemetry init failed); in either
 * case there's no identity to link, and `buildGafferArgv` drops the
 * flags rather than emit a null-id placeholder. `invokedVia` is still
 * required by the type signature even though it has no effect in the
 * opted-out path: commands that bubble through this helper are always
 * user-driven (unlike the manifest fetch, which is extension-internal).
 */
export async function runGafferCommand(
	args: string[],
	cwd: string,
	telemetry: SpawnTelemetry,
	invokedVia: InvokedVia,
): Promise<{ ok: true; stdout: string } | { ok: false; err: unknown }> {
	const argv = buildGafferArgv(args, {
		invokerId: telemetry.invokerId(),
		invokedVia,
	});
	try {
		const env = gafferSpawnEnv(telemetry.isOptedOut());
		const opts: ExecOpts = { cwd };
		if (env !== undefined) opts.env = env;
		const stdout = await execFileAsync(argv, opts);
		log(`gaffer ${args[0] ?? ""} succeeded in ${cwd}`);
		return { ok: true, stdout };
	} catch (err) {
		const msg = err instanceof Error ? err.message : String(err);
		log(`gaffer ${args[0] ?? ""} failed: ${msg}`);
		return { ok: false, err };
	}
}

export const hasCommand = (m: Manifest | null, name: string): boolean =>
	m?.commands?.[name] != null;

export const hasFlag = (
	m: Manifest | null,
	command: string,
	flag: string,
): boolean => m?.commands?.[command]?.flags?.includes(flag) ?? false;

interface ExecOpts {
	cwd?: string;
	env?: NodeJS.ProcessEnv;
}

const SPAWN_TIMEOUT_MS = 10_000;

// Routed through cross-spawn so the Windows PATHEXT lookup works:
// `npm install -g @kurrent/gaffer` drops a `gaffer.cmd` shim into
// `%APPDATA%\npm`, and Node's own `child_process.execFile("gaffer", ...,
// { shell: false })` won't find it (shell: false skips PATHEXT).
// cross-spawn re-routes .cmd/.bat through cmd.exe with proper arg
// quoting so we keep `shell: false`-style safety without the
// injection surface that `shell: true` opens up.
//
// Error shape preserved from the previous execFile-based impl so
// telemetry's classifyManifestError stays accurate:
//   - spawn failure (binary not on PATH, EACCES, etc.) ⇒ err.code
//     is the OS error string (e.g. "ENOENT") and err.killed is unset
//   - timeout ⇒ err.killed === true and err.code is undefined
//   - non-zero exit ⇒ err.code is the numeric exit code
//   - any stderr is attached as err.cause.stderr (kept off
//     err.message so telemetry never accidentally ships local paths)
//
// Settles on whichever of `error`, the timeout timer, or `close`
// fires first, guarded by a single-settle flag. The standard Node
// guarantee is that `close` follows `error`, but a kill on Windows
// is best-effort and `close` can be slow to land, so we don't rely
// on `close` being the only settle path.
//
// Error messages never embed the argv: gaffer.command may be an
// absolute path and scaffold subcommands pass user-supplied paths
// as args; the telemetry exception builder ships err.message
// verbatim. The argv is reachable via the call-site log lines if
// it's needed for debugging.
function execFileAsync(
	argv: string[],
	options: ExecOpts = {},
): Promise<string> {
	return new Promise((resolve, reject) => {
		const [head, ...rest] = argv;
		if (!head) {
			reject(new Error("argv must not be empty"));
			return;
		}
		const child = crossSpawn(head, rest, {
			cwd: options.cwd,
			env: options.env,
			shell: false,
			windowsHide: true,
		});

		const stdoutChunks: Buffer[] = [];
		const stderrChunks: Buffer[] = [];
		let settled = false;

		const attachStderr = (err: Error): void => {
			const stderr = Buffer.concat(stderrChunks).toString().trim();
			if (stderr) {
				(err as { cause?: unknown }).cause = { stderr };
			}
		};
		const settle = (fn: () => void): void => {
			if (settled) return;
			settled = true;
			clearTimeout(timer);
			fn();
		};

		child.stdout?.on("data", (chunk: Buffer) => stdoutChunks.push(chunk));
		child.stderr?.on("data", (chunk: Buffer) => stderrChunks.push(chunk));

		const timer = setTimeout(() => {
			settle(() => {
				child.kill();
				const err = new Error(
					`Command timed out after ${SPAWN_TIMEOUT_MS}ms`,
				) as NodeJS.ErrnoException & { killed?: boolean };
				err.killed = true;
				attachStderr(err);
				reject(err);
			});
		}, SPAWN_TIMEOUT_MS);

		child.once("error", (err) => {
			settle(() => {
				attachStderr(err);
				reject(err);
			});
		});

		child.on("close", (code) => {
			settle(() => {
				if (code === 0) {
					resolve(Buffer.concat(stdoutChunks).toString());
					return;
				}
				const err = new Error("Command failed");
				if (code !== null) {
					// execFile's error.code carries the numeric exit code on
					// non-zero exit; preserve that shape for callers that
					// classify failures (telemetry's classifyManifestError).
					(err as { code?: number }).code = code;
				}
				attachStderr(err);
				reject(err);
			});
		});
	});
}
