import * as vscode from "vscode";

export class StatusViewProvider implements vscode.WebviewViewProvider {
	private _view: vscode.WebviewView | null = null;
	private _name = "";
	private _processed = 0;
	private _skipped = 0;
	private _errors = 0;
	private _renderTimer: NodeJS.Timeout | null = null;

	resolveWebviewView(webviewView: vscode.WebviewView): void {
		this._view = webviewView;
		webviewView.webview.options = { enableScripts: true };

		webviewView.webview.onDidReceiveMessage((msg: { command?: string }) => {
			if (msg.command === "pause") {
				vscode.commands.executeCommand("workbench.action.debug.pause");
			}
		});

		webviewView.onDidDispose(() => {
			this._view = null;
		});

		this._render();
	}

	reset(name: string): void {
		this._name = name;
		this._processed = 0;
		this._skipped = 0;
		this._errors = 0;
		this._render();
	}

	addProcessed(): void {
		this._processed++;
		this._scheduleRender();
	}

	addSkipped(): void {
		this._skipped++;
		this._scheduleRender();
	}

	addError(): void {
		this._errors++;
		this._scheduleRender();
	}

	private _scheduleRender(): void {
		if (this._renderTimer) return;
		this._renderTimer = setTimeout(() => {
			this._renderTimer = null;
			this._render();
		}, 200);
	}

	private _render(): void {
		if (!this._view) return;

		const name = this._name || "projection";
		const stats: string[] = [];
		if (this._processed > 0) stats.push(`${this._processed.toLocaleString()} events processed`);
		if (this._skipped > 0) stats.push(`${this._skipped.toLocaleString()} events skipped`);
		if (this._errors > 0) stats.push(`${this._errors.toLocaleString()} errors`);
		const statsHtml = stats.length
			? stats.map((s) => `<div class="stat">${s}</div>`).join("")
			: `<div class="stat">Waiting for events...</div>`;

		this._view.webview.html = `<!DOCTYPE html>
<html>
<head>
<style>
  body {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100vh;
    margin: 0;
    font-family: var(--vscode-font-family);
    color: var(--vscode-foreground);
    gap: 6px;
  }
  .title {
    font-size: 13px;
    opacity: 0.9;
  }
  .stat {
    font-size: 12px;
    opacity: 0.6;
  }
  button {
    margin-top: 8px;
    padding: 4px 16px;
    background: var(--vscode-button-background);
    color: var(--vscode-button-foreground);
    border: none;
    border-radius: 2px;
    cursor: pointer;
    font-size: 12px;
  }
  button:hover {
    background: var(--vscode-button-hoverBackground);
  }
</style>
</head>
<body>
  <div class="title">Running ${name}...</div>
  ${statsHtml}
  <button onclick="pause()">Pause to inspect</button>
  <script>
    const vscode = acquireVsCodeApi();
    function pause() { vscode.postMessage({ command: 'pause' }); }
  </script>
</body>
</html>`;
	}
}
