import * as vscode from "vscode";
import type { FirstRunChoice } from "../telemetry/notice.js";

const TELEMETRY_DISCLOSURE_URL = "https://telemetry.gaffer.kurrent.io/";

// Disclosure-button labels in the message order they appear. Mapped
// back to the FirstRunChoice union below so the notice runner stays
// vscode-free. Renaming a label is a wire change for the runtime
// mapping but a no-op for the runner.
const BUTTON_DISABLE = "Disable telemetry";
const BUTTON_LEARN_MORE = "Learn more";
const BUTTON_DISMISS = "Dismiss";

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
			"KurrentDB Projections collects anonymous usage data, collected by Kurrent, Inc., to improve the tool. No projection code, stream names, or event content is sent. Click 'Dismiss' to accept, 'Disable telemetry' to opt out, or 'Learn more' for the full notice.",
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
