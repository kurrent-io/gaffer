const vscode = require("vscode");
const { GafferCli } = require("./lib/cli");
const { GafferProcess } = require("./lib/process");
const { ProjectIndex } = require("./lib/project");
const { TomlCodeLensProvider } = require("./lib/codelens-toml");
const { JsCodeLensProvider } = require("./lib/codelens-js");

function activate(context) {
  const output = vscode.window.createOutputChannel("Gaffer");
  const log = (msg) => { output.appendLine(msg); console.log(`Gaffer: ${msg}`); };

  const cli = new GafferCli(log);
  const projectIndex = new ProjectIndex();
  const tomlCodeLens = new TomlCodeLensProvider(cli);
  const jsCodeLens = new JsCodeLensProvider(cli, projectIndex);

  context.subscriptions.push(
    vscode.debug.registerDebugAdapterDescriptorFactory("gaffer", {
      createDebugAdapterDescriptor(session) {
        const config = session.configuration;
        const port = config.port || 4711;
        return new vscode.DebugAdapterServer(port);
      },
    })
  );

  context.subscriptions.push(
    vscode.languages.registerCodeLensProvider(
      { pattern: "**/gaffer.toml" },
      tomlCodeLens
    )
  );

  context.subscriptions.push(
    vscode.languages.registerCodeLensProvider(
      { language: "javascript" },
      jsCodeLens
    )
  );

  context.subscriptions.push(
    vscode.commands.registerCommand("gaffer.runProjection", (args) => {
      const { name, cwd } = args;
      const command = cli.buildCommand(`dev ${name}`);
      const terminal = vscode.window.createTerminal({ name: `Gaffer: ${name}`, cwd });
      terminal.show();
      terminal.sendText(command);
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand("gaffer.debugProjection", async (args) => {
      const { name, tomlUri } = args;
      const port = 4711;
      const command = cli.buildCommand(`dev ${name} --json --debug --debug-port ${port}`);

      log(`Starting debug: ${name}`);
      const proc = new GafferProcess(command, log);
      proc.start();

      proc.onLine((msg) => {
        log(`[json] ${JSON.stringify(msg)}`);
      });

      let debugPort;
      try {
        const msg = await proc.waitForMessage("debug");
        debugPort = msg.port;
        log(`Debug server listening on port ${debugPort}`);
      } catch (err) {
        log(`Failed to start debug: ${err.message}`);
        vscode.window.showErrorMessage(`Gaffer: ${err.message}`);
        proc.kill();
        return;
      }

      const tomlDir = vscode.Uri.joinPath(tomlUri, "..").fsPath;
      const started = await vscode.debug.startDebugging(
        vscode.workspace.getWorkspaceFolder(tomlUri),
        {
          type: "gaffer",
          request: "attach",
          name: `Gaffer: ${name}`,
          port: debugPort,
          localRoot: tomlDir,
        }
      );

      if (!started) {
        log("Debug session failed to start");
        proc.kill();
        return;
      }

      const disposable = vscode.debug.onDidTerminateDebugSession((session) => {
        if (session.name === `Gaffer: ${name}`) {
          log("Debug session ended, stopping CLI");
          proc.kill();
          disposable.dispose();
        }
      });
      context.subscriptions.push(disposable);
    })
  );

  function refreshAll() {
    tomlCodeLens.refresh();
    projectIndex.refresh().then(() => jsCodeLens.refresh());
  }

  const tomlWatcher = vscode.workspace.createFileSystemWatcher("**/gaffer.toml");
  tomlWatcher.onDidChange(() => { log("gaffer.toml changed"); refreshAll(); });
  tomlWatcher.onDidCreate(() => {
    log("gaffer.toml created");
    cli.fetchManifest().then(() => refreshAll()).catch(() => {});
  });
  tomlWatcher.onDidDelete(() => { log("gaffer.toml deleted"); refreshAll(); });
  context.subscriptions.push(tomlWatcher);

  vscode.workspace.onDidChangeConfiguration((e) => {
    if (e.affectsConfiguration("gaffer.command")) {
      log("gaffer.command setting changed, refetching manifest");
      cli.fetchManifest().then(() => refreshAll()).catch(() => {});
    }
  }, null, context.subscriptions);

  projectIndex.refresh().then(() => {
    cli.fetchManifest().then(() => refreshAll()).catch(() => {
      vscode.window.showWarningMessage(
        "Gaffer CLI not found. Install gaffer or configure \"gaffer.command\" in settings.",
        "Open Settings"
      ).then((choice) => {
        if (choice === "Open Settings") {
          vscode.commands.executeCommand("workbench.action.openSettings", "gaffer.command");
        }
      });
    });
  });
}

function deactivate() {}

module.exports = { activate, deactivate };
