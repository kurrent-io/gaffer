# Binding test fixtures

Shared test cases for validating FFI bindings against the runtime. Each binding
(Go, JS) loads these fixtures and runs them through its own test runner.

## Schema

Each fixture file is a JSON array of test cases:

```json
{
  "name": "descriptive test name",
  "source": "fromAll().when({ ... })",
  "options": { "compilationTimeoutMs": 10000 },
  "events": [
    { "eventType": "Ping", "streamId": "s-1", "sequenceNumber": 0, "data": "{}", "isJson": true, "eventId": "00000000-0000-0000-0000-000000000000", "created": "2026-01-01T00:00:00Z" }
  ],
  "expect": {
    "valid": true,
    "sources": { "allStreams": true, "streams": null, ... },
    "state": { "count": 1 },
    "states": { "partition-1": { "items": 2 } },
    "sharedState": { "total": 30 },
    "result": { "total": 2 },
    "emitted": [{ "streamId": "out", "eventType": "Notified", "data": "{}" }],
    "logs": ["hello"],
    "error": "boom"
  }
}
```

All fields in `expect` are optional. Only specified fields are checked.

- `valid: false` - expect compilation to fail (no events fed)
- `sources` - raw `ProjectionInfo` shape from the runtime SDK (camelCase keys)
- `state` - state after feeding all events (default partition)
- `states` - per-partition state (key is partition name)
- `sharedState` - biState shared state after all events
- `result` - transformed result after all events
- `emitted` - emitted events from the last event fed
- `logs` - log messages from the last event fed
- `error` - substring match on error from the last event fed

`options` and `events` are optional. If `events` is omitted, only validation is tested.
