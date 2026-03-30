const vscode = require("vscode");

class StatusViewProvider {
  constructor() {
    this._view = null;
    this._name = "";
    this._processed = 0;
    this._skipped = 0;
    this._errors = 0;
  }

  resolveWebviewView(webviewView) {
    this._view = webviewView;
    webviewView.webview.options = { enableScripts: true };

    webviewView.webview.onDidReceiveMessage((msg) => {
      if (msg.command === "pause") {
        vscode.commands.executeCommand("workbench.action.debug.pause");
      }
    });

    webviewView.onDidDispose(() => {
      this._view = null;
    });

    this._render();
  }

  setName(name) {
    this._name = name;
    this._processed = 0;
    this._skipped = 0;
    this._errors = 0;
    this._render();
  }

  addProcessed() {
    this._processed++;
    this._scheduleRender();
  }

  addSkipped() {
    this._skipped++;
    this._scheduleRender();
  }

  addError() {
    this._errors++;
    this._scheduleRender();
  }

  _scheduleRender() {
    if (this._renderTimer) return;
    this._renderTimer = setTimeout(() => {
      this._renderTimer = null;
      this._render();
    }, 200);
  }

  _render() {
    if (!this._view) return;

    const name = this._name || "projection";
    const stats = [];
    if (this._processed > 0) stats.push(`${this._processed.toLocaleString()} events processed`);
    if (this._skipped > 0) stats.push(`${this._skipped.toLocaleString()} events skipped`);
    if (this._errors > 0) stats.push(`${this._errors.toLocaleString()} errors`);
    const statsHtml = stats.length > 0
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

module.exports = { StatusViewProvider };
