const vscode = require("vscode");
const { jsonToTreeItems } = require("./json-tree");

class StateProvider {
  constructor() {
    this._state = null;
    this._debugSession = null;
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
    this._debugSession = null;
    if (this._refreshTimer) { clearTimeout(this._refreshTimer); this._refreshTimer = null; }
    this._onDidChange.fire();
  }

  setDebugSession(session) {
    this._debugSession = session;
  }

  updateFromState(stateMsg) {
    this._state = stateMsg;
    this._scheduleRefresh();
  }

  getTreeItem(element) {
    return element;
  }

  getChildren(element) {
    if (element) {
      if (element.contextValue === "partition") {
        if (!this._debugSession) {
          return [new vscode.TreeItem("No active session", vscode.TreeItemCollapsibleState.None)];
        }
        return this._fetchPartitionState(element.label);
      }
      return element.children || [];
    }
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
        items.push(partItem);
      }
    }

    return items;
  }

  async _fetchPartitionState(partition) {
    try {
      const resp = await this._debugSession.customRequest("gaffer/partitionState", { partition });
      const body = resp;
      const items = [];

      if (body.state) {
        const stateItem = new vscode.TreeItem("state", vscode.TreeItemCollapsibleState.Expanded);
        stateItem.children = jsonToTreeItems(body.state);
        items.push(stateItem);
      }
      if (body.result) {
        const resultItem = new vscode.TreeItem("result", vscode.TreeItemCollapsibleState.Expanded);
        resultItem.children = jsonToTreeItems(body.result);
        items.push(resultItem);
      }

      if (items.length === 0) {
        items.push(new vscode.TreeItem("(empty)", vscode.TreeItemCollapsibleState.None));
      }

      return items;
    } catch {
      return [new vscode.TreeItem("Failed to load", vscode.TreeItemCollapsibleState.None)];
    }
  }
}

module.exports = { StateProvider };
