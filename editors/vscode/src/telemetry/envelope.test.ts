import type { ExtensionActivated } from "@kurrent/gaffer-telemetry";
import { describe, expect, it } from "vitest";

import { buildEnvelope } from "./envelope.js";
import type { Identity } from "./identity.js";

const id: Identity = {
	telemetryId: "8f2b1a4c-9e7d-4a3e-b5f2-7c8a9d4e1f02",
	salt: "11111111-2222-3333-4444-555555555555",
	runId: "33333333-3333-3333-3333-333333333333",
};

const activated: ExtensionActivated = {
	name: "extension_activated",
	timestamp: "2026-05-13T10:00:00.000Z",
	properties: {
		editor: "vscode",
		editor_version: "1.95.2",
		cli_reachable: true,
		cli_version: "0.4",
		activation_duration_ms: 100,
	},
};

describe("buildEnvelope", () => {
	it("stamps schema_version, emitter_id, run_id, and events", () => {
		const env = buildEnvelope({
			identity: id,
			libVersion: "0.4.2",
			events: [activated],
			env: {},
		});
		expect(env.schema_version).toBe("1");
		expect(env.emitter_id).toBe(id.telemetryId);
		expect(env.run_id).toBe(id.runId);
		expect(env.events).toEqual([activated]);
	});

	it("sets emitter=vscode and lib_version", () => {
		const env = buildEnvelope({
			identity: id,
			libVersion: "0.4.2",
			events: [activated],
			env: {},
		});
		expect(env.context.emitter).toBe("vscode");
		expect(env.context.lib_version).toBe("0.4.2");
	});

	it("detects os/arch from process globals", () => {
		const env = buildEnvelope({
			identity: id,
			libVersion: "0.4.2",
			events: [activated],
			env: {},
		});
		// vitest runs on whatever the dev machine / CI is, so just check
		// the enum membership. The actual mapping is exercised by the
		// type system on the supported targets.
		expect(["linux", "darwin", "windows"]).toContain(env.context.os);
		expect(["x64", "arm64", "ia32"]).toContain(env.context.arch);
	});

	it("flags runtime_environment=ci when CI is set", () => {
		expect(
			buildEnvelope({
				identity: id,
				libVersion: "0.4.2",
				events: [activated],
				env: { CI: "true" },
			}).context.runtime_environment,
		).toBe("ci");
	});

	it("flags runtime_environment=ci on TEAMCITY_VERSION or JENKINS_URL", () => {
		for (const k of ["TEAMCITY_VERSION", "JENKINS_URL"]) {
			expect(
				buildEnvelope({
					identity: id,
					libVersion: "0.4.2",
					events: [activated],
					env: { [k]: "anything" },
				}).context.runtime_environment,
			).toBe("ci");
		}
	});

	it("ignores empty-string CI env values", () => {
		expect(
			buildEnvelope({
				identity: id,
				libVersion: "0.4.2",
				events: [activated],
				env: { CI: "" },
			}).context.runtime_environment,
		).toBe("local");
	});

	it("defaults runtime_environment=local with no CI env", () => {
		expect(
			buildEnvelope({
				identity: id,
				libVersion: "0.4.2",
				events: [activated],
				env: {},
			}).context.runtime_environment,
		).toBe("local");
	});

	it("includes project_id when supplied, omits when not", () => {
		const withProject = buildEnvelope({
			identity: id,
			libVersion: "0.4.2",
			events: [activated],
			env: {},
			projectId: "a1b2c3d4e5f6789a",
		});
		expect(withProject.context.project_id).toBe("a1b2c3d4e5f6789a");

		const without = buildEnvelope({
			identity: id,
			libVersion: "0.4.2",
			events: [activated],
			env: {},
		});
		expect(without.context.project_id).toBeUndefined();
	});

	it("includes invoker_id when supplied, omits when not", () => {
		const withInvoker = buildEnvelope({
			identity: id,
			libVersion: "0.4.2",
			events: [activated],
			env: {},
			invokerId: "00000000-0000-0000-0000-000000000000",
		});
		expect(withInvoker.context.invoker_id).toBe(
			"00000000-0000-0000-0000-000000000000",
		);

		const without = buildEnvelope({
			identity: id,
			libVersion: "0.4.2",
			events: [activated],
			env: {},
		});
		expect(without.context.invoker_id).toBeUndefined();
	});
});
