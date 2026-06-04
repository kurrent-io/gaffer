# Gaffer Runtime

Projection runtime for KurrentDB. Executes projection JavaScript locally via [Jint](https://github.com/sebastienros/jint), the same interpreter KurrentDB uses.

## What it does

- Runs KurrentDB projection JS (fromAll, when, emit, linkTo, foreachStream, partitionBy, biState, etc.)
- Manages per-partition state
- Handles stream deletion (hard and soft deletes)
- Exposes callbacks for emitted events, log output, state changes, and slow handler warnings
- Builds as a NativeAOT shared library with C API exports for FFI

## C# usage

```csharp
using Gaffer.Runtime;
using Gaffer.Runtime.Events;

using var session = new ProjectionSession("""
    fromAll().when({
        $init() { return { count: 0 }; },
        OrderPlaced(s, e) { s.count++; return s; }
    })
""", new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

session.OnEmit = e => Console.WriteLine($"Emitted: {e.EventType} -> {e.StreamId}");

session.Feed(new ProjectionEvent {
    EventType = "OrderPlaced",
    StreamId = "order-123",
    Data = """{"amount": 99.99}""",
});

Console.WriteLine(session.GetState()); // {"count":1}
```

## Building

```sh
just runtime build         # build
just runtime test          # run tests
just runtime publish       # NativeAOT shared library
just runtime check         # verify formatting
just runtime fix           # auto-fix formatting
just runtime clean         # remove build artifacts
```

## License

[Kurrent License v1](LICENSE)
