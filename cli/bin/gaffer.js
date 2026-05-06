#!/usr/bin/env node
// Resolve the platform-specific gaffer binary from optional dependencies and exec it.
const { spawnSync } = require("node:child_process");

// Keep in sync with optionalDependencies in package.json.
const SUPPORTED = new Set([
	"linux-x64",
	"linux-arm64",
	"darwin-x64",
	"darwin-arm64",
	"win32-x64",
]);

const platformKey = `${process.platform}-${process.arch}`;
const platformPackage = `@kurrent/gaffer-${platformKey}`;
const binaryName = process.platform === "win32" ? "gaffer.exe" : "gaffer";

if (!SUPPORTED.has(platformKey)) {
	console.error(`gaffer: unsupported platform ${platformKey}`);
	console.error(`Supported: ${[...SUPPORTED].join(", ")}`);
	process.exit(1);
}

let binaryPath;
try {
	binaryPath = require.resolve(`${platformPackage}/${binaryName}`);
} catch {
	console.error(`gaffer: native binary for ${platformKey} not found.`);
	console.error(`The optional dependency ${platformPackage} may have failed to install.`);
	console.error(`Try reinstalling: \`npm install --force\` or \`pnpm install\`.`);
	process.exit(1);
}

const result = spawnSync(binaryPath, process.argv.slice(2), { stdio: "inherit" });
if (result.error) {
	console.error(`gaffer: failed to launch ${binaryPath}: ${result.error.message}`);
	process.exit(1);
}
process.exit(result.status ?? (result.signal ? 1 : 0));
