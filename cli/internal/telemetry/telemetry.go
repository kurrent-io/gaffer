package telemetry

import (
	"sync"
	"time"
)

// Client owns the sink and the goroutines in flight for a single CLI
// process. main.go constructs one with telemetry.New(...) and stores it on
// the root context; the generated helpers read it off ctx at emit time.
//
// Lifecycle: emits are asynchronous (one goroutine per envelope, tracked
// by an internal WaitGroup). Call Flush exactly once at process exit.
// After Flush returns, the Client is closed: further emits become silent
// no-ops. Flush is idempotent. emit and Flush are safe to call
// concurrently from multiple goroutines.
type Client struct {
	sink           Sink
	perSendTimeout time.Duration

	// mu guards closed and serialises wg.Add against close+wait. Use
	// tryAdd to enter the in-flight section.
	mu     sync.Mutex
	closed bool
	wg     sync.WaitGroup

	// errLog receives in-flight transport / sink errors. Defaults to a
	// no-op; tests and the GAFFER_TELEMETRY_DEBUG=1 path (commit 4)
	// inject their own.
	errLog func(error)

	// httpSink construction inputs; only used when the caller did not
	// supply their own Sink via WithSink.
	workerURL string
	userAgent string
}

// Option mutates a Client at construction.
type Option func(*Client)

// WithSink replaces the default httpSink with a caller-provided sink.
// Primarily for tests and for wrapping the default sink in a decorator.
func WithSink(s Sink) Option {
	return func(c *Client) { c.sink = s }
}

// WithWorkerURL overrides the default production worker URL. Useful for
// staging deployments and integration tests that point at a local server.
// Has no effect when combined with WithSink.
func WithWorkerURL(url string) Option {
	return func(c *Client) { c.workerURL = url }
}

// WithUserAgent overrides the default User-Agent header on outgoing
// requests. Wire the gaffer release version here so the worker can
// attribute traffic per release. Has no effect when combined with
// WithSink.
func WithUserAgent(ua string) Option {
	return func(c *Client) { c.userAgent = ua }
}

// WithPerSendTimeout sets the per-send context deadline. Defaults to 2
// seconds. Match Flush's caller-supplied ctx deadline against this value
// so the per-send budget can actually elapse before Flush gives up.
func WithPerSendTimeout(d time.Duration) Option {
	return func(c *Client) { c.perSendTimeout = d }
}

// WithErrorLogger overrides the no-op default destination for in-flight
// transport / sink errors. The CLI never surfaces telemetry failures to
// the user by default; this is for tests and for GAFFER_TELEMETRY_DEBUG=1
// transparency mode (commit 4).
func WithErrorLogger(f func(error)) Option {
	return func(c *Client) { c.errLog = f }
}

// New constructs a Client. With no options it uses the production httpSink
// pointed at DefaultWorkerURL with a 2-second per-send deadline.
func New(opts ...Option) *Client {
	c := &Client{
		perSendTimeout: 2 * time.Second,
		errLog:         func(error) {}, // silent by default
		workerURL:      DefaultWorkerURL,
		userAgent:      defaultUserAgent,
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.sink == nil {
		c.sink = newHTTPSink(c.workerURL, c.userAgent)
	}
	return c
}
