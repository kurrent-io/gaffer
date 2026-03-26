const vscode = require("vscode");

function activate(context) {
  context.subscriptions.push(
    vscode.debug.registerDebugAdapterDescriptorFactory("gaffer", {
      createDebugAdapterDescriptor(session) {
        const config = session.configuration;
        const port = config.port || 4711;
        return new vscode.DebugAdapterServer(port);
      },
    })
  );
}

function deactivate() {}

module.exports = { activate, deactivate };
