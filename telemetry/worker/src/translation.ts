// Translates a validated gaffer telemetry envelope into PostHog's /batch
// payload shape.
//
// The wire format is provider-neutral by design. Anything PostHog-specific
// (reserved property names like `$exception`, `$lib_version`, person-property
// lifting via `$set`/`$set_once`, array fan-out, object flattening) lives
// here, not in the schema.

import type {
	CommandInvoked,
	Envelope,
	Event,
	Exception,
	ExtensionActivated,
	ProjectionShape,
} from "@kurrent/gaffer-telemetry";

export interface PostHogBatchPayload {
	api_key: string;
	batch: PostHogEvent[];
}

interface PostHogEvent {
	event: string;
	distinct_id: string;
	timestamp: string;
	properties: Record<string, unknown>;
}

export function translateEnvelope(envelope: Envelope, apiKey: string, sessionId: string): PostHogBatchPayload {
	const { emitter_id, run_id, context, events } = envelope;
	const $lib = `gaffer-${context.emitter}`;

	const batch: PostHogEvent[] = events.map((rawEvent, i) => {
		const event = rawEvent as Event;
		const props: Record<string, unknown> = {
			$lib,
			$lib_version: context.lib_version,
			// Worker-stamped session id; not sent by the client.
			$session_id: sessionId,
			// Suppress PostHog's IP-based geo-resolution. The worker's egress
			// IP would otherwise be attached to every event.
			$ip: null,
			runtime_environment: context.runtime_environment,
			run_id,
			emitter: context.emitter,
			...translateEventProperties(event),
		};

		// Person-property lifting on the first event of the batch.
		if (i === 0) {
			props.$set = { lib_version: context.lib_version };
			const setOnce: Record<string, unknown> = {
				os: context.os,
				arch: context.arch,
			};
			if (context.install_date) {
				setOnce.install_date = context.install_date;
				setOnce.first_seen_lib_version = context.lib_version;
			}
			props.$set_once = setOnce;
		}

		return {
			event: translateEventName(event.name),
			distinct_id: emitter_id,
			timestamp: event.timestamp,
			properties: props,
		};
	});

	return { api_key: apiKey, batch };
}

function translateEventName(name: string): string {
	if (name === "exception") return "$exception";
	return name;
}

function translateEventProperties(event: Event): Record<string, unknown> {
	switch (event.name) {
		case "command_invoked":
			return translateCommandInvokedProperties((event as CommandInvoked).properties);
		case "projection_shape":
			return translateProjectionShapeProperties((event as ProjectionShape).properties);
		case "extension_activated":
			return { ...(event as ExtensionActivated).properties };
		case "exception":
			return translateExceptionProperties((event as Exception).properties);
		default: {
			// Schema validation should reject unknown names before this runs, but
			// guard defensively so a future event variant landing in CUE without
			// updating this switch doesn't crash the worker.
			const _exhaustive: never = event;
			throw new Error(`unhandled event variant: ${JSON.stringify(_exhaustive)}`);
		}
	}
}

function translateCommandInvokedProperties(props: CommandInvoked["properties"]): Record<string, unknown> {
	const { manifest_features_used, projection_errors_seen, ...rest } = props as typeof props & {
		manifest_features_used?: string[];
		projection_errors_seen?: string[];
	};
	const out: Record<string, unknown> = { ...rest };

	// Array fan-out: keep the array around for ad-hoc queries, but also emit
	// flat booleans so the no-code Insights builder can pivot on them.
	if (manifest_features_used) {
		out.manifest_features_used = manifest_features_used;
		for (const section of manifest_features_used) {
			out[`manifest_has_${section}`] = true;
		}
	}
	if (projection_errors_seen) {
		out.projection_errors_seen = projection_errors_seen;
		for (const outcome of projection_errors_seen) {
			out[`saw_${outcome}`] = true;
		}
	}

	return out;
}

function translateProjectionShapeProperties(props: ProjectionShape["properties"]): Record<string, unknown> {
	const { handlers, builtin_counts, ...rest } = props;
	const out: Record<string, unknown> = { ...rest };

	if (handlers) {
		out.event_catchall_handler = handlers.any;
		out.init_handler = handlers.init;
		out.deleted_handler = handlers.deleted;
		out.distinct_event_handler_count = handlers.distinct_event_names;
	}

	if (builtin_counts) {
		for (const [apiName, count] of Object.entries(builtin_counts as Record<string, number>)) {
			out[`builtin_${camelToSnake(apiName)}_count`] = count;
		}
	}

	return out;
}

function translateExceptionProperties(props: Exception["properties"]): Record<string, unknown> {
	const { exceptions, ...rest } = props;
	return { ...rest, $exception_list: exceptions };
}

function camelToSnake(s: string): string {
	return s.replace(/[A-Z]/g, (c) => `_${c.toLowerCase()}`);
}
