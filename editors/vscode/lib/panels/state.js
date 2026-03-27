const vscode = require("vscode");
const { jsonToTreeItems } = require("./json-tree");

class StateProvider {
  constructor() {
    this._partitions = new Map();
    this._onDidChange = new vscode.EventEmitter();
    this.onDidChangeTreeData = this._onDidChange.event;
    this._refreshTimer = null;
  }

  _scheduleRefresh() {
    if (this._refreshTimer) return;
    this._refreshTimer = setTimeout(() => {
      this._refreshTimer = null;
      this._onDidChange.fire();
    }, 50);
  }

  clear() {
    this._partitions.clear();
    this._onDidChange.fire();
  }

  update(resultMsg) {
    if (resultMsg.status !== "processed") return;
    if (resultMsg.state == null) return;

    const partition = resultMsg.partition || "(root)";
    this._partitions.set(partition, resultMsg.state);
    this._scheduleRefresh();
  }

  getTreeItem(element) {
    return element;
  }

  getChildren(element) {
    if (element) {
      return element.children || [];
    }

    if (this._partitions.size === 0) {
      const empty = new vscode.TreeItem("No state yet", vscode.TreeItemCollapsibleState.None);
      empty.iconPath = new vscode.ThemeIcon("info");
      return [empty];
    }

    if (this._partitions.size === 1 && this._partitions.has("(root)")) {
      return jsonToTreeItems(this._partitions.get("(root)"));
    }

    return [...this._partitions.entries()].map(([name, state]) => {
      const item = new vscode.TreeItem(name, vscode.TreeItemCollapsibleState.Collapsed);
      item.children = jsonToTreeItems(state);
      item.iconPath = new vscode.ThemeIcon("symbol-namespace");
      return item;
    });
  }
}

module.exports = { StateProvider };
