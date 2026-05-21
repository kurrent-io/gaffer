import * as vscode from "vscode";

// Shared lifecycle for "fix this" prompts that live in the status
// bar rather than as toasts. VS Code auto-dismisses third-party
// message toasts after a few seconds (sticky is reserved for built-
// in extensions per the upstream source), so persistent prompts
// like "CLI not installed" and "update available" use a status bar
// item that stays visible until the user acts.
//
// One handle per prompt module. The handle owns the status bar
// item and the click command's lifecycle; the calling module owns
// the click handler's state (which deps to dispatch into).

export interface StatusBarPromptShowOptions {
	text: string;
	tooltip: string;
	backgroundColor?: vscode.ThemeColor;
}

export interface StatusBarPromptHandle {
	readonly active: boolean;
	show(opts: StatusBarPromptShowOptions): void;
	dismiss(): void;
	// Test-only: drop the item and unregister the command. Production
	// never disposes the command (it lives until the extension host
	// shuts down); tests need a clean slate per it() block.
	__resetForTests(): void;
}

export function createStatusBarPrompt(setup: {
	commandId: string;
	onClick: () => Promise<void> | void;
}): StatusBarPromptHandle {
	let item: vscode.StatusBarItem | null = null;
	// Register the command lazily on first show and keep the
	// disposable for the lifetime of the extension host. Per-show
	// register/dispose would race with `dismiss` and tolerate a
	// "command already registered" throw if the dismiss order ever
	// slipped; once-then-keep avoids the failure mode entirely.
	let commandDisposable: vscode.Disposable | null = null;

	return {
		get active() {
			return item !== null;
		},
		// Create-or-update. When the item already exists, mutate its
		// text/tooltip/colour in place rather than no-opping so the
		// caller's "fresh info wins" semantic holds (e.g. user changes
		// gaffer.command from typo1 to typo2 - the tooltip should now
		// show typo2). VS Code's StatusBarItem fields are live, so
		// updates take effect immediately.
		show(opts) {
			let current = item;
			const isNew = current === null;
			if (current === null) {
				if (commandDisposable === null) {
					commandDisposable = vscode.commands.registerCommand(
						setup.commandId,
						setup.onClick,
					);
				}
				current = vscode.window.createStatusBarItem(
					vscode.StatusBarAlignment.Right,
					100,
				);
				current.command = setup.commandId;
				item = current;
			}
			current.text = opts.text;
			current.tooltip = opts.tooltip;
			if (opts.backgroundColor !== undefined) {
				current.backgroundColor = opts.backgroundColor;
			}
			if (isNew) current.show();
		},
		dismiss() {
			item?.dispose();
			item = null;
		},
		__resetForTests() {
			item?.dispose();
			commandDisposable?.dispose();
			item = null;
			commandDisposable = null;
		},
	};
}
