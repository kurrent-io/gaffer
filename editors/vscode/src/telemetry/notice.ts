// First-run telemetry disclosure flow. Fires once per install at the
// extension's first eligible activation, latches `disclosed=true` on
// any outcome that means the user has seen the notice (button click
// or X-close), records the user's choice via `telemetry_enabled`.
//
// Purely extension-side: writes only to this extension's own
// `telemetry.json`. UI-1566 made the CLI's notice self-suppressing
// on `--invoker-id` + non-TTY, so the extension doesn't need to
// propagate consent into the CLI's config.
//
// Vscode interaction (showInformationMessage, openExternal) is
// injected, so the runner is a pure unit. The thin adapters that
// thread vscode-specific bits live in src/notifications.ts.

import { editConfig, type TelemetryConfig } from "./config.js";

/** Resolved button click. `undefined` from the caller's prompt() means
 * the user closed via the X without picking a button. */
export type FirstRunChoice = "disable" | "learn-more" | "dismiss";

export interface RunFirstRunNoticeArgs {
	storageDir: string;
	config: TelemetryConfig;
	/** True when any opt-out signal is already in effect. Skips the
	 * notice - the user has already declined for this session. Once
	 * the cascade clears (e.g. user unsets DO_NOT_TRACK), the notice
	 * fires again because `disclosed` only latches when the user
	 * actually responded to it. */
	optedOut: boolean;
	/** Show the disclosure UI and resolve with the user's choice
	 * (`undefined` ⇒ closed without picking a button). */
	prompt: () => Thenable<FirstRunChoice | undefined>;
	/** Open the learn-more URL externally (e.g. browser). */
	openLearnMore: () => Thenable<void>;
}

/**
 * Run the first-run disclosure if the config indicates it's still
 * owed AND no opt-out is in effect. No-op otherwise.
 *
 * Outcomes:
 *
 * - `[Disable]` -> `telemetry_enabled=false`, `disclosed=true`.
 * - `[Dismiss]` or X-close -> `telemetry_enabled=true`,
 *   `disclosed=true`. Per the plan, an X-close means the user saw
 *   the notice and chose not to act; re-showing every activation
 *   would be hostile.
 * - `[Learn more]` -> opens the URL, leaves the persisted config
 *   untouched. The user has asked for more information, not made a
 *   decision; the next activation re-shows the notice so they can
 *   actually pick. Matches the CLI's conservative latching: only
 *   record disclosure when the user has actually seen and dispatched
 *   the prompt to a terminal choice.
 */
export async function runFirstRunNotice(
	args: RunFirstRunNoticeArgs,
): Promise<void> {
	if (args.config.disclosed) return;
	if (args.optedOut) return;

	const choice = await args.prompt();

	if (choice === "learn-more") {
		// Open the URL and leave disclosed unset - this isn't a
		// decision, it's a request for more context.
		await args.openLearnMore();
		return;
	}

	const optedOutByChoice = choice === "disable";
	await editConfig(args.storageDir, {
		disclosed: true,
		telemetry_enabled: !optedOutByChoice,
	});
}
