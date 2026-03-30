const vscode = require("vscode");
const path = require("path");
const { buildLens } = require("./codelens-toml");

const fromPattern = /^(fromAll|fromStream|fromCategory|fromStreams)\s*\(/;

class JsCodeLensProvider {
  constructor(cli, projectIndex, debugState) {
    this._cli = cli;
    this._projectIndex = projectIndex;
    this._debugState = debugState;
    this._onDidChange = new vscode.EventEmitter();
    this.onDidChangeCodeLenses = this._onDidChange.event;
  }

  refresh() {
    this._onDidChange.fire();
  }

  provideCodeLenses(document) {
    const resolved = this._projectIndex.lookup(document.uri.fsPath);
    if (!resolved) return [];

    const lines = document.getText().split("\n");
    let fromLine = -1;
    for (let i = 0; i < lines.length && i < 20; i++) {
      if (fromPattern.test(lines[i].trim())) {
        fromLine = i;
        break;
      }
    }
    if (fromLine === -1) return [];

    const { name, tomlDir } = resolved;
    const range = new vscode.Range(fromLine, 0, fromLine, lines[fromLine].length);
    const tomlUri = vscode.Uri.file(path.join(tomlDir, "gaffer.toml"));
    const lens = buildLens(this._cli, this._debugState, name, range, tomlDir, tomlUri);
    return lens ? [lens] : [];
  }
}

module.exports = { JsCodeLensProvider };
