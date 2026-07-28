// The extension's gated method names against what the CLI actually advertises.
//
// `serverServes` is a string match, so a method name the server never advertises
// reads as permanently unsupported and its surface is hidden with no error
// anywhere - the failure is silent and looks like a working gate. A typo, or a
// rename on one side only, produces exactly that.
//
// `lsp-experimental-current.json` is the `capabilities.experimental` block from
// this checkout's `gaffer lsp` initialize result. Regenerate it deliberately when
// the served set changes; the Go side has its own guard
// (TestGafferMethodsMatchProtocol) that the advertised list matches the dispatch
// switch, so this only has to check the extension agrees with it.

import * as fs from "node:fs";
import * as path from "node:path";
import { describe, expect, it } from "vitest";
import {
	METHOD_DIFF_PROJECTION,
	METHOD_DIFF_VERSIONS,
	METHOD_OPERATE_PROJECTION,
	METHOD_PROJECTION_DETAILS,
	METHOD_REFRESH_STATUS,
	readServedMethods,
} from "./capabilities.js";

const experimental = JSON.parse(
	fs.readFileSync(
		path.join(__dirname, "../../test/fixtures/lsp-experimental-current.json"),
		"utf8",
	),
) as unknown;

// Every method the extension has a constant for. Each is either gated on or sent,
// so all of them have to be names the server knows.
const EXTENSION_METHODS = [
	METHOD_DIFF_PROJECTION,
	METHOD_DIFF_VERSIONS,
	METHOD_OPERATE_PROJECTION,
	METHOD_PROJECTION_DETAILS,
	METHOD_REFRESH_STATUS,
];

describe("served method names match the CLI", () => {
	// Routed through readServedMethods rather than reading the JSON directly, so
	// the nesting the extension expects is checked against the shape the server
	// actually sends - a moved or renamed level would surface here.
	const served = readServedMethods({ capabilities: { experimental } });

	it("parses the real advertised block", () => {
		expect(served.size).toBeGreaterThan(0);
	});

	it.each(EXTENSION_METHODS)("the CLI advertises %s", (method) => {
		expect(served.has(method)).toBe(true);
	});

	// Not a failure - a newer CLI may serve methods this extension has no gate for
	// yet - but worth surfacing, since it usually means a surface was added on the
	// CLI side and the editor hasn't caught up.
	it("has a constant for every advertised method", () => {
		expect([...served].filter((m) => !EXTENSION_METHODS.includes(m))).toEqual(
			[],
		);
	});
});
