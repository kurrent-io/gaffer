const vscode = require("vscode");
const { jsonToTreeItems } = require("./json-tree");

class StateProvider {
  constructor() {
    this._state = null;
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
    this._state = null;
    if (this._refreshTimer) { clearTimeout(this._refreshTimer); this._refreshTimer = null; }
    this._onDidChange.fire();
  }

  updateFromState(stateMsg) {
    this._state = stateMsg;
    this._scheduleRefresh();
  }

  getTreeItem(element) {
    return element;
  }

  getChildren(element) {
    if (element) return element.children || [];
    if (!this._state) {
      const empty = new vscode.TreeItem("No state yet", vscode.TreeItemCollapsibleState.None);
      empty.iconPath = new vscode.ThemeIcon("info");
      return [empty];
    }

    const items = [];
    const s = this._state;

    if (s.state) {
      const stateItem = new vscode.TreeItem("state", vscode.TreeItemCollapsibleState.Expanded);
      stateItem.iconPath = new vscode.ThemeIcon("symbol-variable");
      stateItem.children = jsonToTreeItems(s.state);
      items.push(stateItem);
    }

    if (s.result) {
      const resultItem = new vscode.TreeItem("result", vscode.TreeItemCollapsibleState.Expanded);
      resultItem.iconPath = new vscode.ThemeIcon("symbol-variable");
      resultItem.children = jsonToTreeItems(s.result);
      items.push(resultItem);
    }

    if (s.sharedState) {
      const sharedItem = new vscode.TreeItem("shared state", vscode.TreeItemCollapsibleState.Expanded);
      sharedItem.iconPath = new vscode.ThemeIcon("symbol-variable");
      sharedItem.children = jsonToTreeItems(s.sharedState);
      items.push(sharedItem);
    }

    if (s.partitions?.length > 0) {
      for (const name of s.partitions) {
        const partItem = new vscode.TreeItem(name, vscode.TreeItemCollapsibleState.Collapsed);
        partItem.iconPath = new vscode.ThemeIcon("symbol-namespace");
        partItem.contextValue = "partition";
        partItem.children = [
          new vscode.TreeItem("Loading...", vscode.TreeItemCollapsibleState.None),
        ];
        items.push(partItem);
      }
    }

    return items;
  }
}

module.exports = { StateProvider };
