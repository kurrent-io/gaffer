const vscode = require("vscode");
const { GafferCli } = require("./lib/cli");
const { GafferSession } = require("./lib/session");
const { ProjectIndex } = require("./lib/project");
const { TomlCodeLensProvider } = require("./lib/codelens-toml");
const { JsCodeLensProvider } = require("./lib/codelens-js");
const { StepProvider } = require("./lib/panels/step");
const { StateProvider } = require("./lib/panels/state");
const { StatusViewProvider } = require("./lib/panels/status");

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

  const stepProvider = new StepProvider();
  const stateProvider = new StateProvider();
  const statusProvider = new StatusViewProvider();
  let activeSession = null;

  function setSessionActive(active) {
    vscode.commands.executeCommand("setContext", "gaffer.sessionActive", active);
  }
  function setInspecting(inspecting) {
    vscode.commands.executeCommand("setContext", "gaffer.inspecting", inspecting);
  }

  context.subscriptions.push(
    vscode.window.registerTreeDataProvider("gaffer.step", stepProvider),
    vscode.window.registerTreeDataProvider("gaffer.state", stateProvider),
    vscode.window.registerWebviewViewProvider("gaffer.status", statusProvider)
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
      stateProvider.setDebugSession(e.session);

      switch (e.event) {
        case "gaffer/stepStart":
          stepProvider.startStep(e.body.event);
          break;
        case "gaffer/stepLog":
          stepProvider.addLog(e.body.message);
          break;
        case "gaffer/stepEmit":
          stepProvider.addEmit(e.body);
          break;
        case "gaffer/stepResult":
          stepProvider.setResult(e.body.result, e.body.position);
          break;
        case "gaffer/stepError":
          stepProvider.setError(e.body.code, e.body.description);
          vscode.window.showErrorMessage(`Gaffer: ${e.body.code} - ${e.body.description}`);
          break;
        case "gaffer/state":
          stateProvider.updateFromState(e.body);
          break;
        case "gaffer/mode":
          setInspecting(e.body.mode === "inspect");
          break;
      }
    })
  );

  function stopSession() {
    if (!activeSession) return;
    vscode.debug.stopDebugging();
    activeSession.dispose();
    activeSession = null;
    setDebugState(null, "idle");
    setSessionActive(false);
    setInspecting(false);
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

      stepProvider.clear();
      stateProvider.clear();

      setDebugState(name, "starting");
      log(`Starting: ${name}`);
      const session = new GafferSession(name, command, log);
      activeSession = session;

      session.on("exit", (msg) => {
        if (msg.code !== 0 && activeSession === session) {
          log(`CLI exited with code ${msg.code}`);
          vscode.window.showErrorMessage(`Gaffer: projection faulted (exit code ${msg.code})`);
          setDebugState(null, "idle");
          setSessionActive(false);
          setInspecting(false);
          activeSession = null;
        }
      });

      session
        .on("result", (msg) => {
          if (msg.status === "processed") statusProvider.addProcessed();
          else if (msg.status === "skipped") statusProvider.addSkipped();
        })
        .on("error", () => statusProvider.addError());

      session.start();
      statusProvider.setName(name);
      setSessionActive(true);
      setInspecting(false);
      vscode.commands.executeCommand("gaffer.status.focus");

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
          setSessionActive(false);
          setInspecting(false);
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
