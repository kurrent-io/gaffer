import * as vscode from "vscode";
import {
	buildGafferArgv,
	gafferSpawnEnv,
	tryFetchManifest,
} from "./discovery/cli.js";
import type { Manifest } from "./discovery/schemas.js";
import { LspCodeLensProvider } from "./lsp/lens-provider.js";
import { StepProvider } from "./panels/step.js";
import { StateProvider } from "./panels/state.js";
import { StatusViewProvider } from "./panels/status.js";
import { dispatchDapEvent } from "./debugging/dap-dispatch.js";
import { PausePendingTrackerFactory } from "./debugging/pause-pending-tracker.js";
import { PhaseTracker } from "./debugging/phase-tracker.js";
import { RestartTrackerFactory } from "./debugging/restart-tracker.js";
import {
	SessionController,
	type DebugProjectionArgs,
} from "./debugging/session-controller.js";
import { initOutput, log } from "./output.js";
import {
	DismissDiagnosticActionProvider,
	clearDiagnosticsForUri,
	initDiagnostics,
} from "./diagnostics.js";
import { showManifestFailure } from "./notifications/cli.js";
import {
	clearInstallPromptDismissed,
	isInstallPromptDismissed,
	runNpmInstall,
	showCliNotFoundPrompt,
} from "./notifications/install-prompt.js";
import {
	openTelemetryDisclosurePage,
	showTelemetryDisclosure,
} from "./notifications/telemetry.js";
import type { ExtensionActivatedProperties } from "@kurrent/gaffer-telemetry";
import { bucketCliVersion, bucketDuration } from "./telemetry/buckets.js";
import { loadSafe } from "./telemetry/config.js";
import { detectEditor } from "./telemetry/editor.js";
import { createTelemetry, type Telemetry } from "./telemetry/facade.js";
import { classifyManifestError } from "./telemetry/manifest-error.js";
import { runFirstRunNotice } from "./telemetry/notice.js";
import { checkOptOut } from "./telemetry/opt-out.js";
import { readVscodeTelemetryLevel } from "./telemetry/vscode-config.js";
import { wrapAsync } from "./telemetry/wrap.js";
import {
	wrapCodeActionProvider,
	wrapCodeLensProvider,
	wrapMcpServerDefinitionProvider,
	wrapTreeDataProvider,
	wrapWebviewViewProvider,
} from "./telemetry/wrap-provider.js";
import {
	retryStartLanguageClient,
	startLanguageClient,
	stopLanguageClient,
} from "./lsp/client.js";
import { registerTypeScriptPlugin } from "./lsp/typescript-plugin.js";
import { GafferMcpProvider } from "./mcp/provider.js";
import { runProjection } from "./commands/run-projection.js";
import { debugProjectionPick } from "./commands/debug-projection-pick.js";
import { initProjection } from "./commands/init-projection.js";
import {
	createVscodeWizardSteps,
	runScaffoldWizard,
	scaffoldProjection,
} from "./commands/scaffold-projection.js";

// workspaceCwd returns the first workspace folder's filesystem
// path so child processes (e.g. gaffer manifest) spawn relative
// to the user's project, not the editor's launch cwd. Returns
// undefined for single-buffer sessions with no workspace.
function workspaceCwd(): string | undefined {
	return vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
}

// Module-level telemetry handle so deactivate() can drain in-flight
// envelopes before VS Code kills the extension host. Set during
// activation; never reassigned after.
let activeTelemetry: Telemetry | null = null;

