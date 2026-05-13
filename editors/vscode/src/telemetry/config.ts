// Persisted extension telemetry state, stored at
// `${context.globalStorageUri}/telemetry.json`.
//
// Filesystem-backed (not vscode.ExtensionContext.globalState / Memento)
// so VS Code Settings Sync can't propagate identity across machines -
// "one install = one emitter_id" is a load-bearing invariant for the
// worker's identity-merge logic, and Settings Sync would silently break
// it. globalStorageUri is always a local path (extension storage isn't
// transported across remote workspaces), so node:fs is safe.

import { randomUUID } from "node:crypto";
import { mkdir, readFile, rename, unlink, writeFile } from "node:fs/promises";
import { join } from "node:path";

export interface TelemetryConfig {
	/** Per-install random UUID stamped on every envelope as `emitter_id`. */
	telemetry_id?: string;
	/** Per-install random UUID; HMAC key for project_id derivation. Never sent. */
	salt?: string;
	/** Explicit user choice. `undefined` means "no decision yet" (default permissive). */
	telemetry_enabled?: boolean;
	/** Latches `true` on a successful first-run notification ack. */
	disclosed?: boolean;
}

const FILE_NAME = "telemetry.json";

/**
 * Load the persisted config from `storageDir/telemetry.json`. Returns an
 * empty object when the file doesn't exist (fresh install). Throws on
 * parse errors - those indicate a corrupted file the caller needs to
 * surface rather than silently masking with defaults.
 */
export async function load(storageDir: string): Promise<TelemetryConfig> {
	let data: string;
	try {
		data = await readFile(join(storageDir, FILE_NAME), "utf8");
	} catch (err) {
		if ((err as NodeJS.ErrnoException).code === "ENOENT") return {};
		throw err;
	}
	const parsed: unknown = JSON.parse(data);
	if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
		throw new Error(
			`${FILE_NAME}: expected a JSON object, got ${Array.isArray(parsed) ? "array" : typeof parsed}`,
		);
	}
	const out: TelemetryConfig = {};
	const obj = parsed as Record<string, unknown>;
	if (typeof obj.telemetry_id === "string") out.telemetry_id = obj.telemetry_id;
	if (typeof obj.salt === "string") out.salt = obj.salt;
	if (typeof obj.telemetry_enabled === "boolean") {
		out.telemetry_enabled = obj.telemetry_enabled;
	}
	if (typeof obj.disclosed === "boolean") out.disclosed = obj.disclosed;
	return out;
}

/**
 * Persist the config atomically: write to a sibling `.tmp` file then
 * rename into place. A crash mid-write leaves the previous file
 * untouched rather than half-written nonsense the next load would
 * reject. mkdir is idempotent.
 *
 * Windows note: `fs.promises.rename` over an existing file has
 * historically been racy under antivirus or filesystem watchers
 * (EPERM/EEXIST). Node 14+ papers over the common cases, but the
 * worst-case failure mode here is the desired one - the rename
 * surfaces an error, the previous telemetry.json survives.
 */
export async function save(
	storageDir: string,
	config: TelemetryConfig,
): Promise<void> {
	await mkdir(storageDir, { recursive: true, mode: 0o700 });
	const finalPath = join(storageDir, FILE_NAME);
	const tmpPath = `${finalPath}.${randomUUID()}.tmp`;
	const body = `${JSON.stringify(config, null, 2)}\n`;
	await writeFile(tmpPath, body, { mode: 0o600 });
	try {
		await rename(tmpPath, finalPath);
	} catch (err) {
		// Best-effort cleanup so a rename failure doesn't leave a .tmp
		// containing identity material readable on disk. Swallow unlink
		// errors - the rename failure is what the caller needs to see.
		await unlink(tmpPath).catch(() => {});
		throw err;
	}
}

/**
 * Activation-friendly wrapper around `load`: on a parse-class error,
 * rename the bad file aside (`telemetry.json.corrupt-<ISO timestamp>`)
 * and return an empty config so the extension continues as a fresh
 * install. The quarantined file stays in place for a curious user to
 * inspect; the next `save` writes a clean replacement.
 *
 * Quarantine is narrow on purpose. We only re-mint when the file is
 * structurally broken (malformed JSON, non-object top level) - those
 * are the cases where the persisted identity is unreachable anyway.
 * I/O errors (EACCES, EPERM, EIO) propagate so the caller logs them;
 * silently quarantining on a permissions glitch would destroy a
 * real identity that's just temporarily unreadable.
 */
export async function loadSafe(storageDir: string): Promise<TelemetryConfig> {
	try {
		return await load(storageDir);
	} catch (err) {
		if (!isParseClassError(err)) throw err;
		await quarantine(storageDir).catch(() => {});
		return {};
	}
}

/** True for the error shapes that mean "file contents are unparseable":
 * JSON.parse's SyntaxError, or the validation throw from `load`. Note
 * other shapes (filesystem I/O) deliberately fall through. */
function isParseClassError(err: unknown): boolean {
	if (err instanceof SyntaxError) return true;
	if (err instanceof Error && err.message.startsWith(`${FILE_NAME}:`)) {
		return true;
	}
	return false;
}

async function quarantine(storageDir: string): Promise<void> {
	const ts = new Date().toISOString().replace(/[:.]/g, "-");
	const src = join(storageDir, FILE_NAME);
	const dst = join(storageDir, `${FILE_NAME}.corrupt-${ts}`);
	await rename(src, dst);
}
