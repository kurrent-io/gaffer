// Builds the argv for the history viewer's cold spawns: the timeline read, a
// per-entry version diff, and a rollback apply.
//
// Flags go first; the positional projection name (and, for diff, it's the only
// positional) goes last, after a `--` separator. `--` terminates flag parsing so
// a projection named like `--env` (names are unconstrained gaffer.toml strings)
// can't be read as a flag. Mirrors deploy-args.ts and the debug spawn.

export function historyArgs(env: string, name: string): string[] {
	return ["history", "--json", "--env", env, "--", name];
}

// Rollback redeploys a prior version by full content hash. `--yes` because the
// webview runs its own confirm; a non-terminal spawn without it fails closed.
// The hash is a positional too, so both positionals follow the `--`.
export function rollbackArgs(
	env: string,
	name: string,
	hash: string,
): string[] {
	return ["rollback", "--json", "--yes", "--env", env, "--", name, hash];
}