export async function activate(
	context: vscode.ExtensionContext,
): Promise<void> {
	const activationStart = performance.now();
	initOutput(context);
	initDiagnostics(context);

	const libVersion = context.extension.packageJSON.version;
	if (typeof libVersion !== "string") {
		throw new Error(
			`extension package.json version must be a string, got ${typeof libVersion}`,
		);
	}

	// Build the telemetry facade first so the manifest fetch can pass
	// `--invoker-id` to its CLI spawn and so telemetry holds a direct
	// telemetry handle (no lazy module-level indirection). The facade
	// itself is just identity-load + sink-construction; it emits
	// nothing on its own.
	activeTelemetry = await createTelemetry({
		storageDir: context.globalStorageUri.fsPath,
		libVersion,
		env: process.env,
		vscodeTelemetryLevel: readVscodeTelemetryLevel(),
		extensionPath: context.extensionPath,
		getWorkspaceFolders: () =>
			vscode.workspace.workspaceFolders?.map((f) => f.uri.fsPath) ?? [],
		log,
	});
	const telemetry = activeTelemetry;

	// First-run telemetry disclosure. Fire-and-forget; we hold the
	// promise so the facade can pick up a mid-session `[Disable]`
	// click once disclosure resolves (see refreshOptOut wiring below).
	const disclosurePromise = runTelemetryDisclosure(context);

	// Stale-on-edit: any text change to a file with a runtime error
	// invalidates that error (the in-memory content no longer matches
	// what was running when the error fired). Clear preemptively so
	// the user doesn't keep staring at a squiggle on code they're
	// already fixing. Wired here, adjacent to initDiagnostics, so
	// it's live before any awaited work below could surface a fatal.
	// Selector intentionally broad (any file) so we cover whatever
	// path the runtime reports — selector and reportFatalError stay
	// in sync regardless of future projection extensions.
	context.subscriptions.push(
		vscode.workspace.onDidChangeTextDocument((e) => {
			if (e.document.uri.scheme === "file" && e.contentChanges.length > 0) {
				clearDiagnosticsForUri(e.document.uri);
			}
		}),
		vscode.languages.registerCodeActionsProvider(
			{ scheme: "file" },
			wrapCodeActionProvider(new DismissDiagnosticActionProvider(), telemetry),
			{ providedCodeActionKinds: [vscode.CodeActionKind.QuickFix] },
		),
	);

	// Initial manifest snapshot - awaited up front so the lens
	// provider's first provideCodeLenses call sees the real
	// dev/--debug capability set. cwd is the first workspace
	// folder so gaffer-relative binaries resolve correctly;
	// node's execFile defaults to process.cwd() (the editor's
	// launch directory), not the workspace, so we must pass
	// it explicitly.
	//
	// Snapshot trust state at fetch time so the extension_activated
	// emit can distinguish "we didn't probe, workspace was untrusted"
	// from "we probed and the binary was missing" - tryFetchManifest
	// silently returns null on untrusted without firing onError, so
	// without this snapshot the two paths look identical downstream.
	const untrustedAtFetch = !vscode.workspace.isTrusted;
	let manifestErr: unknown;
	// The error is just captured here; the user-facing toast (generic
	// CLI-failure or the install prompt for ENOENT) fires from
	// activateAfterTelemetry below, once reloadManifest is in scope.
	const initialManifest = await tryFetchManifest(
		workspaceCwd(),
		activeTelemetry,
		(err) => {
			manifestErr = err;
		},
	);

	try {
		await activateAfterTelemetry({
			context,
			telemetry,
			disclosurePromise,
			initialManifest,
			manifestErr,
			untrustedAtFetch,
			activationStart,
		});
	} catch (err) {
		// Provider wiring / command registration / deep imports can
		// throw during activate. Fire an exception envelope before
		// propagating to VS Code's "extension failed to activate"
		// surface.
		telemetry.reportException("startup", err);
		throw err;
	}
}

interface ActivateAfterTelemetryArgs {
	context: vscode.ExtensionContext;
	telemetry: Telemetry;
	disclosurePromise: Promise<void>;
	initialManifest: Manifest | null;
	manifestErr: unknown;
	untrustedAtFetch: boolean;
	activationStart: number;
}

