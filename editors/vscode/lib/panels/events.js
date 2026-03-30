const vscode = require("vscode");
const { jsonToTreeItems } = require("./json-tree");

class EventStreamProvider {
  constructor() {
    this._events = [];
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
    this._events = [];
    if (this._refreshTimer) { clearTimeout(this._refreshTimer); this._refreshTimer = null; }
    this._onDidChange.fire();
  }

  addEvent(eventMsg) {
    this._events.push({ event: eventMsg, result: null });
    this._scheduleRefresh();
  }

  addResult(resultMsg) {
    const entry = this._events.find((e) => e.event.id === resultMsg.eventId);
    if (entry) {
      entry.result = resultMsg;
      this._scheduleRefresh();
    }
  }

  addError(errorMsg) {
    const entry = this._events.find((e) => e.event.id === errorMsg.eventId);
    if (entry) {
      entry.result = { status: "error", code: errorMsg.code, description: errorMsg.description };
      this._scheduleRefresh();
    }
  }

  getTreeItem(element) {
    return element;
  }

  getChildren(element) {
    if (element) {
      return element.children || [];
    }

    return this._events.map((entry, i) => {
      const e = entry.event;
      const label = `${e.sequenceNumber}@${e.streamId}`;
      const item = new vscode.TreeItem(label, vscode.TreeItemCollapsibleState.Collapsed);
      item.description = e.eventType;

      if (!entry.result) {
        item.iconPath = new vscode.ThemeIcon("sync~spin");
      } else if (entry.result.status === "processed") {
        item.iconPath = new vscode.ThemeIcon("pass", new vscode.ThemeColor("testing.iconPassed"));
      } else if (entry.result.status === "error") {
        item.iconPath = new vscode.ThemeIcon("error", new vscode.ThemeColor("testing.iconFailed"));
        item.tooltip = `${entry.result.code}: ${entry.result.description}`;
      } else {
        item.iconPath = new vscode.ThemeIcon("circle-outline");
        item.tooltip = entry.result.reason;
      }

      const children = [];

      if (e.data) {
        const dataItem = new vscode.TreeItem("data", vscode.TreeItemCollapsibleState.Collapsed);
        dataItem.children = jsonToTreeItems(e.data);
        children.push(dataItem);
      }

      if (entry.result?.status === "processed" && entry.result.state) {
        const stateItem = new vscode.TreeItem("state", vscode.TreeItemCollapsibleState.Collapsed);
        stateItem.children = jsonToTreeItems(entry.result.state);
        children.push(stateItem);
      }

      if (entry.result?.status === "processed" && entry.result.emitted?.length > 0) {
        const emittedItem = new vscode.TreeItem(`emitted (${entry.result.emitted.length})`, vscode.TreeItemCollapsibleState.Collapsed);
        emittedItem.children = entry.result.emitted.map((em) => {
          const emItem = new vscode.TreeItem(`${em.streamId}`, vscode.TreeItemCollapsibleState.None);
          emItem.description = em.eventType;
          emItem.iconPath = new vscode.ThemeIcon(em.isLink ? "link" : "arrow-right");
          return emItem;
        });
        children.push(emittedItem);
      }

      if (entry.result?.logs?.length > 0) {
        for (const l of entry.result.logs) {
          const logItem = new vscode.TreeItem(l, vscode.TreeItemCollapsibleState.None);
          logItem.iconPath = new vscode.ThemeIcon("output");
          children.push(logItem);
        }
      }

      item.children = children;
      if (children.length === 0) {
        item.collapsibleState = vscode.TreeItemCollapsibleState.None;
      }

      return item;
    }).reverse();
  }
}

module.exports = { EventStreamProvider };
