import * as path from "node:path";
import * as vscode from "vscode";
import type { PluginConfig } from "gaffer-tsserver-plugin/config";
import { log } from "../output.js";

interface TypeScriptApi {
	configurePlugin(pluginId: string, configuration: unknown): void;
}

interface TypeScriptExports {
	getAPI(version: number): TypeScriptApi | undefined;
}

const PLUGIN_ID = "gaffer-tsserver-plugin";
const SETTING_ENABLED = "gaffer.injectProjectionTypes";

// projectionTypesPath returns the absolute path of the vendored
// projections.d.ts. vite's vendor-projection-types plugin copies
// types/src/ to dist/types/ at build time so the file is reachable
// regardless of whether the extension is running from source or an
// installed VSIX.
function projectionTypesPath(context: vscode.ExtensionContext): string {
	return path.join(context.extensionPath, "dist/types/projections.d.ts");
}

async function getTypeScriptApi(): Promise<TypeScriptApi | undefined> {
	const ext =
		vscode.extensions.getExtension<TypeScriptExports>(
			"vscode.typescript-language-features",
		) ??
		vscode.extensions.getExtension<TypeScriptExports>(
			"ms-vscode.vscode-typescript-next",
		);
	if (!ext) {
		log("ts-plugin: typescript-language-features not installed");
		return undefined;
	}
	try {
		if (!ext.isActive) {
			await ext.activate();
		}
	} catch (err) {
		log(
			`ts-plugin: typescript-language-features activate failed: ${err instanceof Error ? err.message : String(err)}`,
		);
		return undefined;
	}
	const api = ext.exports.getAPI(0);
	if (!api) {
		log("ts-plugin: typescript-language-features API v0 unavailable");
	}
	return api;
}

/**
 * Wire the gaffer tsserver plugin's configuration. Resolves the
 * typescript-language-features API once at activation, then pushes
 * (typesEntryPath, enabled) into the plugin via `configurePlugin`.
 *
 * Re-pushes when the user toggles the gaffer.injectProjectionTypes
 * setting; otherwise the configuration is static for the session.
 * The projection-files allowlist that earlier versions threaded
 * through here was redundant - tsserver groups loose JS into one
 * InferredProject per workspace root, so any registered projection's
 * presence in the workspace already widens the program to siblings.
 */
export function registerTypeScriptPlugin(
	context: vscode.ExtensionContext,
): void {
	const typesEntryPath = projectionTypesPath(context);
	let api: TypeScriptApi | undefined;
	let lastConfig: PluginConfig | undefined;

	// Serialise pushes on a promise chain so two rapid configuration
	// events (e.g. the activation push racing the user toggling
	// injectProjectionTypes) can't both pass the lastConfig check
	// before either assigns it. Same posture as the manifest reload
	// chain in extension.ts.
	let pushChain: Promise<void> = Promise.resolve();
	const push = (): Promise<void> => {
		pushChain = pushChain.then(async () => {
			if (!api) {
				api = await getTypeScriptApi();
				if (!api) return;
			}
			const enabled = vscode.workspace
				.getConfiguration("gaffer")
				.get<boolean>("injectProjectionTypes", true);
			const config: PluginConfig = { typesEntryPath, enabled };
			// Skip configurePlugin when the payload hasn't changed.
			// configurePlugin triggers a tsserver project reload, which
			// invalidates user-visible state (open diagnostics, hover,
			// completions); pushing identical config would churn that
			// for no reason.
			if (
				lastConfig &&
				lastConfig.typesEntryPath === config.typesEntryPath &&
				lastConfig.enabled === config.enabled
			) {
				return;
			}
			lastConfig = config;
			api.configurePlugin(PLUGIN_ID, config);
			log(
				`ts-plugin: configurePlugin enabled=${config.enabled} typesEntryPath=${config.typesEntryPath}`,
			);
		});
		return pushChain;
	};

	context.subscriptions.push(
		vscode.workspace.onDidChangeConfiguration((e) => {
			if (e.affectsConfiguration(SETTING_ENABLED)) {
				void push();
			}
		}),
	);
	void push();
}
