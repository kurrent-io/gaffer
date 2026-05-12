package telemetry

import (
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// EnvDebug is the env var that flips Client into transparency mode:
// every envelope is written to stderr as JSON before forwarding to
// the sink. Read once at New() so `export GAFFER_TELEMETRY_DEBUG=1`
// mid-process has no effect; gaffer is a per-command CLI so this is
// the natural read point. Matches the disclosure promised in
// TELEMETRY.md.
const EnvDebug = "GAFFER_TELEMETRY_DEBUG"

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

	// errLog receives in-flight transport / sink errors. Defaults to
	// a no-op; tests and the GAFFER_TELEMETRY_DEBUG=1 debug-tee
	// path inject their own.
	errLog func(error)

	// httpSink construction inputs; only used when the caller did not
	// supply their own Sink via WithSink.
	workerURL string
	userAgent string

	// libVersion is the gaffer release semver stamped onto every
	// envelope's Context.LibVersion. Distinct from userAgent which
	// also embeds OS / arch / Go version; libVersion is just the
	// gaffer-side version that release tooling sets via ldflags.
	libVersion string

	// identity is the resolved per-install identity stamped onto
	// outgoing envelopes. Set by StartupGate at construction; the
	// zero value means "no identity available" - emit helpers should
	// skip in that case rather than send anonymous envelopes the
	// worker would reject.
	identity Identity

	// projectID is the wire-format project_id stamped on every
	// envelope's Context. Computed once in StartupGate from the
	// identity salt + the project root that main.go resolved via
	// project.FindRootFrom; empty when the process started outside a
	// gaffer project, in which case buildEnvelope omits the field.
	// Treated as immutable post-construction. Long-running surfaces
	// (lsp, mcp) carry the launch-cwd's value for the whole session;
	// per-request resolution is future work when those surfaces gain
	// project-aware telemetry.
	projectID string

	// invocation carries values parsed from the hidden root flags
	// --invoker-id / --invoked-by / --invoked-via. Set at Client
	// construction from main.go's pre-cobra argv peek (the flags
	// drive notice suppression during identity mint, which needs to
	// know before EnsureIdentity runs). Zero values mean the flag
	// wasn't passed; stamp helpers fall through to command-aware
	// defaults in that case.
	//
	// Treat as immutable post-construction. Reads happen from emit
	// goroutines; the lack of mutex is safe only because no code
	// path writes after New returns.
	invocation Invocation

	// shapeCache dedupes projection_shape envelopes: only emit when
	// a projection is first seen or its content_hash drifts. Keyed
	// by hashed projection_id, value is the last-observed content
	// hash. Bounded to shapeCacheCap entries via FIFO eviction so a
	// long LSP session across a monorepo doesn't grow it
	// unbounded. Mutex-guarded because LSP / dev / MCP can call
	// into EmitProjectionShape from request goroutines.
	shapeMu    sync.Mutex
	shapeCache map[string][32]byte
	shapeOrder []string

	// currentCommand records which cobra command is in flight, set
	// by Begin*/Emit* helpers at entry. EmitException reads it to
	// stamp Exception.command so a panic envelope can be attributed
	// to the command that triggered it. atomic.Value (rather than a
	// mutex on the string) because reads happen from the main goroutine
	// inside the panic-recover defer, while writes happen from the
	// same goroutine slightly earlier; the atomic gives a clean
	// happens-before for free and is overkill insurance.
	currentCommand atomic.Value // holds CommandName

	// startTime is captured at construction (process startup) and
	// used to compute duration_ms on command_invoked envelopes. The
	// RawDuration bucket math (in events.gen.go) collapses sub-10ms
	// runs to the 0 bucket, so clock skew is irrelevant.
	startTime time.Time
}

// Option mutates a Client at construction.
type Option func(*Client)

// WithSink replaces the default httpSink with a caller-provided sink.
// Primarily for tests and for wrapping the default sink in a decorator.
//
// The GAFFER_TELEMETRY_DEBUG=1 debug-tee still wraps a caller-injected
// sink. Tests that want quiet output unset the env var (t.Setenv(EnvDebug, "")).
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
// the user by default; this is for tests and for the
// GAFFER_TELEMETRY_DEBUG=1 transparency-tee path.
func WithErrorLogger(f func(error)) Option {
	return func(c *Client) { c.errLog = f }
}

// WithLibVersion stamps the gaffer release semver onto Context.LibVersion
// of every emitted envelope. main.go passes cmd.Version; tests usually
// don't set it (defaults to empty).
func WithLibVersion(v string) Option {
	return func(c *Client) { c.libVersion = v }
}

// WithIdentity stamps id onto the Client. Set by StartupGate in production;
// integration tests in telemetrytest use it to inject a fixed identity
// without going through the full mint flow.
func WithIdentity(id Identity) Option {
	return func(c *Client) { c.identity = id }
}

// WithInvocation stamps the spawn-linkage values onto the Client. Wired
// from main.go's argv peek of the hidden root flags --invoker-id /
// --invoked-by / --invoked-via. Empty fields mean the flag wasn't
// passed; the stamp helpers apply command-aware defaults at emit time.
func WithInvocation(inv Invocation) Option {
	return func(c *Client) { c.invocation = inv }
}

// WithProjectID stamps the salted project_id onto the Client.
// StartupGate computes this from the resolved identity's salt and the
// project root discovered via project.FindRootFrom(cwd); tests inject
// directly. Empty value means "no project" and buildEnvelope omits
// Context.ProjectID.
func WithProjectID(id string) Option {
	return func(c *Client) { c.projectID = id }
}

// setCurrentCommand stashes the in-flight cobra command name. Called
// by every Begin* / Emit* helper so a subsequent EmitException
// (typically from main.go's global panic-recover) can attribute the
// exception to the command that was running.
func (c *Client) setCurrentCommand(name CommandName) {
	c.currentCommand.Store(name)
}

// currentCommandName returns the stashed command, or empty if no
// Begin*/Emit* has fired yet (panic before cobra dispatched a RunE).
func (c *Client) currentCommandName() CommandName {
	v := c.currentCommand.Load()
	if v == nil {
		return ""
	}
	return v.(CommandName)
}

// CurrentCommand returns the in-flight cobra command name, or empty
// if cobra hasn't dispatched a RunE yet (panic during flag parsing,
// opt-out check, etc.). Exported for main.go's panic-recover defer
// to pick the right ExceptionPhase: empty -> startup, set ->
// event_processing.
//
// Nil-safe: returns empty when called on a nil receiver, matching
// the rest of the package's "telemetry-off is silent" posture.
func (c *Client) CurrentCommand() CommandName {
	if c == nil {
		return ""
	}
	return c.currentCommandName()
}

// New constructs a Client. With no options it uses the production httpSink
// pointed at DefaultWorkerURL with a 2-second per-send deadline.
//
// When GAFFER_TELEMETRY_DEBUG=1 is set in the process environment, the
// configured sink is wrapped in a debug-tee that writes every envelope
// to stderr as JSON before forwarding. Env var is checked once at
// construction time.
func New(opts ...Option) *Client {
	c := &Client{
		perSendTimeout: 2 * time.Second,
		errLog:         func(error) {}, // silent by default
		workerURL:      DefaultWorkerURL,
		userAgent:      defaultUserAgent,
		startTime:      time.Now(),
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.sink == nil {
		c.sink = newHTTPSink(c.workerURL, c.userAgent)
	}
	if isTruthy(os.Getenv(EnvDebug)) {
		c.sink = newDebugTeeSink(c.sink, os.Stderr, c.errLog)
	}
	return c
}
