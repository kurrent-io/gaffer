const vscode = require("vscode");

function jsonToTreeItems(value) {
  if (value === null || value === undefined) return [];
  if (typeof value !== "object") {
    return [new vscode.TreeItem(String(value), vscode.TreeItemCollapsibleState.None)];
  }
  if (Array.isArray(value)) {
    return value.map((v, i) => {
      if (typeof v === "object" && v !== null) {
        const item = new vscode.TreeItem(`[${i}]`, vscode.TreeItemCollapsibleState.Collapsed);
        item.children = jsonToTreeItems(v);
        return item;
      }
      const item = new vscode.TreeItem(`[${i}]`, vscode.TreeItemCollapsibleState.None);
      item.description = String(v);
      return item;
    });
  }
  return Object.entries(value).map(([key, val]) => {
    if (typeof val === "object" && val !== null) {
      const item = new vscode.TreeItem(key, vscode.TreeItemCollapsibleState.Collapsed);
      item.children = jsonToTreeItems(val);
      return item;
    }
    const item = new vscode.TreeItem(key, vscode.TreeItemCollapsibleState.None);
    item.description = String(val);
    return item;
  });
}

module.exports = { jsonToTreeItems };
