/// <reference path="../src/projections.d.ts" />

// --- log ---

// Valid: variadic, any args
log("message");
log("event", "OrderPlaced", 42);
log({ complex: "object" });

// --- emit ---

// Valid: with metadata
emit("stream-1", "OrderPlaced", { orderId: "123" }, { userId: "user-1" });

// Valid: without metadata
emit("stream-1", "OrderPlaced", { orderId: "123" });

// @ts-expect-error emit requires at least streamId, eventType, eventBody
emit("stream-1", "OrderPlaced");

// --- linkTo ---

// Valid: with metadata
declare const ev: Projection.KurrentEvent;
linkTo("target-stream", ev, { source: "my-projection" });

// Valid: without metadata
linkTo("target-stream", ev);

// @ts-expect-error linkTo requires event object, not just a string
linkTo("target-stream", "not-an-event");

// --- linkStreamTo ---

// Valid: with metadata
linkStreamTo("target", "source", { reason: "audit" });

// Valid: without metadata
linkStreamTo("target", "source");

// --- copyTo ---

// Valid: 3 args
copyTo("target-stream", "EventType", { data: true });

// @ts-expect-error copyTo requires 3 args
copyTo("target-stream", "EventType");

// --- options ---

// Valid: all options
options({
	$includeLinks: true,
	resultStreamName: "my-results",
	reorderEvents: true,
	processingLag: 500,
	biState: true,
});

// Valid: empty options
options({});

// @ts-expect-error resultStreamName must be string, not number
options({ resultStreamName: 42 });

// @ts-expect-error $includeLinks must be boolean
options({ $includeLinks: "yes" });

// @ts-expect-error processingLag must be number
options({ processingLag: "500" });

// @ts-expect-error unknown option key
options({ unknownOption: true });
