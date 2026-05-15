// Schema validation coverage. Pins one envelope per command_invoked
// variant (with its variant-specific properties exercised), plus one
// per non-command event type. The schema generator's allOf composition
// for command variants is fiddly enough that a missing variant fixture
// hid a worker-side rejection of every real CLI dev/manifest/mcp/lsp
// event during staging shakedown - existing fixtures only exercised
// `version` which has no variant-specific extras.

import { Validator } from "@cfworker/json-schema";
import { describe, expect, it } from "vitest";
import schema from "../../generated/telemetry.schema.json" with { type: "json" };

const validator = new Validator(schema as never, "2019-09");

const baseEnvelope = {
	schema_version: "1",
	emitter_id: "00000000-0000-0000-0000-000000000001",
	run_id: "00000000-0000-0000-0000-000000000002",
	context: {
		emitter: "cli",
		lib_version: "0.4.2",
		os: "linux",
		arch: "x64",
		runtime_environment: "local",
	},
};

function commandInvoked(properties: Record<string, unknown>) {
	return {
		...baseEnvelope,
		events: [
			{
				name: "command_invoked",
				timestamp: "2026-05-08T12:00:00.000Z",
				properties,
			},
		],
	};
}

function assertValid(envelope: unknown, label: string) {
	const result = validator.validate(envelope);
	if (!result.valid) {
		const messages = result.errors.map((e) => `  - ${JSON.stringify(e)}`).join("\n");
		throw new Error(`${label} should validate, got:\n${messages}`);
	}
}

describe("schema validation - command_invoked variants", () => {
	const baseProps = {
		duration_ms: 0,
		outcome: "success",
		invoked_by: "direct",
		invoked_via: "terminal",
	} as const;

	it("version: base-only", () => {
		assertValid(commandInvoked({ ...baseProps, command: "version" }), "version");
	});

	it("init: base-only", () => {
		assertValid(commandInvoked({ ...baseProps, command: "init" }), "init");
	});

	it("scaffold: base-only", () => {
		assertValid(commandInvoked({ ...baseProps, command: "scaffold" }), "scaffold");
	});

	it("info: base-only", () => {
		assertValid(commandInvoked({ ...baseProps, command: "info" }), "info");
	});

	it("manifest: with manifest_features_used + bucketed counts", () => {
		assertValid(
			commandInvoked({
				...baseProps,
				command: "manifest",
				manifest_features_used: ["connection", "engine_version", "fixtures"],
				projection_count: 2,
				fixture_count: 2,
			}),
			"manifest",
		);
	});

	it("dev: with connected_to_db and shared manifest_* fields", () => {
		assertValid(
			commandInvoked({
				...baseProps,
				command: "dev",
				outcome: "manifest_not_found",
				connected_to_db: false,
				manifest_features_used: ["connection"],
				projection_count: 0,
				fixture_count: 0,
			}),
			"dev",
		);
	});

	it("mcp: with manifest_features_used + tool/resource counts", () => {
		assertValid(
			commandInvoked({
				...baseProps,
				command: "mcp",
				manifest_features_used: ["connection"],
				tool_call_count: 10,
				resource_read_count: 2,
			}),
			"mcp",
		);
	});

	it("lsp: with code_lens / diagnostic counts", () => {
		assertValid(
			commandInvoked({
				...baseProps,
				command: "lsp",
				code_lens_request_count: 10,
				diagnostic_publish_count: 1,
			}),
			"lsp",
		);
	});

	it("debug: with breakpoint / step / pause / restart counts", () => {
		assertValid(
			commandInvoked({
				...baseProps,
				command: "debug",
				breakpoint_count: 2,
				step_count: 10,
				pause_count: 2,
				restart_count: 1,
				fixture_event_count: 100,
			}),
			"debug",
		);
	});

	it("rejects an unknown property on the variant (closedness preserved)", () => {
		const envelope = commandInvoked({
			...baseProps,
			command: "version",
			not_a_real_field: "nope",
		});
		expect(validator.validate(envelope).valid).toBe(false);
	});

	it("rejects a command value outside the enum", () => {
		const envelope = commandInvoked({ ...baseProps, command: "not-a-command" });
		expect(validator.validate(envelope).valid).toBe(false);
	});

	it("rejects a non-bucket duration_ms", () => {
		const envelope = commandInvoked({
			...baseProps,
			command: "version",
			duration_ms: 1,
		});
		expect(validator.validate(envelope).valid).toBe(false);
	});
});

describe("schema validation - non-command events", () => {
	it("projection_shape: full property set", () => {
		assertValid(
			{
				...baseEnvelope,
				events: [
					{
						name: "projection_shape",
						timestamp: "2026-05-08T12:00:00.000Z",
						properties: {
							projection_id: "0123456789abcdef",
							parsable: true,
							file_size: 1024,
							handlers: {
								any: false,
								init: true,
								deleted: false,
								distinct_event_names: 2,
							},
							builtin_counts: { emit: 1, partitionBy: 0 },
						},
					},
				],
			},
			"projection_shape",
		);
	});

	it("extension_activated: cli_reachable=true with cli_version", () => {
		assertValid(
			{
				...baseEnvelope,
				context: { ...baseEnvelope.context, emitter: "vscode" },
				events: [
					{
						name: "extension_activated",
						timestamp: "2026-05-08T12:00:00.000Z",
						properties: {
							editor: "vscode",
							editor_version: "1.95.2",
							cli_reachable: true,
							cli_version: "0.4.2",
							activation_duration_ms: 100,
						},
					},
				],
			},
			"extension_activated (reachable)",
		);
	});

	it("extension_activated: cli_reachable=false with cli_unreachable_reason", () => {
		assertValid(
			{
				...baseEnvelope,
				context: { ...baseEnvelope.context, emitter: "vscode" },
				events: [
					{
						name: "extension_activated",
						timestamp: "2026-05-08T12:00:00.000Z",
						properties: {
							editor: "vscode",
							editor_version: "1.95.2",
							cli_reachable: false,
							cli_unreachable_reason: "binary_not_found",
							activation_duration_ms: 100,
						},
					},
				],
			},
			"extension_activated (unreachable)",
		);
	});

	it("exception: full property set", () => {
		assertValid(
			{
				...baseEnvelope,
				context: { ...baseEnvelope.context, emitter: "vscode" },
				events: [
					{
						name: "exception",
						timestamp: "2026-05-08T12:00:00.000Z",
						properties: {
							exceptions: [
								{
									type: "RuntimeError",
									value: "things broke",
									in_app: true,
									stacktrace: { type: "raw", frames: [] },
								},
							],
							phase: "startup",
						},
					},
				],
			},
			"exception",
		);
	});
});
