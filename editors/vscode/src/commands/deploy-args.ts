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
