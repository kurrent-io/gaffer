import * as vscode from "vscode";

export class StatusViewProvider implements vscode.WebviewViewProvider {
	#view: vscode.WebviewView | null = null;
	#name = "";
	#processed = 0;
	#skipped = 0;
	#errors = 0;
	#renderTimer: NodeJS.Timeout | null = null;

	resolveWebviewView(webviewView: vscode.WebviewView): void {
		this.#view = webviewView;
		webviewView.webview.options = { enableScripts: true };

		webviewView.webview.onDidReceiveMessage((msg: { command?: string }) => {
			if (msg.command === "pause") {
				vscode.commands.executeCommand("workbench.action.debug.pause");
			}
		});

		webviewView.onDidDispose(() => {
			this.#view = null;
		});

		this.#render();
	}

	reset(name: string): void {
		this.#name = name;
		this.#processed = 0;
		this.#skipped = 0;
		this.#errors = 0;
		this.#render();
	}

	addProcessed(): void {
		this.#processed++;
		this.#scheduleRender();
	}

	addSkipped(): void {
		this.#skipped++;
		this.#scheduleRender();
	}

	addError(): void {
		this.#errors++;
		this.#scheduleRender();
	}

	#scheduleRender(): void {
		if (this.#renderTimer) return;
		this.#renderTimer = setTimeout(() => {
			this.#renderTimer = null;
			this.#render();
		}, 200);
	}

	#render(): void {
		if (!this.#view) return;

		const name = this.#name || "projection";
		const stats: string[] = [];
		if (this.#processed > 0)
			stats.push(`${this.#processed.toLocaleString()} events processed`);
		if (this.#skipped > 0)
			stats.push(`${this.#skipped.toLocaleString()} events skipped`);
		if (this.#errors > 0) stats.push(`${this.#errors.toLocaleString()} errors`);
		const statsHtml = stats.length
			? stats.map((s) => `<div class="stat">${s}</div>`).join("")
			: `<div class="stat">Waiting for events...</div>`;

		this.#view.webview.html = `<!DOCTYPE html>
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
