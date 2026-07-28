// Gate decisions against a real released CLI manifest, not a hand-written stub.
//
// The stubs elsewhere describe what a test needs; this one is `gaffer manifest`
// captured verbatim from the published @kurrent/gaffer 0.4.2 - the version a user
// is on when the extension auto-updates ahead of their CLI. It catches what a
// stub can't: a gate keyed on a command or flag name that no real CLI has, and a
// schema that stopped parsing an older manifest.
//
// The fixture is pinned to a released version and never regenerated - it is a
// record of what shipped. A newer floor means a new fixture alongside it.

import * as fs from "node:fs";
import * as path from "node:path";
import { describe, expect, it } from "vitest";
import * as v from "valibot";
import { ManifestSchema } from "./schemas.js";
import { canRunArgv, hasCommand, hasFlag } from "./cli.js";
import { canDeploy } from "../commands/deploy-args.js";
import { canReadHistory, canRollback } from "../commands/history-args.js";

function loadFixture(name: string): unknown {
	return JSON.parse(
		fs.readFileSync(path.join(__dirname, "../../test/fixtures", name), "utf8"),
	) as unknown;
}

const fixture = loadFixture("manifest-0.4.2.json");

describe("released CLI 0.4.2 manifest", () => {
	// The schema has to keep parsing an older manifest, or the extension falls back
	// to a null manifest and hides everything - including the surfaces that CLI can
	// serve perfectly well.
	it("still parses under the current schema", () => {
		const parsed = v.safeParse(ManifestSchema, fixture);
		expect(parsed.success).toBe(true);
	});

	const manifest = v.parse(ManifestSchema, fixture);

	it("is the version it claims to be", () => {
		expect(manifest.version).toBe("0.4.2");
	});

	// What this release could do, so a gate that starts passing against it shows up
	// here rather than as a surface that errors on a click.
	it("carries only the pre-deploy command surface", () => {
		expect(Object.keys(manifest.commands).sort()).toEqual([
			"auth",
			"config telemetry off",
			"config telemetry on",
			"config telemetry status",
			"dev",
			"info",
			"init",
			"lsp",
			"mcp",
			"scaffold",
		]);
	});

	it("cannot deploy, read history, or roll back", () => {
		expect(canDeploy(manifest)).toBe(false);
		expect(canReadHistory(manifest)).toBe(false);
		expect(canRollback(manifest)).toBe(false);
	});

	// The debug surface is the one thing this release does support, so the gating
	// work must not have hidden it. A test that only checks things are off would
	// pass just as well with every gate stuck closed.
	it("can still debug, so the gates aren't uniformly closed", () => {
		expect(hasCommand(manifest, "dev")).toBe(true);
		expect(hasFlag(manifest, "dev", "debug")).toBe(true);
		expect(
			canRunArgv(manifest, ["dev", "--debug", "--json", "--", "orders"]),
		).toBe(true);
	});
});

// The other direction, and this change's worst failure mode: a gate that closes
// against a CLI that can in fact serve the surface, so the release quietly ships
// with no way to deploy from the editor. The 0.4.2 fixture can't catch that - a
// gate stuck closed passes every one of its assertions.
//
// `manifest-current.json` is `gaffer manifest` from this checkout, with `version`
// normalised so a version bump alone doesn't churn it. It is meant to rot: if a
// command or flag the extension passes is renamed or dropped, this fails and
// points at the argv builder that needs updating. Regenerate deliberately with
// `gaffer manifest`, never to make a red test green without reading why.
describe("current CLI manifest", () => {
	const manifest = v.parse(
		ManifestSchema,
		loadFixture("manifest-current.json"),
	);

	it("can run every gated cold spawn", () => {
		expect(canDeploy(manifest)).toBe(true);
		expect(canReadHistory(manifest)).toBe(true);
		expect(canRollback(manifest)).toBe(true);
	});

	it("still supports the debug spawn", () => {
		expect(
			canRunArgv(manifest, ["dev", "--debug", "--json", "--", "orders"]),
		).toBe(true);
	});
});
