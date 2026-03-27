const vscode = require("vscode");
const path = require("path");

const fromPattern = /^(fromAll|fromStream|fromCategory|fromStreams)\s*\(/;

class JsCodeLensProvider {
  constructor(cli, projectIndex) {
    this._cli = cli;
    this._projectIndex = projectIndex;
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
    const args = { name, cwd: tomlDir, tomlUri };
    const lenses = [];

    if (this._cli.hasCommand("dev")) {
      lenses.push(
        new vscode.CodeLens(range, {
          title: "\u25b6 Run",
          command: "gaffer.runProjection",
          arguments: [args],
        })
      );

      if (this._cli.hasFlag("dev", "debug")) {
        lenses.push(
          new vscode.CodeLens(range, {
            title: "\ud83d\udd0d Debug",
            command: "gaffer.debugProjection",
            arguments: [args],
          })
        );
      }
    }

    return lenses;
  }
}

module.exports = { JsCodeLensProvider };
