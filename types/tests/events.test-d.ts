/// <reference path="../src/projections.d.ts" />

// Verify event property types via handler access
fromAll().when({
	$any: (_state, event) => {
		// Required string properties
		const _streamId: string = event.streamId;
		const _eventType: string = event.eventType;
		const _partition: string = event.partition;
		const _category: string = event.category;
		const _eventId: string = event.eventId;
		const _created: string = event.created;

		// Required number
		const _seqNum: number = event.sequenceNumber;

		// Required boolean
		const _isJson: boolean = event.isJson;

		// Nullable strings
		const _bodyRaw: string | null = event.bodyRaw;
		const _metadataRaw: string | null = event.metadataRaw;

		// Optional + nullable objects
		const _body: Projection.EventBody | null | undefined = event.body;
		const _data: Projection.EventBody | null | undefined = event.data;
		const _metadata: Projection.EventMetadata | null | undefined = event.metadata;
		const _linkMeta: Projection.EventMetadata | null | undefined = event.linkMetadata;
		const _linkMetaRaw: string | null | undefined = event.linkMetadataRaw;

		// @ts-expect-error bodyRaw is string | null, not just string
		const _bodyRawStrict: string = event.bodyRaw;

		// @ts-expect-error metadataRaw is string | null, not just string
		const _metaRawStrict: string = event.metadataRaw;

		return _state;
	},
});

// --- KurrentEvent<TBody> narrowing for standalone handlers ---

// Valid: parameterise event body shape for a standalone helper / test
// fixture. body and data are then `TBody | null` rather than the wider
// `EventBody | null`.
type OrderPlacedBody = { orderId: string; cents: number };
function totalForOrder(event: Projection.KurrentEvent<OrderPlacedBody>): number {
	return event.body?.cents ?? 0;
}
declare const _orderEvent: Projection.KurrentEvent<OrderPlacedBody>;
const _ordersTotal: number = totalForOrder(_orderEvent);

// Valid: the legacy `data` alias narrows the same way
declare const _orderEvent2: Projection.KurrentEvent<OrderPlacedBody>;
// eslint-disable-next-line @typescript-eslint/no-deprecated
const _ordersTotalViaData: number = _orderEvent2.data?.cents ?? 0;

// @ts-expect-error narrowed body doesn't expose unrelated fields
const _missingField: string = _orderEvent.body?.unknownField ?? "";

// Without a generic, body falls back to EventBody and field access is unknown
declare const _untypedEvent: Projection.KurrentEvent;
const _untypedField: unknown = _untypedEvent.body?.anything;
