import { randomBytes } from "node:crypto";
import type * as vscode from "vscode";

const SECRET_KEY = "gaffer.keyringPassword";

// Returns the extension-managed keyring passphrase, generating and persisting a
// random one on first use. It's injected into spawned gaffer processes as
// GAFFER_KEYRING_PASSWORD so gaffer's encrypted-file token store unlocks without
// a prompt on hosts with no OS keyring (gaffer ignores it when an OS keyring is
// available). Stored in VS Code SecretStorage, which is OS-encrypted at rest.
export async function getOrCreateKeyringPassword(
	secrets: vscode.SecretStorage,
): Promise<string> {
	const existing = await secrets.get(SECRET_KEY);
	if (existing) return existing;
	const generated = randomBytes(32).toString("hex");
	await secrets.store(SECRET_KEY, generated);
	return generated;
}
