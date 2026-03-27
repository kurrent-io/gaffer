const vscode = require("vscode");
const { exec } = require("child_process");

class GafferCli {
  constructor(log) {
    this._manifest = null;
    this._log = log || (() => {});
  }

  get manifest() {
    return this._manifest;
  }

  hasCommand(name) {
    return this._manifest?.commands?.[name] != null;
  }

  hasFlag(command, flag) {
    return this._manifest?.commands?.[command]?.flags?.includes(flag) ?? false;
  }

  buildCommand(args) {
    const template = vscode.workspace.getConfiguration("gaffer").get("command", "gaffer");
    if (template.includes("{command}")) {
      return template.replace("{command}", args);
    }
    return `${template} ${args}`;
  }

  async fetchManifest() {
    const command = this.buildCommand("manifest");
    try {
      const output = await execAsync(command);
      this._manifest = JSON.parse(output);
      this._log(`Manifest loaded (v${this._manifest.version})`);
      return this._manifest;
    } catch (err) {
      this._log(`Manifest fetch failed: ${err.message}`);
      this._manifest = null;
      throw err;
    }
  }
}

function execAsync(command, options = {}) {
  return new Promise((resolve, reject) => {
    const shell = process.env.SHELL;
    const shellCmd = shell ? `${shell} -i -c ${JSON.stringify(command)}` : command;
    exec(shellCmd, { ...options, timeout: 10000 }, (err, stdout, stderr) => {
      if (err) {
        reject(new Error(`${err.message}${stderr ? ` (stderr: ${stderr.trim()})` : ""}`));
      } else {
        resolve(stdout);
      }
    });
  });
}

module.exports = { GafferCli };
