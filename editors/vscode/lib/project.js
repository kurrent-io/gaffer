const vscode = require("vscode");
const path = require("path");
const fs = require("fs");

class ProjectIndex {
  constructor() {
    this._entries = new Map();
  }

  refresh() {
    this._entries.clear();

    const tomlFiles = vscode.workspace.findFiles("**/gaffer.toml", "**/node_modules/**");
    return tomlFiles.then((uris) => {
      for (const uri of uris) {
        const tomlDir = path.dirname(uri.fsPath);
        const projections = parseProjections(uri.fsPath);
        for (const proj of projections) {
          const absEntry = path.resolve(tomlDir, proj.entry);
          this._entries.set(absEntry, { name: proj.name, tomlDir });
        }
      }
    });
  }

  lookup(filePath) {
    return this._entries.get(filePath) ?? null;
  }

  get entryPaths() {
    return [...this._entries.keys()];
  }
}

function parseProjections(tomlPath) {
  const projections = [];
  try {
    const text = fs.readFileSync(tomlPath, "utf8");
    const lines = text.split("\n");

    let current = null;
    for (const line of lines) {
      const trimmed = line.trim();
      if (trimmed === "[[projection]]") {
        if (current) projections.push(current);
        current = {};
        continue;
      }
      if (!current) continue;
      if (trimmed.startsWith("[")) {
        projections.push(current);
        current = null;
        continue;
      }

      const nameMatch = trimmed.match(/^name\s*=\s*(?:"([^"]+)"|'([^']+)')/);
      if (nameMatch) current.name = nameMatch[1] ?? nameMatch[2];

      const entryMatch = trimmed.match(/^entry\s*=\s*(?:"([^"]+)"|'([^']+)')/);
      if (entryMatch) current.entry = entryMatch[1] ?? entryMatch[2];
    }
    if (current) projections.push(current);
  } catch {
    // ignore read errors
  }
  return projections.filter((p) => p.name && p.entry);
}

module.exports = { ProjectIndex };
