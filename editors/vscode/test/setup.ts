import { beforeEach } from "vitest";
import { __resetForTest as resetDiagnostics } from "../src/diagnostics.js";
import { __resetForTest as resetOutput } from "../src/output.js";
import { resetVscode } from "./testutil/vscode-state.js";

beforeEach(() => {
	resetVscode();
	// Module-level singletons in production cache the OutputChannel /
	// DiagnosticCollection across imports. Without resetting them, the
	// next test's writes go to the previous test's (now-orphaned) fake
	// channel and disappear from the new state.outputChannels[].
	resetOutput();
	resetDiagnostics();
});
