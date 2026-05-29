---
"@kurrent/projections-testing": minor
---

The `TestEvent` schema now rejects inputs KurrentDB could never deliver to a handler: `eventType` and `streamId` must be non-empty strings, and `sequenceNumber` must be a non-negative integer. Previously a unit test could pass against events the projection would never see in production, such as a negative sequence number or an empty stream id.
