// gaffer-tsserver-plugin: injects projection-runtime type declarations
// into the tsserver program for projection JS files, so users get
// autocomplete / hover / signature help on `fromStream`, `when`,
// `emit`, etc. without writing a jsconfig in their repo.
//
// Loaded by tsserver via the `typescriptServerPlugins` contribution
// point in the VS Code extension. Configuration arrives at runtime
// through `info.config` (set via `configurePlugin` by the extension)
// and is updated when the user toggles the gaffer.injectProjectionTypes
// setting.
//
// The plugin scopes injection at the tsserver-project level: globals
// declared via `declare global` in projections.d.ts apply to every
// file in the same inferred project, not just registered projections.
// In practice tsserver groups loose JS in one InferredProject per
// workspace root, so the types appear in any sibling .js. The
// gaffer.injectProjectionTypes setting lets users opt out for
// workspaces where that's noisy.

import type ts from "typescript/lib/tsserverlibrary.js";
import type { PluginConfig } from "./config.js";

// Marker on a wrapped getScriptFileNames so repeated create() calls
// against the same host don't stack wrappers (which would let the
// types entry be included multiple times). VS Code's tsserver
// reconfigures projects when configurePlugin is called, and create()
// can run again on the same host - without the marker, each run
// adds another layer.
const WRAPPED = Symbol.for("gaffer-tsserver-plugin/wrapped");

function init(_modules: { typescript: typeof ts }): ts.server.PluginModule {
	// Shared between create() and onConfigurationChanged. tsserver
	// calls create() once per project but only ever instantiates the
	// plugin module once, so a single config closure covers all of
	// them.
	let config: PluginConfig = {};

	return {
		create(info) {
			config = (info.config ?? {}) as PluginConfig;
			info.project.projectService.logger.info(
				`[gaffer-tsserver-plugin] create: ${describeConfig(config)}`,
			);

			const host = info.languageServiceHost;
			const existing =
				host.getScriptFileNames as typeof host.getScriptFileNames & {
					[WRAPPED]?: true;
				};
			if (existing[WRAPPED]) {
				return info.languageService;
			}
			const originalGetScriptFileNames = host.getScriptFileNames.bind(host);

			const wrapped: (() => string[]) & { [WRAPPED]?: true } = () => {
				const base = originalGetScriptFileNames();
				if (config.enabled === false) {
					return [...base];
				}
				if (!config.typesEntryPath) {
					return [...base];
				}
				if (base.includes(config.typesEntryPath)) {
					return [...base];
				}
				// Return the program's existing files plus the
				// projection types entry. tsserver follows the
				// entry's imports to load the rest of the types
				// modules - no need to enumerate them here.
				return [...base, config.typesEntryPath];
			};
			wrapped[WRAPPED] = true;
			host.getScriptFileNames = wrapped;

			return info.languageService;
		},
		onConfigurationChanged(next: unknown) {
			config = (next ?? {}) as PluginConfig;
		},
	};
}

function describeConfig(config: PluginConfig): string {
	const enabled = config.enabled === false ? "disabled" : "enabled";
	const types = config.typesEntryPath ?? "<unset>";
	return `${enabled} typesEntryPath=${types}`;
}

// tsserver loads the plugin by `require`ing the package and calling
// the default export with the typescript module.
export = init;
