const vscode = require("vscode");
const { GafferCli } = require("./lib/cli");
const { GafferSession } = require("./lib/session");
const { ProjectIndex } = require("./lib/project");
const { TomlCodeLensProvider } = require("./lib/codelens-toml");
const { JsCodeLensProvider } = require("./lib/codelens-js");
const { EventStreamProvider } = require("./lib/panels/events");
const { StateProvider } = require("./lib/panels/state");

function activate(context) {
  const output = vscode.window.createOutputChannel("Gaffer");
  const log = (msg) => { output.appendLine(msg); console.log(`Gaffer: ${msg}`); };

  const cli = new GafferCli(log);
  const projectIndex = new ProjectIndex();
  const tomlCodeLens = new TomlCodeLensProvider(cli);
  const jsCodeLens = new JsCodeLensProvider(cli, projectIndex);

  const eventsProvider = new EventStreamProvider();
  const stateProvider = new StateProvider();
  let activeSession = null;

  context.subscriptions.push(
    vscode.window.registerTreeDataProvider("gaffer.events", eventsProvider),
    vscode.window.registerTreeDataProvider("gaffer.state", stateProvider)
  );

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

  function startSession(name, command) {
    if (activeSession) {
      activeSession.dispose();
    }

    eventsProvider.clear();
    stateProvider.clear();

    const session = new GafferSession(name, command, log);
    activeSession = session;

    session
      .on("event", (msg) => eventsProvider.addEvent(msg))
      .on("result", (msg) => {
        eventsProvider.addResult(msg);
        stateProvider.update(msg);
      })
      .on("error", (msg) => eventsProvider.addError(msg));

    session.start();
    return session;
  }

  context.subscriptions.push(
    vscode.commands.registerCommand("gaffer.runProjection", (args) => {
      const { name } = args;
      const command = cli.buildCommand(`dev ${name} --json`);
      startSession(name, command);
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand("gaffer.debugProjection", async (args) => {
      const { name, tomlUri } = args;
      const port = 4711;
      const command = cli.buildCommand(`dev ${name} --json --debug --debug-port ${port}`);

      log(`Starting debug: ${name}`);
      const session = startSession(name, command);

      let debugPort;
      try {
        const msg = await session.waitForDebug();
        debugPort = msg.port;
        log(`Debug server listening on port ${debugPort}`);
      } catch (err) {
        log(`Failed to start debug: ${err.message}`);
        vscode.window.showErrorMessage(`Gaffer: ${err.message}`);
        session.dispose();
        activeSession = null;
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
        session.dispose();
        activeSession = null;
        return;
      }

      const disposable = vscode.debug.onDidTerminateDebugSession((dbgSession) => {
        if (dbgSession.name === `Gaffer: ${name}`) {
          log("Debug session ended, stopping CLI");
          session.dispose();
          if (activeSession === session) activeSession = null;
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
