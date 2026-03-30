/// <reference path="../projections.d.ts" />

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
