# Gaffer Runtime - Go Bindings

Go bindings for the gaffer projection runtime. Wraps the NativeAOT shared library via cgo.

## Prerequisites

Build the runtime shared library first:

```sh
just runtime publish
```

## Usage

```go
import gafferruntime "github.com/kurrent-io/gaffer/bindings/go"

session := gafferruntime.SessionCreate(`
    fromAll().when({
        $init() { return { count: 0 }; },
        OrderPlaced(s, e) { s.count++; return s; }
    })
`, nil)
defer gafferruntime.SessionDestroy(session)

gafferruntime.SessionFeed(session, `{"eventType":"OrderPlaced","streamId":"order-1","data":"{}"}`)

state := gafferruntime.SessionGetState(session, nil)
fmt.Println(*state) // {"count":1}
```

## Building

```sh
just bindings go test      # run tests
just bindings go check     # run linter
just bindings go fix       # format code and apply lint fixes
```

## License

[Kurrent License v1](LICENSE)
