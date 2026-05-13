package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/kurrent-io/gaffer/cli/cmd"
	"github.com/kurrent-io/gaffer/cli/internal/project"
	"github.com/kurrent-io/gaffer/cli/internal/telemetry"
	"github.com/kurrent-io/gaffer/cli/internal/userconfig"
)

// flushTimeout bounds how long process exit waits for in-flight
// telemetry sends to drain. Sized comfortably above the 2s per-send
// default so the per-send budget can elapse without flush preempting
// it; on a quiet run (no events emitted) wg.Wait returns instantly
// so this is the worst case, not the typical case.
//
// In-flight emits are concurrent goroutines (one per envelope) so
// many can drain inside this window in parallel.
const flushTimeout = 5 * time.Second

func main() {
	os.Exit(runMain())
}

// runMain owns the process lifecycle so the deferred Flush
// always runs - os.Exit (called from main on a non-zero return)
// would otherwise skip defers. Returns the exit code; panics
// re-propagate through the outermost defer so users see Go's
// default panic stack-trace + non-zero exit.
func runMain() (exitCode int) {
	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Peek argv for the hidden root flags before cobra parses. Two
	// readers need the values early: the Client itself (so every
	// envelope can stamp invoker_id / invoked_by / invoked_via)
	// and main.go (so cobra accepts the flags - they're registered
	// as hidden persistent for the same reason).
	invocation := telemetry.PeekInvocationFlags(os.Args[1:])
	// `gaffer config ...` commands own identity / opt-out lifecycle
	// (mint, disclose, opt out) themselves. Skip the pre-cobra
	// StartupGate path for the whole subtree so the config
	// subcommands' own EnsureIdentity calls aren't racing one that
	// StartupGate would have fired first. Config commands don't emit
	// command_invoked of their own, so skipping the Client here
	// costs nothing.
	var client *telemetry.Client
	if !telemetry.IsConfigCommand(os.Args[1:]) {
		client = buildClient(os.Stderr, invocation)
	}
	ctx := telemetry.WithClient(rootCtx, client)

	// Three-defer panic-recover chain. Registered first to last
	// so they fire LAST to FIRST on a panic:
	//
	//   1. recover-and-emit: catches the cmd.Execute panic,
	//      stashes it for re-panic, fires the exception
	//      envelope. Pairs with each long-running cmd's inner
	//      `defer tx.End(ctx)` which already emitted the
	//      matching command_invoked with outcome=internal_error.
	//   2. flush: drains both envelopes (command_invoked +
	//      exception) before the panic propagates further.
	//   3. re-panic: re-raises the stashed panic value so the
	//      Go runtime prints the original stack and exits
	//      non-zero. Without this re-raise the panic vanishes
	//      and the user gets no crash output.
	//
	// On the happy path: emit-defer sees recover()=nil (no-op),
	// flush-defer drains in-flight emits, re-panic-defer sees no
	// stashed value (no-op), runMain returns the exit code.
	var recoveredPanic any
	defer func() {
		if recoveredPanic != nil {
			panic(recoveredPanic)
		}
	}()
	defer func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), flushTimeout)
		_ = client.Flush(flushCtx)
		cancel()
	}()
	defer func() {
		if r := recover(); r != nil {
			// Client.ExceptionPhase() picks startup vs
			// event_processing based on whether cobra has
			// dispatched a RunE yet. projection_init / shutdown
			// are projection-runtime concerns that surface via
			// the .NET runtime's own exception path, not Go
			// panics caught here.
			telemetry.EmitException(ctx, r, client.ExceptionPhase())
			recoveredPanic = r
		}
	}()

	if err := cmd.Execute(ctx); err != nil {
		return 1
	}
	return 0
}

// buildClient resolves opt-out and identity from the user's config
// and constructs the per-process telemetry Client. Returns nil when
// opt-out is active, when the config is unreadable, or when identity
// resolution fails - all "telemetry off for this run" signals.
//
// Errors are swallowed silently: telemetry is best-effort and main
// has no useful surface to report config-read failures on without
// surprising users with errors about a feature they haven't asked
// for. Users who want to inspect the state run
// `gaffer config telemetry status`.
func buildClient(noticeOut io.Writer, invocation telemetry.Invocation) *telemetry.Client {
	store, err := userconfig.Open()
	if err != nil {
		return nil
	}
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	projectRoot := project.FindRootFrom(cwd)
	return telemetry.StartupGate(store, cwd, home, projectRoot, noticeOut, invocation,
		telemetry.WithUserAgent(userAgent()),
		telemetry.WithLibVersion(cmd.Version),
	)
}

// userAgent stamps a structured per-request identifier so the
// ingest worker can break events down by release, OS, and arch.
// Format mirrors Go's net/http default: `<product>/<version>
// (<os>; <arch>; <runtime>)`. Read at startup once and stamped on
// every emit's HTTP request.
func userAgent() string {
	return fmt.Sprintf("gaffer-cli/%s (%s; %s; %s)",
		cmd.Version, runtime.GOOS, runtime.GOARCH, runtime.Version())
}
