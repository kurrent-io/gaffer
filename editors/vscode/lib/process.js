const { spawn } = require("child_process");
const readline = require("readline");

class GafferProcess {
  constructor(command, options = {}) {
    this._log = options.log || (() => {});
    this._proc = null;
    this._onLine = () => {};
    this._onExit = () => {};
    this._command = command;
    this._cwd = options.cwd;
  }

  start() {
    const shell = process.env.SHELL || "/bin/sh";
    this._log(`Spawning: ${shell} -i -c ${JSON.stringify(this._command)}${this._cwd ? ` (cwd: ${this._cwd})` : ""}`);

    this._proc = spawn(shell, ["-i", "-c", this._command], {
      stdio: ["ignore", "pipe", "pipe"],
      cwd: this._cwd,
    });

    const rl = readline.createInterface({ input: this._proc.stdout });
    rl.on("line", (line) => {
      try {
        const msg = JSON.parse(line);
        this._onLine(msg);
      } catch {
        this._log(`[stdout] ${line}`);
      }
    });

    this._proc.stderr.on("data", (data) => {
      const text = stripAnsi(data.toString()).trim();
      if (text) this._log(`[stderr] ${text}`);
    });

    this._proc.on("exit", (code) => {
      this._log(`Process exited with code ${code}`);
      this._onExit(code);
    });

    return this;
  }

  onLine(fn) {
    this._onLine = fn;
    return this;
  }

  onExit(fn) {
    this._onExit = fn;
    return this;
  }

  waitForMessage(type, timeoutMs = 15000) {
    return new Promise((resolve, reject) => {
      const prev = this._onLine;
      const prevExit = this._onExit;

      function cleanup() {
        clearTimeout(timer);
      }

      const timer = setTimeout(() => {
        this._onLine = prev;
        this._onExit = prevExit;
        reject(new Error(`Timeout waiting for "${type}" message`));
      }, timeoutMs);

      this._onExit = (code) => {
        cleanup();
        this._onLine = prev;
        this._onExit = prevExit;
        prevExit(code);
        reject(new Error(`Process exited (code ${code}) before "${type}" message`));
      };

      this._onLine = (msg) => {
        prev(msg);
        if (msg.type === type) {
          cleanup();
          this._onLine = prev;
          this._onExit = prevExit;
          resolve(msg);
        }
      };
    });
  }

  kill() {
    if (this._proc && !this._proc.killed) {
      this._proc.kill();
    }
  }
}

// eslint-disable-next-line no-control-regex
const ansiRegex = /\x1b\[[0-9;]*m/g;
function stripAnsi(str) { return str.replace(ansiRegex, ""); }

module.exports = { GafferProcess };