async function activateAfterTelemetry(
	args: ActivateAfterTelemetryArgs,
): Promise<void> {
	const {
		context,
		telemetry,
		disclosurePromise,
		initialManifest,
		manifestErr,
		untrustedAtFetch,
		activationStart,
	} = args;
	// extension_activated is emitted before disclosure resolves. If the
	// user clicks `[Disable]` on the first-run notice while activation
	// is in flight, this event still ships - it's the cost of having
	// extension_activated reflect every activation rather than only
	// the consenting ones. refreshOptOut (below) silences subsequent
	// emits.
	emitExtensionActivated(telemetry, {
		manifest: initialManifest,
		manifestErr,
		untrustedAtFetch,
		activationStart,
	});

	// When the disclosure finishes, re-read opt-out so an exception
	// fired after a `[Disable]` click is silenced.
	void disclosurePromise.then(() =>
		telemetry
			.refreshOptOut()
			.catch((err: unknown) =>
				log(
					`telemetry: refreshOptOut failed: ${err instanceof Error ? err.message : String(err)}`,
				),
			),
	);

	const stepProvider = new StepProvider();
	const stateProvider = new StateProvider();
	const statusProvider = new StatusViewProvider();
	const phaseTracker = new PhaseTracker((phase) =>
		statusProvider.setPhase(phase),
	);
	const lspCodeLens = new LspCodeLensProvider();
	context.subscriptions.push(stepProvider, stateProvider, lspCodeLens);

	// Single source of truth for the latest manifest. The LSP spawn
	// gate reads it via predicate; reloadManifest + handleManifestOutcome
	// update it.
	let latestManifest: Manifest | null = null;

	// Spawn the LSP server. The lens provider activates once
	// the client is ready; until then provideCodeLenses returns
	// [] (briefly, while initialize completes). startLanguageClient
	// owns both the trust gate and the manifest gate, deferring
	// the spawn until both clear and reattempting via
	// retryStartLanguageClient when the manifest reload chain
	// publishes a non-null result.
	startLanguageClient(
		context,
		() => latestManifest !== null,
		telemetry,
		(client) => {
			lspCodeLens.setClient(client);
		},
	);

	// Wire the tsserver plugin's configuration. Loaded by tsserver
	// via the `typescriptServerPlugins` contribution; configured here
	// with the vendored projection-types path. Static for the session
	// unless the user toggles gaffer.injectProjectionTypes.
	registerTypeScriptPlugin(context);

	// Register `gaffer mcp` as an MCP server with VS Code so
	// Copilot Chat (and any other MCP-aware agent in VS Code) picks
	// it up automatically. Provider returns [] under untrusted
	// workspaces; fires onDidChange on trust grant and on workspace
	// folder changes so the picker tracks reality.
	const mcpProvider = new GafferMcpProvider(telemetry);
	context.subscriptions.push(
		mcpProvider,
		vscode.lm.registerMcpServerDefinitionProvider(
			"gaffer",
			wrapMcpServerDefinitionProvider(mcpProvider, telemetry),
		),
		vscode.workspace.onDidGrantWorkspaceTrust(() => mcpProvider.refresh()),
		vscode.workspace.onDidChangeWorkspaceFolders(() => mcpProvider.refresh()),
		vscode.workspace.onDidChangeConfiguration((e) => {
			if (e.affectsConfiguration("gaffer.command")) {
				mcpProvider.refresh();
			}
		}),
	);

	const controller = new SessionController({
		buildArgv: (args, invokedVia) =>
			buildGafferArgv(args, {
				invokerId: telemetry.invokerId(),
				invokedVia,
			}),
		getSpawnEnv: () => gafferSpawnEnv(telemetry.isOptedOut()),
		stepProvider,
		stateProvider,
		statusProvider,
		phaseTracker,
		pushDebugState: (state) => {
			lspCodeLens.setDebugState(state);
		},
		readDebugPort: () =>
			vscode.workspace.getConfiguration("gaffer").get<number>("debugPort", -1),
	});
	controller.register(context);

	// Single sink for every manifest result, initial and reload alike.
	// Owns: latestManifest write-through, lens-provider notify, dismiss-
	// flag clear, and the toast / install-prompt routing for failures.
	// ENOENT goes to the install bootstrap unless the user dismissed it
	// for this workspace; the dismissal silences the fallback generic
	// toast too, since both share the same "binary missing" cause.
	// Other failures (parse, EACCES, timeout) always toast.
	const handleManifestOutcome = (
		manifest: Manifest | null,
		err: unknown,
		opts: { trustedAtFetch: boolean },
	): void => {
		latestManifest = manifest;
		lspCodeLens.setManifest(manifest);
		// Kick the LSP gate after every outcome so a manifest that
		// just transitioned from null to a value spawns the deferred
		// client. Idempotent if the client is already up or if any
		// gate still fails.
		retryStartLanguageClient();
		if (manifest !== null) {
			void clearInstallPromptDismissed(context);
			return;
		}
		if (err === undefined) return;
		if (!opts.trustedAtFetch) return;
		const reason = classifyManifestError(err);
		if (reason === "binary_not_found") {
			if (!isInstallPromptDismissed(context)) {
				void showCliNotFoundPrompt({
					context,
					runInstall: runNpmInstall,
					onInstalled: reloadManifest,
				});
			}
			return;
		}
		void showManifestFailure(err);
	};

	// Async orchestrator, serialised on a promise chain so
	// overlapping events (config change + trust grant in quick
	// succession) can't interleave setters out of order. Body is
	// try/caught so a transient failure can't poison the chain.
	// Drives the manifest only - toml content and projection
	// metadata flow through the LSP server's own walker /
	// watcher / cached parses.
	let refreshChain: Promise<void> = Promise.resolve();
	const reloadManifest = (): Promise<void> => {
		refreshChain = refreshChain.then(async () => {
			try {
				let reloadErr: unknown;
				const trustedAtFetch = vscode.workspace.isTrusted;
				const m = await tryFetchManifest(workspaceCwd(), telemetry, (e) => {
					reloadErr = e;
				});
				handleManifestOutcome(m, reloadErr, { trustedAtFetch });
			} catch (err) {
				log(
					`Manifest reload failed: ${err instanceof Error ? err.message : String(err)}`,
				);
			}
		});
		return refreshChain;
	};

	// Initial outcome routed through the same sink. The error was
	// captured during activate()'s tryFetchManifest call; we surface
	// it here because the install-prompt's Install action needs
	// reloadManifest in scope.
	handleManifestOutcome(initialManifest, manifestErr, {
		trustedAtFetch: !untrustedAtFetch,
	});

	context.subscriptions.push(
		vscode.window.registerTreeDataProvider(
			"gaffer.step",
			wrapTreeDataProvider(stepProvider, telemetry),
		),
		vscode.window.registerTreeDataProvider(
			"gaffer.state",
			wrapTreeDataProvider(stateProvider, telemetry),
		),
		vscode.window.registerWebviewViewProvider(
			"gaffer.status",
			wrapWebviewViewProvider(statusProvider, telemetry),
			{ webviewOptions: { retainContextWhenHidden: true } },
		),
	);

	context.subscriptions.push(
		vscode.debug.registerDebugAdapterTrackerFactory(
			"gaffer",
			new PausePendingTrackerFactory(statusProvider),
		),
		vscode.debug.registerDebugAdapterTrackerFactory(
			"gaffer",
			new RestartTrackerFactory({
				stepProvider,
				stateProvider,
				statusProvider,
				phaseTracker,
				sessionName: () => controller.getDebugState().name ?? "projection",
			}),
		),
	);

	context.subscriptions.push(
		vscode.debug.registerDebugAdapterDescriptorFactory("gaffer", {
			createDebugAdapterDescriptor(session) {
				// session.configuration.port is set by SessionController.start
				// (from the CLI's actual bound port via waitForDebug). For
				// launch.json-driven attach the schema defaults it to 4711.
				const port = session.configuration.port;
				if (typeof port !== "number") {
					throw new Error("gaffer debug session missing port in configuration");
				}
				return new vscode.DebugAdapterServer(port);
			},
		}),
	);

	context.subscriptions.push(
		vscode.languages.registerCodeLensProvider(
			[
				{ scheme: "file", pattern: "**/gaffer.toml" },
				{ scheme: "file", language: "javascript" },
			],
			wrapCodeLensProvider(lspCodeLens, telemetry),
		),
	);

	context.subscriptions.push(
		vscode.debug.onDidReceiveDebugSessionCustomEvent((e) =>
			dispatchDapEvent(e, {
				stepProvider,
				stateProvider,
				statusProvider,
				phaseTracker,
				setEngineMode: (mode) => controller.setEngineMode(mode),
			}),
		),
	);

	// Command handlers live in src/commands/. activate() injects the
	// SessionController.start binding (and workspace cwd resolver for
	// runProjection's manifest fetch); the command bodies own their
	// own UX flows.
	//
	// Every handler runs through wrapAsync so a thrown error fires an
	// `exception` envelope before VS Code's command-failure surface
	// kicks in. The handlers stay otherwise unchanged.
	//
	// debugProjection* are CodeLens-only (package.json `when: false`);
	// runProjection is the palette entry point.
	const startSessionLens = (args: DebugProjectionArgs): Promise<void> =>
		controller.start(args, "code_lens");
	const startSessionPalette = (args: DebugProjectionArgs): Promise<void> =>
		controller.start(args, "command_palette");
	const wrap = <A extends unknown[], R>(
		fn: (...args: A) => Promise<R> | R,
	): ((...args: A) => Promise<R>) =>
		wrapAsync(telemetry, "event_processing", fn);
	context.subscriptions.push(
		vscode.commands.registerCommand(
			"gaffer.stopDebug",
			wrap(() => controller.stop()),
		),
		vscode.commands.registerCommand(
			"gaffer.debugProjection",
			wrap(startSessionLens),
		),
		vscode.commands.registerCommand(
			"gaffer.debugProjectionPick",
			wrap(debugProjectionPick({ start: startSessionLens })),
		),
		vscode.commands.registerCommand(
			"gaffer.runProjection",
			wrap(
				runProjection({
					start: startSessionPalette,
					workspaceCwd,
					telemetry,
				}),
			),
		),
		(() => {
			const init = wrap(initProjection({ telemetry }));
			const scaffold = wrap(
				scaffoldProjection({
					telemetry,
					wizard: (flavor) =>
						runScaffoldWizard(createVscodeWizardSteps(flavor)),
				}),
			);
			// gaffer.scaffold is the palette entry; gaffer.scaffoldHere
			// is the explorer/context-menu entry (URI arg = clicked
			// folder). Two IDs so the menu can show a folder-specific
			// label - VS Code menu items can't override command titles.
			return vscode.Disposable.from(
				vscode.commands.registerCommand("gaffer.init", init),
				vscode.commands.registerCommand("gaffer.scaffold", scaffold),
				vscode.commands.registerCommand("gaffer.scaffoldHere", scaffold),
			);
		})(),
		// Click target for the "Invalid fixture: <reason>" lens. The lens
		// is informational; the user fixes the toml. CodeLens.command is
		// required by VS Code, so we route to a registered no-op.
		vscode.commands.registerCommand("gaffer.noop", () => {}),
		// Lightbulb action target for runtime fatal-error squiggles.
		// Clears the diagnostic for the file without requiring an edit.
		vscode.commands.registerCommand(
			"gaffer.dismissDiagnostic",
			wrap((uri: vscode.Uri) => clearDiagnosticsForUri(uri)),
		),
	);

	context.subscriptions.push(
		vscode.workspace.onDidChangeConfiguration(async (e) => {
			if (e.affectsConfiguration("gaffer.command")) {
				log("gaffer.command setting changed");
				await reloadManifest();
			}
			if (e.affectsConfiguration("telemetry.telemetryLevel")) {
				// The user can flip the global telemetry level mid-session
				// without a reload. refreshOptOut re-reads the cascade and
				// flips the facade's one-way latch if the new level is no
				// longer "all". (Re-enabling needs a fresh activation -
				// the latch is one-way.)
				await telemetry
					.refreshOptOut()
					.catch((err: unknown) =>
						log(
							`telemetry: refreshOptOut failed: ${err instanceof Error ? err.message : String(err)}`,
						),
					);
			}
		}),
		vscode.workspace.onDidGrantWorkspaceTrust(async () => {
			log("workspace trusted");
			lspCodeLens.refresh();
			await reloadManifest();
		}),
	);
}

