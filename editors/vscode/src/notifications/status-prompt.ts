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
		show(opts) {
			if (item !== null) return;
			if (commandDisposable === null) {
				commandDisposable = vscode.commands.registerCommand(
					setup.commandId,
					setup.onClick,
				);
			}
			const created = vscode.window.createStatusBarItem(
				vscode.StatusBarAlignment.Right,
				100,
			);
			created.text = opts.text;
			created.tooltip = opts.tooltip;
			if (opts.backgroundColor !== undefined) {
				created.backgroundColor = opts.backgroundColor;
			}
			created.command = setup.commandId;
			created.show();
			item = created;
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
