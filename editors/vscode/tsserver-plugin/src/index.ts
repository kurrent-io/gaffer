// gaffer-tsserver-plugin: injects projection-runtime type declarations
// into the tsserver program for projection JS files, so users get
// autocomplete / hover / signature help on `fromStream`, `when`,
// `emit`, etc. without writing a jsconfig in their repo.
//
// Loaded by tsserver via the `typescriptServerPlugins` contribution
// point in the VS Code extension. Configuration arrives at runtime
// through `info.config` (set via `configurePlugin` by the extension)
// and is updated whenever the projection set changes.

import * as path from "node:path";
import type ts from "typescript/lib/tsserverlibrary.js";

interface PluginConfig {
	// Absolute paths of every valid projection's entry .js, sourced
	// from gaffer/projectionEntryPaths over LSP. Programs containing
	// any of these files get the projection types injected.
	projectionPaths?: readonly string[];
	// Absolute path of types/src/projections.d.ts, sourced from the
	// extension at activation time (it knows where it shipped the
	// vendored copy). Required for the plugin to do anything useful;
	// when missing, the plugin no-ops.
	typesEntryPath?: string;
}

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
			const originalGetScriptFileNames = host.getScriptFileNames.bind(host);

			host.getScriptFileNames = () => {
				const base = originalGetScriptFileNames();
				if (!config.typesEntryPath) {
					return base;
				}
				if (!isProjectionProgram(base, config.projectionPaths)) {
					return base;
				}
				if (base.includes(config.typesEntryPath)) {
					return base;
				}
				// Return the program's existing files plus the
				// projection types entry. tsserver follows the
				// entry's imports to load the rest of the types
				// modules - no need to enumerate them here.
				return [...base, config.typesEntryPath];
			};

			return info.languageService;
		},
		onConfigurationChanged(next: unknown) {
			config = (next ?? {}) as PluginConfig;
		},
	};
}

function isProjectionProgram(
	files: readonly string[],
	projectionPaths: readonly string[] | undefined,
): boolean {
	if (!projectionPaths || projectionPaths.length === 0) {
		return false;
	}
	const set = new Set(projectionPaths.map((p) => path.normalize(p)));
	for (const f of files) {
		if (set.has(path.normalize(f))) {
			return true;
		}
	}
	return false;
}

function describeConfig(config: PluginConfig): string {
	const count = config.projectionPaths?.length ?? 0;
	const types = config.typesEntryPath ?? "<unset>";
	return `projectionPaths=${count} typesEntryPath=${types}`;
}

// tsserver loads the plugin by `require`ing the package and calling
// the default export with the typescript module.
export = init;