function emitExtensionActivated(
	telemetry: Telemetry,
	args: {
		manifest: Manifest | null;
		manifestErr: unknown;
		untrustedAtFetch: boolean;
		activationStart: number;
	},
): void {
	const cliReachable = args.manifest !== null;
	const properties: ExtensionActivatedProperties = {
		editor: detectEditor(vscode.env.appName),
		editor_version: vscode.version,
		cli_reachable: cliReachable,
		activation_duration_ms: bucketDuration(
			performance.now() - args.activationStart,
		),
	};
	if (args.manifest !== null && args.manifest.version) {
		properties.cli_version = bucketCliVersion(args.manifest.version);
	}
	if (!cliReachable) {
		// Trust gate wins over execFile classification: an untrusted
		// workspace never spawned the binary, so cataloguing the
		// failure as "binary_not_found" would mislead dashboards.
		if (args.untrustedAtFetch) {
			properties.cli_unreachable_reason = "workspace_untrusted";
		} else if (args.manifestErr !== undefined) {
			properties.cli_unreachable_reason = classifyManifestError(
				args.manifestErr,
			);
		} else {
			properties.cli_unreachable_reason = "unknown_error";
		}
	}
	telemetry.emit({
		name: "extension_activated",
		timestamp: new Date().toISOString(),
		properties,
	});
}

