import { canRunArgv } from "../discovery/cli.js";
import type { Manifest } from "../discovery/schemas.js";

// Builds the argv for a deploy spawn (preview or apply), scoped to a single
// projection when `name` is set.
//
// Flags go first; the positional projection name goes last, after a `--`
// separator. `--` terminates flag parsing so a projection named like
// `--no-validate` or `--env` (names are unconstrained gaffer.toml strings)
// can't be read as a flag and silently change the deploy's scope or behaviour.
// This mirrors the debug spawn (session-controller.ts), which guards the same
// hazard the same way.

export function deployPreviewArgs(
	env: string,
	name: string | undefined,
): string[] {
	return [
		"deploy",
		"--dry-run",
		"--json",
		"--env",
		env,
		...(name ? ["--", name] : []),
	];
}

export function deployApplyArgs(
	env: string,
	name: string | undefined,
	noValidate: boolean,
): string[] {
	const args = ["deploy", "--yes", "--json", "--stream", "--env", env];
	if (noValidate) args.push("--no-validate");
	if (name) args.push("--", name);
	return args;
}

/**
 * Whether the installed gaffer can run the deploy spawns. Checked against the
 * argv the extension builds, so these track the spawns above rather than
 * restating their flags.
 *
 * Both are gated together, on the maximal apply argv (`--no-validate` included):
 * the preview and the apply are one user-facing flow - the plan webview's Deploy
 * button applies the plan it is showing - so offering the preview while the apply
 * would be rejected just moves the failure later. A released gaffer carries the
 * whole deploy surface or none of it, so requiring the conditional flag costs
 * nothing real.
 */
export function canDeploy(manifest: Manifest | null): boolean {
	return (
		canRunArgv(manifest, deployPreviewArgs("env", undefined)) &&
		canRunArgv(manifest, deployApplyArgs("env", undefined, true))
	);
}
