const vscode = require("vscode");

class TomlCodeLensProvider {
  constructor(cli) {
    this._cli = cli;
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
      const args = { name, cwd, tomlUri };

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
    }

    return lenses;
  }
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

module.exports = { TomlCodeLensProvider };
