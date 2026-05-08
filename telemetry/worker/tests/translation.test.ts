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
		const result = translateEnvelope(env, "phc_test");
		expect(result.batch[0]?.distinct_id).toBe(env.emitter_id);
	});

	it("derives $lib from emitter", () => {
		const env: Envelope = {
			...baseEnvelope,
			context: { ...baseContext, emitter: "extension" },
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
		const result = translateEnvelope(env, "phc_test");
		expect(result.batch[0]?.properties.$lib).toBe("gaffer-extension");
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
		const result = translateEnvelope(env, "phc_test");
		expect(result.batch[0]?.event).toBe("$exception");
		expect(result.batch[0]?.properties.$exception_list).toHaveLength(1);
		expect(result.batch[0]?.properties.exceptions).toBeUndefined();
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
		const result = translateEnvelope(env, "phc_test");
		expect(result.batch[0]?.properties.$set_once).toMatchObject({ os: "linux", arch: "x64" });
		expect(result.batch[1]?.properties.$set_once).toBeUndefined();
		expect(result.batch[1]?.properties.$set).toBeUndefined();
	});

	it("includes install_date and first_seen_lib_version in $set_once when present", () => {
		const env: Envelope = {
			...baseEnvelope,
			context: { ...baseContext, install_date: "2026-05-08" },
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
		const result = translateEnvelope(env, "phc_test");
		expect(result.batch[0]?.properties.$set_once).toMatchObject({
			os: "linux",
			arch: "x64",
			install_date: "2026-05-08",
			first_seen_lib_version: "0.4.2",
		});
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
		const result = translateEnvelope(env, "phc_test");
		expect(result.batch[0]?.properties.manifest_has_projections).toBe(true);
		expect(result.batch[0]?.properties.manifest_has_fixtures).toBe(true);
		expect(result.batch[0]?.properties.manifest_features_used).toEqual(["projections", "fixtures"]);
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
						projection_errors_seen: ["projection_user_throw", "projection_type_error"],
					},
				},
			],
		};
		const result = translateEnvelope(env, "phc_test");
		expect(result.batch[0]?.properties.saw_projection_user_throw).toBe(true);
		expect(result.batch[0]?.properties.saw_projection_type_error).toBe(true);
		expect(result.batch[0]?.properties.projection_errors_seen).toEqual([
			"projection_user_throw",
			"projection_type_error",
		]);
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
							// camelToSnake edge case: multiple capitals in a row.
							linkStreamTo: 1,
							chainHandlers: 1,
						},
					},
				},
			],
		};
		const result = translateEnvelope(env, "phc_test");
		const props = result.batch[0]!.properties;
		expect(props.event_catchall_handler).toBe(true);
		expect(props.init_handler).toBe(false);
		expect(props.deleted_handler).toBe(true);
		expect(props.distinct_event_handler_count).toBe(10);
		expect(props.builtin_from_all_count).toBe(1);
		expect(props.builtin_when_count).toBe(10);
		expect(props.builtin_partition_by_count).toBe(1);
		expect(props.builtin_link_stream_to_count).toBe(1);
		expect(props.builtin_chain_handlers_count).toBe(1);
		expect(props.handlers).toBeUndefined();
		expect(props.builtin_counts).toBeUndefined();
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
		const result = translateEnvelope(env, "phc_test");
		expect(result.batch[0]?.properties.invoker_id).toBeUndefined();
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
		const result = translateEnvelope(env, "phc_test");
		expect(result.batch[0]?.properties.runtime_environment).toBe("ci");
		expect((result.batch[0]?.properties.$set_once as Record<string, unknown>).runtime_environment).toBeUndefined();
	});
});
