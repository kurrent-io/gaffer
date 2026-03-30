const vscode = require("vscode");

class TomlCodeLensProvider {
  constructor(cli, debugState) {
    this._cli = cli;
    this._debugState = debugState;
    this._onDidChange = new vscode.EventEmitter();
    this.onDidChangeCodeLenses = this._onDidChange.event;
  }

  refresh() {
    this._onDidChange.fire();
  }

  provideCodeLenses(document) {
    const lenses = [];
    const text = document.getText();
    const lines = text.split("\n");
    const tomlUri = document.uri;
    const cwd = vscode.Uri.joinPath(tomlUri, "..").fsPath;

    for (let i = 0; i < lines.length; i++) {
      if (lines[i].trim() !== "[[projection]]") continue;

      const name = extractName(lines, i + 1);
      if (!name) continue;

      const range = new vscode.Range(i, 0, i, lines[i].length);
      const lens = buildLens(this._cli, this._debugState, name, range, cwd, tomlUri);
      if (lens) lenses.push(lens);
    }

    return lenses;
  }
}

function buildLens(cli, debugState, name, range, cwd, tomlUri) {
  if (debugState.name === name) {
    const labels = {
      starting: "$(sync~spin) Starting",
      debugging: "$(debug-stop) Debugging",
    };
    const label = labels[debugState.status] || debugState.status;
    if (debugState.status === "debugging") {
      return new vscode.CodeLens(range, { title: label, command: "gaffer.stopDebug" });
    }
    return new vscode.CodeLens(range, { title: label });
  }

  if (!cli.hasCommand("dev") || !cli.hasFlag("dev", "debug")) return null;

  return new vscode.CodeLens(range, {
    title: "$(debug-start) Debug",
    command: "gaffer.debugProjection",
    arguments: [{ name, cwd, tomlUri }],
  });
}

function extractName(lines, startLine) {
  for (let i = startLine; i < lines.length && i < startLine + 10; i++) {
    const line = lines[i].trim();
    if (line.startsWith("[")) break;

    const match = line.match(/^name\s*=\s*(?:"([^"]+)"|'([^']+)')/);
    if (match) return match[1] ?? match[2];
  }
  return null;
}

module.exports = { TomlCodeLensProvider, buildLens };
