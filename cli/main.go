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
	"github.com/kurrent-io/gaffer/cli/internal/envvar"
	"github.com/kurrent-io/gaffer/cli/internal/project"
	"github.com/kurrent-io/gaffer/cli/internal/telemetry"
	"github.com/kurrent-io/gaffer/cli/internal/updatecheck"
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
//
// The updatecheck Flush runs from a separate defer ahead of this one
// in the LIFO chain (see runMain), so worst-case exit wait is
// flushTimeout + updateCheckFlushTimeout, not max. Typical case is
// near-instant for both because their WaitGroups are usually empty.
const flushTimeout = 5 * time.Second

// updateCheckFlushTimeout bounds the wait for the once-per-day npm
// registry refresh goroutine. Shorter than telemetry's budget
// because update-check is strictly best-effort: a refresh that
// doesn't finish is just deferred until the next invocation.
const updateCheckFlushTimeout = 2 * time.Second

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

	// Snapshot the real shell environment before loading any .env, so
	// connection ${VAR} expansion can apply shell > .env.<env> > .env
	// precedence (after Load, shell and base-.env vars are
	// indistinguishable in the process env).
	envvar.Snapshot()

	// Load the project's base .env into the process environment before
	// anything reads env vars (telemetry opt-out, update-check), so a
	// committed .env is honoured uniformly - not just on the DB
	// connection path. Non-fatal: a broken .env must not brick a plain
	// `gaffer --help`, so we warn and carry on rather than exiting.
	if root := startupEnvRoot(); root != "" {
		if err := envvar.Load(root); err != nil {
			fmt.Fprintf(os.Stderr, "gaffer: %v\n", err)
		}
	}

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

	// updatecheck.Client lives on ctx alongside the telemetry one.
	// PersistentPreRunE on the cobra root reads it via FromCtx and
	// invokes Start; main.runMain's deferred Flush bounds how long
	// process exit waits for the background refresh goroutine.
	//
	// No IsConfigCommand carve-out here: `gaffer config ...` runs in a
	// terminal too, and a config user is exactly as well served by a
	// "newer release available" hint as anyone else. The carve-out
	// above is specifically about telemetry identity-mint racing with
	// `gaffer config telemetry`, which has no analogue here.
	updateClient := updatecheck.New(updatecheck.Options{
		Current:  cmd.Version,
		DevBuild: cmd.IsDevBuild(),
		Fetcher:  updatecheck.NpmFetcher{UserAgent: userAgent()},
	})
	ctx = updatecheck.WithClient(ctx, updateClient)

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
		flushCtx, cancel := context.WithTimeout(context.Background(), updateCheckFlushTimeout)
		_ = updateClient.Flush(flushCtx)
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

// startupEnvRoot picks the project root whose base .env is auto-loaded at
// startup. It honours the mcp project override (--project / GAFFER_PROJECT),
// so a server launched outside its project still loads that project's .env
// before opt-out and update-check read the environment; otherwise it
// discovers the root from the working directory. Returns "" when no project
// is in scope.
//
// The walk is bounded at $HOME so a stray gaffer.toml in an ancestor outside
// the user's home (a world-writable /tmp, /home on a shared host) can't turn
// its .env - which may carry KURRENTDB credentials - into ambient state for
// every invocation below it. Mirrors the telemetry opt-out walk's bound. An
// undeterminable home falls back to unbounded, matching prior behaviour.
func startupEnvRoot() string {
	home, _ := os.UserHomeDir()
	if override := cmd.PeekProjectOverride(os.Args[1:]); override != "" {
		return project.FindRootFromBounded(override, home)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return project.FindRootFromBounded(cwd, home)
}

// buildClient resolves opt-out and identity from the user's config
// and constructs the per-process telemetry Client. Relies on runMain
// having already loaded the project's .env into the process env, so a
// .env-declared opt-out (GAFFER_TELEMETRY_OPTOUT etc.) is honoured -
// keep that Load ahead of this call. Returns nil when
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
	// Bounded at $HOME like the .env loader and opt-out walk, so a stray
	// ancestor gaffer.toml outside home can't set this run's project_id.
	projectRoot := project.FindRootFromBounded(cwd, home)
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
