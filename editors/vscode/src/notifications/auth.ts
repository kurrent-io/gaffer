import * as vscode from "vscode";

// Shown when a live run can't authenticate without an interactive sign-in
// (no stored token, or a keyring that can't be unlocked non-interactively).
// Resolves true when the user chose to sign in.
export async function showAuthRequired(env: string): Promise<boolean> {
	const choice = await vscode.window.showErrorMessage(
		`Environment "${env}" needs you to sign in before running against it.`,
		"Sign in",
	);
	return choice === "Sign in";
}