// runTelemetryDisclosure reads the persisted extension telemetry
// state + opt-out cascade, and fires the first-run notification
// when both (a) we haven't already disclosed on this install and
// (b) no other opt-out signal is in effect. Errors are logged but
// not surfaced - failing to disclose is preferable to crashing
// activation.
async function runTelemetryDisclosure(
	context: vscode.ExtensionContext,
): Promise<void> {
	try {
		const storageDir = context.globalStorageUri.fsPath;
		const config = await loadSafe(storageDir);
		const optOut = checkOptOut({
			config,
			env: process.env,
			vscodeTelemetryLevel: readVscodeTelemetryLevel(),
		});
		await runFirstRunNotice({
			storageDir,
			config,
			optedOut: optOut.disabled,
			prompt: showTelemetryDisclosure,
			openLearnMore: async () => {
				await openTelemetryDisclosurePage();
			},
		});
	} catch (err) {
		log(
			`telemetry disclosure failed: ${err instanceof Error ? err.message : String(err)}`,
		);
	}
}

export async function deactivate(): Promise<void> {
	// VS Code's deactivate budget is ~5s. Run telemetry drain and
	// LSP stop concurrently under a single deadline so a slow LSP
	// shutdown can't push the total wall-clock past the host's
	// tolerance, and so a slow drain doesn't delay LSP cleanup.
	const tasks: Promise<unknown>[] = [stopLanguageClient()];
	if (activeTelemetry !== null) {
		tasks.push(activeTelemetry.drain(4500));
	}
	let timer: NodeJS.Timeout | undefined;
	const deadline = new Promise<void>((resolve) => {
		timer = setTimeout(resolve, 4500);
	});
	try {
		await Promise.race([Promise.allSettled(tasks), deadline]);
	} finally {
		if (timer !== undefined) clearTimeout(timer);
	}
}
