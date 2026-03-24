/**
 * Gaffer Runtime - Projection runtime for KurrentDB
 *
 * A puppetable projection runtime that executes KurrentDB projections via Jint.
 * Callers feed JS source and events, register callbacks, and query state.
 *
 * Thread safety: Sessions are NOT thread-safe. Do not feed events or query
 * state from multiple threads concurrently on the same session.
 *
 * Memory: All returned strings are owned by the runtime and valid until the
 * next API call on the same session (or globally for gaffer_get_last_error).
 * Callers must copy if they need to keep the data. Callback string arguments
 * are valid for the duration of the callback only.
 *
 * Error handling: On failure, functions return NULL/non-zero and store
 * structured error JSON retrievable via gaffer_get_last_error(). The JSON
 * has a "code" field (kebab-case) and "description" field, plus error-specific
 * fields. Error codes: "invalid-projection", "compilation-timeout",
 * "invalid-argument", "handler-error", "execution-timeout", "malformed-event",
 * "state-serialization-error", "projection-transform-error", "unexpected".
 */

#ifndef GAFFER_H
#define GAFFER_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

/** Opaque session handle. */
typedef struct gaffer_session gaffer_session;

/* --------------------------------------------------------------------------
 * Session lifecycle
 * -------------------------------------------------------------------------- */

/**
 * Create a new projection session.
 *
 * Compiles and validates the projection JS source. Returns NULL on error.
 * Call gaffer_get_last_error() for structured error JSON on failure.
 *
 * @param source       Projection JavaScript source code (UTF-8)
 * @param options_json JSON options string, or NULL for defaults.
 *                     Supported fields:
 *                       - "version": "v1" | "v2" (default "v2")
 *                       - "compilationTimeoutMs": int (default 5000)
 *                       - "executionTimeoutMs": int (default 5000)
 *                       - "enableContentTypeValidation": bool (default false)
 *                       - "debug": bool (default false)
 * @return Session handle, or NULL on error
 */
gaffer_session* gaffer_session_create(const char* source, const char* options_json);

/**
 * Destroy a session and free all associated memory.
 * After this call, the session handle is invalid.
 */
void gaffer_session_destroy(gaffer_session* session);

/* --------------------------------------------------------------------------
 * Callbacks
 *
 * Register before feeding events. All callbacks are synchronous - the runtime
 * blocks until the callback returns. String arguments are valid for the
 * duration of the callback only; copy if needed.
 * -------------------------------------------------------------------------- */

/** Callback for emitted events (both emit() and linkTo()). */
typedef void (*gaffer_emit_cb)(
    const char* stream_id,
    const char* event_type,
    const char* data,
    const char* metadata_json,
    int is_json,
    int is_link,
    void* user_data
);

/** Callback for console.log output from the projection. */
typedef void (*gaffer_log_cb)(
    const char* message,
    void* user_data
);

/** Callback when projection state changes after processing an event. */
typedef void (*gaffer_state_changed_cb)(
    const char* partition,
    const char* state_json,
    void* user_data
);

/**
 * Register a callback for emitted events.
 * Check event_type for "$>" to distinguish linkTo from emit.
 */
void gaffer_on_emit(gaffer_session* session, gaffer_emit_cb cb, void* user_data);

/** Register a callback for console.log output. */
void gaffer_on_log(gaffer_session* session, gaffer_log_cb cb, void* user_data);

/** Register a callback for state changes. */
void gaffer_on_state_changed(gaffer_session* session, gaffer_state_changed_cb cb, void* user_data);

/* --------------------------------------------------------------------------
 * Event feeding
 * -------------------------------------------------------------------------- */

/**
 * Feed a single event to the projection. Returns the step result as JSON.
 *
 * Blocks until the event is fully processed. Handles $streamDeleted and
 * soft delete ($metadata on $$stream with $tb=long.MaxValue) internally,
 * routing to the $deleted handler if defined.
 *
 * @param session    Session handle
 * @param event_json JSON event string (UTF-8). Required fields:
 *                     "eventType", "streamId", "sequenceNumber",
 *                     "isJson", "eventId", "created"
 *                   Optional: "data", "metadata", "linkMetadata"
 * @return JSON string with step result. Valid until next API call on this session.
 *         NULL on error - call gaffer_get_last_error() for details.
 *
 *         Result shapes:
 *           {"status":"skipped","reason":"link|non-json|no-partition|unhandled|no-delete-handler"}
 *           {"status":"processed","partition":"...","state":...,"result":...,
 *            "sharedState":...,"emitted":[...],"logs":[...]}
 */
const char* gaffer_session_feed(gaffer_session* session, const char* event_json);

/* --------------------------------------------------------------------------
 * State
 * -------------------------------------------------------------------------- */

/**
 * Get current state for a partition.
 * @param partition Partition key, or NULL for the default (unpartitioned) state.
 * @return State JSON string, or NULL if the partition has not been seen.
 *         Valid until the next API call on this session.
 */
const char* gaffer_session_get_state(gaffer_session* session, const char* partition);

/**
 * Get shared state for biState projections.
 * @return Shared state JSON, or NULL if not a biState projection or no
 *         events have been processed. Valid until the next API call.
 */
const char* gaffer_session_get_shared_state(gaffer_session* session);

/**
 * Restore state for a partition (e.g. from a cache).
 * @param partition  Partition key, or NULL for the default.
 * @param state_json JSON state string to restore.
 */
void gaffer_session_set_state(gaffer_session* session, const char* partition, const char* state_json);

/**
 * Get the transformed result for a partition (applies transformBy/filterBy).
 * @param partition Partition key, or NULL for the default.
 * @return Result JSON, or NULL if the partition is unknown or filtered out.
 *         Valid until the next API call.
 */
const char* gaffer_session_get_result(gaffer_session* session, const char* partition);

/**
 * Get the source definition - what events/streams the projection reads.
 * @return JSON object describing the projection's source configuration.
 *         Valid until the next API call.
 */
const char* gaffer_session_get_sources(gaffer_session* session);

/**
 * Get the partition key that would be computed for an event.
 * Only meaningful for projections using partitionBy or foreachStream.
 * @param event_json JSON event string (same shape as gaffer_session_feed).
 * @return Partition key string, or NULL. Valid until the next API call.
 */
const char* gaffer_session_get_partition_key(gaffer_session* session, const char* event_json);

/* --------------------------------------------------------------------------
 * Error handling
 * -------------------------------------------------------------------------- */

/**
 * Get the last error as structured JSON.
 *
 * Returns NULL if the last operation succeeded. The returned string is
 * valid until the next API call on the same thread.
 *
 * @return Error JSON string, or NULL if no error.
 */
const char* gaffer_get_last_error(void);

#ifdef __cplusplus
}
#endif

#endif /* GAFFER_H */
