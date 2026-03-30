const vscode = require("vscode");
const { GafferCli } = require("./lib/cli");
const { GafferSession } = require("./lib/session");
const { ProjectIndex } = require("./lib/project");
const { TomlCodeLensProvider } = require("./lib/codelens-toml");
const { JsCodeLensProvider } = require("./lib/codelens-js");
const { EventStreamProvider } = require("./lib/panels/events");
const { StateProvider } = require("./lib/panels/state");
const { EmittedProvider } = require("./lib/panels/emitted");

function activate(context) {
  const output = vscode.window.createOutputChannel("Gaffer");
  const log = (msg) => { output.appendLine(msg); console.log(`Gaffer: ${msg}`); };

  const cli = new GafferCli(log);
  const projectIndex = new ProjectIndex();

  const debugState = {
    name: null,
    status: "idle",
  };

  function setDebugState(name, status) {
    debugState.name = name;
    debugState.status = status;
    tomlCodeLens.refresh();
    jsCodeLens.refresh();
  }

  const tomlCodeLens = new TomlCodeLensProvider(cli, debugState);
  const jsCodeLens = new JsCodeLensProvider(cli, projectIndex, debugState);

  const eventsProvider = new EventStreamProvider();
  const stateProvider = new StateProvider();
  const emittedProvider = new EmittedProvider();
  let activeSession = null;

  context.subscriptions.push(
    vscode.window.registerTreeDataProvider("gaffer.events", eventsProvider),
    vscode.window.registerTreeDataProvider("gaffer.state", stateProvider),
    vscode.window.registerTreeDataProvider("gaffer.emitted", emittedProvider)
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

  // Handle custom DAP events from the CLI
  context.subscriptions.push(
    vscode.debug.onDidReceiveDebugSessionCustomEvent((e) => {
      if (e.session.type !== "gaffer") return;

      if (e.event === "gaffer/step") {
        const { event, result } = e.body;
        const id = `${event.sequenceNumber}@${event.streamId}`;
        eventsProvider.addEvent({ ...event, id });
        eventsProvider.addResult({ ...result, eventId: id });
        stateProvider.update(result);
        emittedProvider.addFromResult({ ...result, eventId: id });
      }
    })
  );

  function stopSession() {
    if (!activeSession) return;
    vscode.debug.stopDebugging();
    activeSession.dispose();
    activeSession = null;
    setDebugState(null, "idle");
  }

  context.subscriptions.push(
    vscode.commands.registerCommand("gaffer.stopDebug", stopSession)
  );

  context.subscriptions.push(
    vscode.commands.registerCommand("gaffer.debugProjection", async (args) => {
      const { name, tomlUri } = args;
      const port = 4711;
      const command = cli.buildCommand(`dev ${name} --json --debug --debug-port ${port}`);

      if (activeSession) {
        activeSession.dispose();
        activeSession = null;
      }

      eventsProvider.clear();
      stateProvider.clear();
      emittedProvider.clear();

      setDebugState(name, "starting");
      log(`Starting: ${name}`);
      const session = new GafferSession(name, command, log);
      activeSession = session;

      session.start();
      vscode.commands.executeCommand("gaffer.events.focus");

      let debugPort;
      try {
        const msg = await session.waitForDebug();
        debugPort = msg.port;
        log(`Debug server listening on port ${debugPort}`);
      } catch (err) {
        log(`Failed to start: ${err.message}`);
        vscode.window.showErrorMessage(`Gaffer: ${err.message}`);
        session.dispose();
        activeSession = null;
        setDebugState(null, "idle");
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
          internalConsoleOptions: "neverOpen",
        }
      );

      if (!started) {
        log("Debug session failed to start");
        session.dispose();
        activeSession = null;
        setDebugState(null, "idle");
        return;
      }

      setDebugState(name, "debugging");

      const disposable = vscode.debug.onDidTerminateDebugSession((dbgSession) => {
        if (dbgSession.name === `Gaffer: ${name}`) {
          log("Debug session ended");
          session.dispose();
          if (activeSession === session) activeSession = null;
          setDebugState(null, "idle");
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
