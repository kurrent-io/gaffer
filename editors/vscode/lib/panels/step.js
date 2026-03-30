const vscode = require("vscode");
const { jsonToTreeItems } = require("./json-tree");

class StepProvider {
  constructor() {
    this._items = [];
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
    this._items = [];
    if (this._refreshTimer) { clearTimeout(this._refreshTimer); this._refreshTimer = null; }
    this._onDidChange.fire();
  }

  startStep(event) {
    this._items = [buildInputItem(event)];
    this._onDidChange.fire();
  }

  addLog(message) {
    this._items.push(buildLogItem(message));
    this._scheduleRefresh();
  }

  addEmit(emitData) {
    this._items.push(buildEmitItem(emitData));
    this._scheduleRefresh();
  }

  setResult(result, position) {
    this._items.push(buildResultItem(result));
    this._scheduleRefresh();
  }

  setFull(event, result, position) {
    this._items = [buildInputItem(event)];
    if (result.logs) {
      for (const l of result.logs) this._items.push(buildLogItem(l));
    }
    if (result.emitted) {
      for (const em of result.emitted) this._items.push(buildEmitItem(em));
    }
    this._items.push(buildResultItem(result));
    this._onDidChange.fire();
  }

  setError(code, description) {
    const item = new vscode.TreeItem(code, vscode.TreeItemCollapsibleState.None);
    item.iconPath = new vscode.ThemeIcon("error", new vscode.ThemeColor("testing.iconFailed"));
    item.description = description;
    this._items.push(item);
    this._onDidChange.fire();
  }

  getTreeItem(element) {
    return element;
  }

  getChildren(element) {
    if (element) return element.children || [];
    return this._items;
  }
}

function buildInputItem(event) {
  const label = `${event.sequenceNumber}@${event.streamId}`;
  const item = new vscode.TreeItem(label, vscode.TreeItemCollapsibleState.Expanded);
  item.description = event.eventType;
  item.iconPath = new vscode.ThemeIcon("rocket");

  const children = [];
  children.push(leaf("type", event.eventType));
  children.push(leaf("stream", event.streamId));
  children.push(leaf("seq", String(event.sequenceNumber)));

  if (event.data) {
    const dataItem = new vscode.TreeItem("data", vscode.TreeItemCollapsibleState.Collapsed);
    dataItem.children = jsonToTreeItems(event.data);
    children.push(dataItem);
  }
  if (event.metadata) {
    const metaItem = new vscode.TreeItem("metadata", vscode.TreeItemCollapsibleState.Collapsed);
    metaItem.children = jsonToTreeItems(event.metadata);
    children.push(metaItem);
  }

  item.children = children;
  return item;
}

function buildLogItem(message) {
  const msg = typeof message === "string" ? message : message.message || String(message);
  const item = new vscode.TreeItem(msg, vscode.TreeItemCollapsibleState.None);
  item.iconPath = new vscode.ThemeIcon("comment");
  item.description = "";
  return item;
}

function buildEmitItem(em) {
  const isLink = em.isLink;
  const item = new vscode.TreeItem(em.streamId, vscode.TreeItemCollapsibleState.Collapsed);
  item.description = isLink ? "link" : em.eventType;
  item.iconPath = new vscode.ThemeIcon(isLink ? "link" : "forward");

  const children = [];
  if (!isLink && em.eventType) children.push(leaf("type", em.eventType));
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
  if (children.length === 0) item.collapsibleState = vscode.TreeItemCollapsibleState.None;
  return item;
}

function buildResultItem(result) {
  if (result.status === "processed") {
    const desc = result.partition ? `[${result.partition}]` : "";
    const item = new vscode.TreeItem("processed", result.state ? vscode.TreeItemCollapsibleState.Collapsed : vscode.TreeItemCollapsibleState.None);
    item.iconPath = new vscode.ThemeIcon("arrow-circle-right");
    item.description = desc;

    if (result.state) {
      const children = [];
      if (result.partition) children.push(leaf("partition", result.partition));
      const stateItem = new vscode.TreeItem("state", vscode.TreeItemCollapsibleState.Collapsed);
      stateItem.children = jsonToTreeItems(result.state);
      children.push(stateItem);
      item.children = children;
    }
    return item;
  }

  if (result.status === "skipped") {
    const item = new vscode.TreeItem("skipped", vscode.TreeItemCollapsibleState.None);
    item.iconPath = new vscode.ThemeIcon("circle-large");
    item.description = result.reason;
    return item;
  }

  const item = new vscode.TreeItem("error", vscode.TreeItemCollapsibleState.None);
  item.iconPath = new vscode.ThemeIcon("error", new vscode.ThemeColor("testing.iconFailed"));
  item.description = result.status;
  return item;
}

function leaf(label, value) {
  const item = new vscode.TreeItem(label, vscode.TreeItemCollapsibleState.None);
  item.description = value;
  return item;
}

module.exports = { StepProvider };
