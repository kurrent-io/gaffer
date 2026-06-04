# Gaffer Runtime - Go Bindings

Go bindings for the gaffer projection runtime. Wraps the NativeAOT shared library via cgo.

## Prerequisites

Build the runtime shared library first:

```sh
just runtime publish
```

## Usage

```go
import (
	"fmt"
	"log"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
)

session, err := gafferruntime.NewSession(`
    fromAll().when({
        $init() { return { count: 0 }; },
        OrderPlaced(s, e) { s.count++; return s; }
    })
`, nil)
if err != nil {
    log.Fatal(err)
}
defer session.Destroy()

if _, err := session.Feed(`{"eventType":"OrderPlaced","streamId":"order-1","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`); err != nil {
    log.Fatal(err)
}

state := session.GetState(nil)
fmt.Println(*state) // {"count":1}
```

`NewSession`'s second argument is an optional options JSON (`*string`); pass `nil` for the defaults.

## Building

```sh
just bindings go test      # run tests
just bindings go check     # run linter
just bindings go fix       # format code and apply lint fixes
```

## License

[Kurrent License v1](LICENSE)
