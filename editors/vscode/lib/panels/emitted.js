const vscode = require("vscode");
const { jsonToTreeItems } = require("./json-tree");

class EmittedProvider {
  constructor() {
    this._emitted = [];
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
    this._emitted = [];
    if (this._refreshTimer) { clearTimeout(this._refreshTimer); this._refreshTimer = null; }
    this._onDidChange.fire();
  }

  addFromResult(resultMsg) {
    if (resultMsg.status !== "processed") return;
    if (!resultMsg.emitted || resultMsg.emitted.length === 0) return;

    for (const em of resultMsg.emitted) {
      this._emitted.push({ ...em, eventId: resultMsg.eventId });
    }
    this._scheduleRefresh();
  }

  getTreeItem(element) {
    return element;
  }

  getChildren(element) {
    if (element) {
      return element.children || [];
    }

    if (this._emitted.length === 0) return [];

    return this._emitted.map((em) => {
      const label = em.streamId;
      const isLink = em.isLink;
      const item = new vscode.TreeItem(label, vscode.TreeItemCollapsibleState.Collapsed);
      item.description = isLink ? `link` : em.eventType;
      item.iconPath = new vscode.ThemeIcon(isLink ? "link" : "arrow-right");

      const children = [];

      if (!isLink && em.eventType) {
        const typeItem = new vscode.TreeItem("type", vscode.TreeItemCollapsibleState.None);
        typeItem.description = em.eventType;
        children.push(typeItem);
      }

      const fromItem = new vscode.TreeItem("from", vscode.TreeItemCollapsibleState.None);
      fromItem.description = em.eventId;
      children.push(fromItem);

      if (em.data) {
        const dataItem = new vscode.TreeItem("data", vscode.TreeItemCollapsibleState.Collapsed);
        dataItem.children = jsonToTreeItems(em.data);
        children.push(dataItem);
      }

      if (em.metadata) {
        const metaItem = new vscode.TreeItem("metadata", vscode.TreeItemCollapsibleState.Collapsed);
        metaItem.children = jsonToTreeItems(em.metadata);
        children.push(metaItem);
      }

      item.children = children;
      return item;
    }).reverse();
  }
}

module.exports = { EmittedProvider };
