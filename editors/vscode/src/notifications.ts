// All user-facing toasts/dialogs in one place. Each function expresses
// the meaning of the notification rather than the primitive
// (showWarningMessage / showErrorMessage / etc.) - call sites read
// `notifications.showTrustWarning()` instead of choosing the right
// vscode.window method and re-typing the message.
//
// All exports return Thenable<unknown> for a uniform contract: callers
// await when they need to sequence on dismissal (e.g. to delay cleanup
// until the user has seen the error), or `void` when they don't care.

import * as vscode from "vscode";
import { showOutputPanel } from "./output.js";
import type { FirstRunChoice } from "./telemetry/notice.js";

const TELEMETRY_DISCLOSURE_URL = "https://telemetry.gaffer.kurrent.io/";

// Disclosure-button labels in the message order they appear. Mapped
// back to the FirstRunChoice union below so the notice runner stays
// vscode-free. Renaming a label is a wire change for the runtime
// mapping but a no-op for the runner.
const BUTTON_DISABLE = "Disable telemetry";
const BUTTON_LEARN_MORE = "Learn more";
const BUTTON_DISMISS = "Dismiss";

export const showManifestFailure = (err: unknown): Thenable<unknown> => {
	const raw = err instanceof Error ? err.message : String(err);
	// execFileAsync stashes stderr on err.cause.stderr (kept off
	// err.message so telemetry never accidentally ships local paths).
	const cause =
		err instanceof Error && typeof err.cause === "object" ? err.cause : null;
	const stderr =
		cause !== null && typeof (cause as { stderr?: unknown }).stderr === "string"
			? (cause as { stderr: string }).stderr
			: "";
	const detail = stderr ? `${raw} (stderr: ${stderr})` : raw;
	const truncated = detail.length > 200 ? `${detail.slice(0, 200)}…` : detail;
	return vscode.window
		.showErrorMessage(
			`Gaffer CLI failed: ${truncated}`,
			"View Output",
			"Open Settings",
		)
		.then((choice) => {
			if (choice === "View Output") {
				showOutputPanel();
			} else if (choice === "Open Settings") {
				void vscode.commands.executeCommand(
					"workbench.action.openSettings",
					"gaffer.command",
				);
			}
		});
};

export const showTrustWarning = (): Thenable<unknown> =>
	vscode.window
		.showWarningMessage(
			"Trust this workspace to enable Gaffer debugging.",
			"Manage Trust",
		)
		.then((choice) => {
			if (choice === "Manage Trust") {
				void vscode.commands.executeCommand("workbench.trust.manage");
			}
		});

export const showNoProjections = (): Thenable<unknown> =>
	vscode.window.showInformationMessage(
		"Gaffer: no projections found in this workspace.",
	);

export const showLspNotReady = (): Thenable<unknown> =>
	vscode.window.showInformationMessage(
		"Gaffer is still starting up. Try again in a moment.",
	);

export const showLspError = (): Thenable<unknown> =>
	vscode.window
		.showErrorMessage(
			"Gaffer: failed to fetch projections from the LSP server.",
			"View Output",
		)
		.then((choice) => {
			if (choice === "View Output") {
				showOutputPanel();
			}
		});

export const showDebugUnsupported = (): Thenable<unknown> =>
	vscode.window.showErrorMessage(
		"Gaffer: this gaffer version doesn't support `dev --debug`. Update gaffer or set gaffer.command to a newer build.",
	);

// Shared shape for any "LSP isn't running" surface: error toast
// with a "View Output" button that opens the LSP channel (NOT
// our generic Gaffer channel - the LSP one carries the
// actionable detail). Used by both the initial-start failure
// path (c.start() threw) and the give-up-after-restarts path.
const showLspBroken = (
	message: string,
	channel: vscode.OutputChannel,
): Thenable<unknown> =>
	vscode.window.showErrorMessage(message, "View Output").then((choice) => {
		if (choice === "View Output") {
			channel.show(true);
		}
	});

export const showLspFailedToStart = (
	detail: string,
	channel: vscode.OutputChannel,
): Thenable<unknown> =>
	showLspBroken(`Gaffer LSP failed to start: ${detail}`, channel);

export const showLspCrashed = (
	channel: vscode.OutputChannel,
): Thenable<unknown> =>
	showLspBroken(
		"Gaffer LSP keeps crashing - features that depend on it (lenses, projection list) are unavailable.",
		channel,
	);

export const showProjectionFault = (
	exitCode: number | null,
): Thenable<unknown> =>
	vscode.window.showErrorMessage(
		`Gaffer: projection faulted (exit code ${exitCode})`,
	);

export const showStartFailure = (message: string): Thenable<unknown> =>
	vscode.window.showErrorMessage(`Gaffer: ${message}`);

export const showProjectionFailed = (): Thenable<unknown> =>
	vscode.window.showErrorMessage(
		"Gaffer: projection failed - see Problems panel",
	);

export const showPortInUse = (description: string): Thenable<unknown> =>
	vscode.window
		.showErrorMessage(
			`Gaffer: ${description}. Change gaffer.debugPort or set it to -1 to let the OS pick a free port.`,
			"Open Settings",
		)
		.then((choice) => {
			if (choice === "Open Settings") {
				void vscode.commands.executeCommand(
					"workbench.action.openSettings",
					"gaffer.debugPort",
				);
			}
		});

// One-shot first-run telemetry disclosure. Mapped to the runner's
// FirstRunChoice union so notice.ts doesn't have to know about vscode.
//
// Button order is leftmost-is-affirmative per VS Code convention; the
// disable action sits on the right so a reflexive click doesn't opt
// the user out by accident. Dismissing the toast (X) is also treated
// as "accept" - same as the explicit Dismiss button.
export const showTelemetryDisclosure = (): Thenable<
	FirstRunChoice | undefined
> =>
	vscode.window
		.showInformationMessage(
			"Gaffer collects anonymous usage data, collected by Kurrent, Inc., to improve the tool. No projection code, stream names, or event content is sent. Click 'Learn more' to read the full notice and how to opt out.",
			BUTTON_DISMISS,
			BUTTON_LEARN_MORE,
			BUTTON_DISABLE,
		)
		.then((label) => {
			switch (label) {
				case BUTTON_DISABLE:
					return "disable";
				case BUTTON_LEARN_MORE:
					return "learn-more";
				case BUTTON_DISMISS:
					return "dismiss";
				default:
					return undefined;
			}
		});

export const openTelemetryDisclosurePage = (): Thenable<unknown> =>
	vscode.env.openExternal(vscode.Uri.parse(TELEMETRY_DISCLOSURE_URL));
