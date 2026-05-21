import { beforeEach, vi } from "vitest";
import { __resetForTest as resetDiagnostics } from "../src/diagnostics.js";
import { __resetCommandUnresolvedPromptStateForTests } from "../src/notifications/command-unresolved-prompt.js";
import { __resetInstallPromptStateForTests } from "../src/notifications/install-prompt.js";
import { __resetUpdatePromptStateForTests } from "../src/notifications/update-prompt.js";
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
	// Status-bar prompts hold module-level state (active item, command
	// disposable) that survives a vscode-state reset because the JS
	// variables aren't part of that state. Reset here so each test
	// sees a clean prompt module.
	__resetInstallPromptStateForTests();
	__resetUpdatePromptStateForTests();
	__resetCommandUnresolvedPromptStateForTests();
	// Default-stub globalThis.fetch so tests that exercise activate()
	// (or any other path that builds a real Telemetry facade) don't
	// fire envelopes to the staging worker. The facade's `fetchImpl`
	// option is the explicit override for tests that want to assert on
	// outgoing requests; this guard is for everything else.
	vi.stubGlobal(
		"fetch",
		vi.fn(async () => new Response("", { status: 200 })),
	);
});
