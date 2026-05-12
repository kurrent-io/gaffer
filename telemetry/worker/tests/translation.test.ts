import type { Envelope } from "@kurrent/gaffer-telemetry";
import { describe, expect, it } from "vitest";
import { translateEnvelope } from "../src/translation";

const baseContext: Envelope["context"] = {
	emitter: "cli",
	lib_version: "0.4.2",
	os: "linux",
	arch: "x64",
	runtime_environment: "local",
};

const baseEnvelope: Envelope = {
	schema_version: "1",
	emitter_id: "00000000-0000-0000-0000-000000000001",
	run_id: "00000000-0000-0000-0000-000000000002",
	context: baseContext,
	events: [],
};

const testSessionId = "00000000-0000-0000-0000-0000000000aa";
const testDeployedAt = "2026-05-11T00:00:00.000Z";

describe("translateEnvelope", () => {
	it("uses emitter_id as PostHog distinct_id", () => {
		const env: Envelope = {
			...baseEnvelope,
			events: [
				{
					name: "command_invoked",
					timestamp: "2026-05-08T12:00:00.000Z",
					properties: {
						command: "version",
						duration_ms: 10,
						outcome: "success",
						invoked_by: "direct",
						invoked_via: "terminal",
					},
				},
			],
		};
		const result = translateEnvelope(env, testSessionId, testDeployedAt);
		expect(result[0]?.distinct_id).toBe(env.emitter_id);
	});

	it("stamps worker_deployed_at from CF_VERSION_METADATA", () => {
		const env: Envelope = {
			...baseEnvelope,
			events: [
				{
					name: "command_invoked",
					timestamp: "2026-05-08T12:00:00.000Z",
					properties: {
						command: "version",
						duration_ms: 10,
						outcome: "success",
						invoked_by: "direct",
						invoked_via: "terminal",
					},
				},
			],
		};
		const result = translateEnvelope(env, testSessionId, testDeployedAt);
		expect(result[0]?.properties.worker_deployed_at).toBe("2026-05-11T00:00:00.000Z");
	});

	it("derives $lib from emitter", () => {
		const env: Envelope = {
			...baseEnvelope,
			context: { ...baseContext, emitter: "vscode" },
			events: [
				{
					name: "extension_activated",
					timestamp: "2026-05-08T12:00:00.000Z",
					properties: {
						editor: "vscode",
						editor_version: "1.95.2",
						cli_reachable: true,
						activation_duration_ms: 100,
					},
				},
			],
		};
		const result = translateEnvelope(env, testSessionId, testDeployedAt);
		expect(result[0]?.properties.$lib).toBe("gaffer-vscode");
	});

	it("renames `exception` event to `$exception` and `exceptions` to `$exception_list`", () => {
		const env: Envelope = {
			...baseEnvelope,
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
		};
		const result = translateEnvelope(env, testSessionId, testDeployedAt);
		expect(result[0]?.event).toBe("$exception");
		expect(result[0]?.properties.$exception_list).toHaveLength(1);
		expect(result[0]?.properties.exceptions).toBeUndefined();
	});

	it("lifts os/arch to $set_once on the first event of the batch only", () => {
		const env: Envelope = {
			...baseEnvelope,
			events: [
				{
					name: "command_invoked",
					timestamp: "2026-05-08T12:00:00.000Z",
					properties: {
						command: "dev",
						duration_ms: 1000,
						outcome: "success",
						invoked_by: "direct",
						invoked_via: "terminal",
					},
				},
				{
					name: "projection_shape",
					timestamp: "2026-05-08T12:00:01.000Z",
					properties: {
						projection_id: "abc123",
						parsable: true,
						file_size: 1024,
						handlers: { any: false, init: false, deleted: false, distinct_event_names: 1 },
						builtin_counts: {},
					},
				},
			],
		};
		const result = translateEnvelope(env, testSessionId, testDeployedAt);
		expect(result[0]?.properties.$set_once).toMatchObject({ os: "linux", arch: "x64" });
		expect(result[1]?.properties.$set_once).toBeUndefined();
		expect(result[1]?.properties.$set).toBeUndefined();
	});

	it("fans out manifest_features_used into per-section booleans", () => {
		const env: Envelope = {
			...baseEnvelope,
			events: [
				{
					name: "command_invoked",
					timestamp: "2026-05-08T12:00:00.000Z",
					properties: {
						command: "manifest",
						duration_ms: 100,
						outcome: "success",
						invoked_by: "direct",
						invoked_via: "terminal",
						manifest_features_used: ["projections", "fixtures"],
					},
				},
			],
		};
		const result = translateEnvelope(env, testSessionId, testDeployedAt);
		expect(result[0]?.properties.manifest_has_projections).toBe(true);
		expect(result[0]?.properties.manifest_has_fixtures).toBe(true);
		expect(result[0]?.properties.manifest_features_used).toEqual(["projections", "fixtures"]);
	});

	it("fans out projection_errors_seen into per-class booleans", () => {
		const env: Envelope = {
			...baseEnvelope,
			events: [
				{
					name: "command_invoked",
					timestamp: "2026-05-08T12:00:00.000Z",
					properties: {
						command: "dev",
						duration_ms: 1000,
						outcome: "user_interrupt",
						invoked_by: "direct",
						invoked_via: "terminal",
						projection_errors_seen: ["projection_compile_error", "projection_user_throw"],
					},
				},
			],
		};
		const result = translateEnvelope(env, testSessionId, testDeployedAt);
		expect(result[0]?.properties.saw_projection_compile_error).toBe(true);
		expect(result[0]?.properties.saw_projection_user_throw).toBe(true);
		expect(result[0]?.properties.projection_errors_seen).toEqual(["projection_compile_error", "projection_user_throw"]);
	});

	it("flattens projection_shape handlers and builtin_counts", () => {
		const env: Envelope = {
			...baseEnvelope,
			events: [
				{
					name: "projection_shape",
					timestamp: "2026-05-08T12:00:00.000Z",
					properties: {
						projection_id: "abc123",
						parsable: true,
						file_size: 5120,
						handlers: { any: true, init: false, deleted: true, distinct_event_names: 10 },
						builtin_counts: {
							fromAll: 1,
							when: 10,
							partitionBy: 1,
							linkStreamTo: 1,
							chainHandlers: 1,
						},
					},
				},
			],
		};
		const result = translateEnvelope(env, testSessionId, testDeployedAt);
		const props = result[0]!.properties;
		expect(props.event_catchall_handler).toBe(true);
		expect(props.init_handler).toBe(false);
		expect(props.deleted_handler).toBe(true);
		expect(props.distinct_event_handler_count).toBe(10);
		expect(props.builtin_fromAll_count).toBe(1);
		expect(props.builtin_when_count).toBe(10);
		expect(props.builtin_partitionBy_count).toBe(1);
		expect(props.builtin_linkStreamTo_count).toBe(1);
		expect(props.builtin_chainHandlers_count).toBe(1);
		expect(props.handlers).toBeUndefined();
		expect(props.builtin_counts).toBeUndefined();
	});

	it("forwards project_id as a per-event property on every event", () => {
		// Project_id should land on every event in the batch, not
		// just the first. Lifted-once identity properties ($set /
		// $set_once) deliberately fire on event[0] only; project_id
		// is per-event so dashboards can group / filter the
		// projection_shape event alongside its command_invoked
		// envelope-mate.
		const env: Envelope = {
			...baseEnvelope,
			context: {
				...baseContext,
				project_id: "cd3c08fa1f4183d7",
			},
			events: [
				{
					name: "command_invoked",
					timestamp: "2026-05-08T12:00:00.000Z",
					properties: {
						command: "dev",
						duration_ms: 10,
						outcome: "success",
						invoked_by: "direct",
						invoked_via: "terminal",
					},
				},
				{
					name: "projection_shape",
					timestamp: "2026-05-08T12:00:01.000Z",
					properties: {
						projection_id: "abc123",
						parsable: true,
						file_size: 1024,
						handlers: { any: false, init: false, deleted: false, distinct_event_names: 1 },
						builtin_counts: {},
					},
				},
			],
		};
		const result = translateEnvelope(env, testSessionId, testDeployedAt);
		expect(result[0]?.properties.project_id).toBe("cd3c08fa1f4183d7");
		expect(result[1]?.properties.project_id).toBe("cd3c08fa1f4183d7");
	});

	it("omits project_id when the producer wasn't in a project", () => {
		const env: Envelope = {
			...baseEnvelope,
			events: [
				{
					name: "command_invoked",
					timestamp: "2026-05-08T12:00:00.000Z",
					properties: {
						command: "version",
						duration_ms: 10,
						outcome: "success",
						invoked_by: "direct",
						invoked_via: "terminal",
					},
				},
			],
		};
		const result = translateEnvelope(env, testSessionId, testDeployedAt);
		expect(result[0]?.properties.project_id).toBeUndefined();
	});

	it("never forwards invoker_id even when present", () => {
		const env: Envelope = {
			...baseEnvelope,
			context: {
				...baseContext,
				invoker_id: "00000000-0000-0000-0000-00000000abcd",
			},
			events: [
				{
					name: "command_invoked",
					timestamp: "2026-05-08T12:00:00.000Z",
					properties: {
						command: "dev",
						duration_ms: 10,
						outcome: "success",
						invoked_by: "vscode",
						invoked_via: "code_lens",
					},
				},
			],
		};
		const result = translateEnvelope(env, testSessionId, testDeployedAt);
		expect(result[0]?.properties.invoker_id).toBeUndefined();
	});

	it("includes runtime_environment as a per-event property (not lifted)", () => {
		const env: Envelope = {
			...baseEnvelope,
			context: { ...baseContext, runtime_environment: "ci" },
			events: [
				{
					name: "command_invoked",
					timestamp: "2026-05-08T12:00:00.000Z",
					properties: {
						command: "dev",
						duration_ms: 100,
						outcome: "success",
						invoked_by: "direct",
						invoked_via: "terminal",
					},
				},
			],
		};
		const result = translateEnvelope(env, testSessionId, testDeployedAt);
		expect(result[0]?.properties.runtime_environment).toBe("ci");
		expect((result[0]?.properties.$set_once as Record<string, unknown>).runtime_environment).toBeUndefined();
	});
});
