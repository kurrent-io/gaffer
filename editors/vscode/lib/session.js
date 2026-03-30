const vscode = require("vscode");
const { GafferProcess } = require("./process");

class GafferSession {
  constructor(name, command, log) {
    this._name = name;
    this._command = command;
    this._log = log;
    this._proc = null;
    this._listeners = new Map();
    this._output = vscode.window.createOutputChannel(`Gaffer: ${name}`, "log");
  }

  get name() { return this._name; }
  get output() { return this._output; }

  on(type, fn) {
    if (!this._listeners.has(type)) {
      this._listeners.set(type, []);
    }
    this._listeners.get(type).push(fn);
    return this;
  }

  start() {
    this._proc = new GafferProcess(this._command, this._log);

    this._proc.onLine((msg) => {
      this._dispatch(msg);
    });

    this._proc.onExit((code) => {
      this._writeOutput(`Process exited (code ${code})`);
      this._dispatch({ type: "exit", code });
    });

    this._proc.start();
    return this;
  }

  async waitForDebug() {
    return this._proc.waitForMessage("debug");
  }

  stop() {
    if (this._proc) {
      this._proc.kill();
      this._proc = null;
    }
  }

  dispose() {
    this.stop();
    this._output.dispose();
    this._listeners.clear();
  }

  _dispatch(msg) {
    this._renderOutput(msg);

    const fns = this._listeners.get(msg.type);
    if (fns) {
      for (const fn of fns) fn(msg);
    }

    const allFns = this._listeners.get("*");
    if (allFns) {
      for (const fn of allFns) fn(msg);
    }
  }

  _renderOutput(msg) {
    switch (msg.type) {
      case "info":
        this._writeOutput(`${msg.projection.name}`);
        if (msg.projection.source) this._writeOutput(`  Source: ${msg.projection.source}`);
        if (msg.projection.partitioning) this._writeOutput(`  Partitioning: ${msg.projection.partitioning}`);
        if (msg.projection.events) this._writeOutput(`  Events: ${msg.projection.events.join(", ")}`);
        if (msg.projection.engine) this._writeOutput(`  Engine: ${msg.projection.engine}`);
        this._writeOutput("");
        break;
      case "event":
        this._writeOutput(`${msg.sequenceNumber}@${msg.streamId} ${msg.eventType}`);
        break;
      case "result":
        if (msg.status === "processed") {
          const partition = msg.partition ? ` [${msg.partition}]` : "";
          this._writeOutput(`  -> processed${partition}`);
          if (msg.logs?.length > 0) {
            for (const l of msg.logs) this._writeOutput(`  [log] ${l}`);
          }
        } else {
          this._writeOutput(`  -> ${msg.status}: ${msg.reason}`);
        }
        break;
      case "error":
        this._writeOutput(`  ERROR: ${msg.code} - ${msg.description}`);
        break;
      case "summary":
        this._writeOutput("");
        this._writeOutput(`Summary: ${msg.handled} handled, ${msg.skipped} skipped, ${msg.errors} errors`);
        break;
    }
  }

  _writeOutput(text) {
    this._output.appendLine(text);
  }
}

module.exports = { GafferSession };
