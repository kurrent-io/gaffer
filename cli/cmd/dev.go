package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	dapserver "github.com/kurrent-io/gaffer/cli/internal/dap"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/history"
	"github.com/kurrent-io/gaffer/cli/internal/telemetry"
)

const statsEmitInterval = 100 * time.Millisecond

type devOpts struct {
	Events                     string
	Fixture                    string
	JSON                       bool
	Connection                 string
	Debug                      bool
	DebugPort                  int
	UntilCaughtUp              bool
	StartPausedIfNoBreakpoints bool
}

func newDevCmd() *cobra.Command {
	opts := &devOpts{}

	cmd := &cobra.Command{
		Use:   "dev [projection]",
		Short: "Run a projection locally",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// `gaffer dev` (cobra) maps to two telemetry event
			// variants: `dev` (single / fixture) and `debug` (DAP
			// server attached). Branch on opts.Debug so each
			// variant gets the right Tx + setters; both forms
			// share runDev's flag validation + setup.
			if opts.Debug {
				return runDevWithDebugTx(cmd, args[0], opts)
			}
			return runDevWithDevTx(cmd, args[0], opts)
		},
	}
	cmd.Flags().StringVar(&opts.Events, "events", "", "Path to a JSON events file (ad-hoc fixture)")
	cmd.Flags().StringVar(&opts.Fixture, "fixture", "", "Named fixture declared as fixtures.<name> in gaffer.toml")
	cmd.Flags().BoolVar(&opts.JSON, "json", false, "Output as NDJSON")
	cmd.Flags().StringVar(&opts.Connection, "connection", "", "KurrentDB connection string (overrides config)")
	cmd.Flags().BoolVar(&opts.Debug, "debug", false, "Start DAP debug server")
	cmd.Flags().IntVar(&opts.DebugPort, "debug-port", 0, "DAP debug server port (0 = OS picks a free port; the actual bound port is reported on stderr and in --json output)")
	cmd.Flags().BoolVar(&opts.UntilCaughtUp, "until-caught-up", false, "Exit when subscription catches up (live mode only)")
	cmd.Flags().BoolVar(&opts.StartPausedIfNoBreakpoints, "start-paused-if-no-breakpoints", false, "Pause at the start of the first event when no breakpoints are set (debug mode only)")
	_ = cmd.RegisterFlagCompletionFunc("fixture", completeFixtures)
	return cmd
}

// completeFixtures returns the declared fixture names for the
// projection in the positional arg. Returns no suggestions when
// the projection isn't yet on the command line or the toml can't
// be loaded; load failures here are normal (cwd outside a gaffer
// project, gaffer.toml not yet saved) and shouldn't surface as
// shell error noise. The user finds real config errors when they
// run `gaffer dev` for actual.
func completeFixtures(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) < 1 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	proj, err := engine.LoadProjection(args[0])
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return proj.Def.FixtureNames(), cobra.ShellCompDirectiveNoFileComp
}

// runDevWithDevTx is the cobra wrapper for the non-debug path:
// opens a DevTx, calls runDev, drains the dev-side setters that
// are knowable here (currently just SetConnectedToDB). Defer-direct
// per the Tx contract.
func runDevWithDevTx(cmd *cobra.Command, name string, opts *devOpts) error {
	tx := telemetry.BeginDev(cmd.Context())
	defer tx.End(cmd.Context())

	tx.SetConnectedToDB(opts.Connection != "")

	err := runDev(cmd, name, opts, nil)
	if err != nil {
		tx.SetOutcome(telemetry.OutcomeUserError)
	}
	return err
}

// runDevWithDebugTx is the cobra wrapper for the debug path:
// opens a DebugTx, calls runDev with a stats out-param the inner
// runDevDebug populates from the DAP server before returning,
// then drains the counters into typed setters. Defer-direct.
func runDevWithDebugTx(cmd *cobra.Command, name string, opts *devOpts) error {
	tx := telemetry.BeginDebug(cmd.Context())
	defer tx.End(cmd.Context())

	var dapStats dapserver.Stats
	err := runDev(cmd, name, opts, &dapStats)

	tx.SetBreakpointCount(dapStats.BreakpointCount)
	tx.SetStepCount(dapStats.StepCount)
	tx.SetPauseCount(dapStats.PauseCount)
	tx.SetRestartCount(dapStats.RestartCount)

	if err != nil {
		tx.SetOutcome(telemetry.OutcomeUserError)
	}
	return err
}

func runDev(cmd *cobra.Command, name string, opts *devOpts, dapStats *dapserver.Stats) error {
	if opts.StartPausedIfNoBreakpoints && !opts.Debug {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "warning: --start-paused-if-no-breakpoints requires --debug; ignoring")
	}

	if opts.Events != "" && opts.Fixture != "" {
		return fmt.Errorf("only one of --events or --fixture may be used at a time")
	}

	proj, err := engine.LoadProjection(name)
	if err != nil {
		return err
	}

	// --fixture is a layer on top of --events: resolve the named
	// fixture's path through the loaded config, then everything
	// downstream uses opts.Events as the path. Mutex with --events
	// is the manual check above, not cobra-enforced - so we never
	// reach here with both set.
	if opts.Fixture != "" {
		path, ok := proj.Def.FindFixture(opts.Fixture)
		if !ok {
			names := proj.Def.FixtureNames()
			if len(names) == 0 {
				return fmt.Errorf("projection %q has no fixtures declared in gaffer.toml", name)
			}
			return fmt.Errorf("projection %q has no fixture named %q (available: %s)", name, opts.Fixture, strings.Join(names, ", "))
		}
		opts.Events = filepath.Join(proj.Root, path)
	}

	sourcePath, _ := filepath.Abs(filepath.Join(proj.Root, proj.Def.Entry))

	var writer outputWriter
	var tw *textWriter
	if opts.JSON {
		writer = newJSONWriter(os.Stdout)
	} else {
		tw = newTextWriter(os.Stdout, os.Stderr)
		// Fixture mode: surface skipped events + reasons. The user
		// curated these events; a skip is diagnostic. Live mode
		// hides them as runtime hygiene noise.
		tw.showSkipped = opts.Events != ""
		writer = tw
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()

	if opts.Debug {
		return runDevDebug(ctx, stop, proj, sourcePath, writer, tw, opts, dapStats)
	}
	return runDevSingle(ctx, stop, proj, sourcePath, writer, tw, opts)
}

// runDevSingle handles the non-debug path: one engine.Session, one
// source.Run, summary write. Restart isn't relevant here.
func runDevSingle(
	ctx context.Context,
	stop context.CancelFunc,
	proj *engine.Projection,
	sourcePath string,
	writer outputWriter,
	tw *textWriter,
	opts *devOpts,
) error {
	session, info, err := engine.CreateSession(proj, false)
	if err != nil {
		writer.WriteFatalError(toFatalError(err, sourcePath))
		return silent(err)
	}
	defer session.Destroy()

	if tw != nil {
		tw.RegisterCallbacks(session)
	}

	writer.WriteInfo(proj.Def.Name, info, proj.EngineVersion, proj.DbVersion)

	r := engine.NewRunner(engine.RunnerConfig{
		Feed:    engine.FeedFn(session.Feed),
		Session: session,
		Info:    info,
		Writer:  &eventWriterAdapter{writer: writer},
	})

	var caughtUp bool
	source, err := buildSource(opts, proj, info, nil, stop, &caughtUp)
	if err != nil {
		return err
	}
	srcErr := source.Run(ctx, r.ProcessOne)

	if err := finalizeRun(ctx, caughtUp, srcErr, r, os.Stderr); err != nil {
		return err
	}

	summary := r.CollectState()
	writer.WriteSummary(r.Stats(), summary)
	if r.Faulted() {
		if lastErr := r.LastError(); lastErr != nil {
			writer.WriteFatalError(toFatalError(lastErr, sourcePath))
		}
		return silent(fmt.Errorf("projection faulted"))
	}
	return nil
}

// runDevDebug handles the --debug path with restart support. The DAP
// server, history store, and adapter persist across restart; engine
// session, runner, and the source loop are recreated per iteration.
// Outer ctx done = real teardown (signal or socket close); the loop
// ends naturally after the source's final iteration.
func runDevDebug(
	ctx context.Context,
	stop context.CancelFunc,
	proj *engine.Projection,
	sourcePath string,
	writer outputWriter,
	tw *textWriter,
	opts *devOpts,
	dapStats *dapserver.Stats,
) error {
	store, err := history.New()
	if err != nil {
		return fmt.Errorf("creating history store: %w", err)
	}
	defer func() { _ = store.Close() }()

	absRoot, _ := filepath.Abs(proj.Root)

	// Bootstrap session for the first iteration. Provides the initial
	// session ref to the adapter so OnLog/OnEmit wiring has something
	// to bind to before the loop's first iteration explicitly rebinds.
	session, info, err := engine.CreateSession(proj, true)
	if err != nil {
		writer.WriteFatalError(toFatalError(err, sourcePath))
		return silent(err)
	}

	writer.WriteInfo(proj.Def.Name, info, proj.EngineVersion, proj.DbVersion)

	adapter := dapserver.NewDebugAdapter(session, sourcePath, absRoot)
	adapter.SetStartPausedIfNoBreakpoints(opts.StartPausedIfNoBreakpoints)
	adapter.SetFixtureMode(opts.Events != "")

	addr := fmt.Sprintf("127.0.0.1:%d", opts.DebugPort)
	srv, err := dapserver.NewServer(addr, adapter.Handler())
	if err != nil {
		session.Destroy()
		// Surface EADDRINUSE as a structured fatal_error so the
		// extension can route it to a "change port" toast instead of
		// the generic "projection failed" path. Other bind errors
		// (permission denied, etc) fall through to the generic error.
		if errors.Is(err, syscall.EADDRINUSE) {
			writer.WriteFatalError(fatalError{
				Code:        "PORT_IN_USE",
				Description: fmt.Sprintf("port %d is already in use", opts.DebugPort),
			})
		}
		return fmt.Errorf("starting debug server: %w", err)
	}
	defer func() {
		// Approximation note: this fires when the projection loop
		// returns, which is typically AFTER Serve has already
		// returned (Serve's exit triggers stop(), the main loop
		// sees ctx.Done, returns). On the early-exit paths
		// (fixture exhausted, source.Run error before disconnect)
		// the Serve goroutine may still be processing in-flight
		// DAP messages when we drain; those bumps are missed.
		// The bound is "a handful of messages during shutdown"
		// and the counters are atomic so this is a semantic
		// approximation, not a data race. A deterministic fix
		// requires dap.Server.Close to close the active conn and
		// wait for readLoop to drain - tracked for follow-up
		// alongside the dev/debug wrapper restructure.
		if dapStats != nil {
			*dapStats = srv.Stats()
		}
		_ = srv.Close()
	}()
	adapter.SetServer(srv)

	// Report the actual bound port, not opts.DebugPort - the latter is
	// the requested port (often 0 for OS-pick) and the editor needs to
	// know the real port to attach to.
	boundAddr := srv.Addr().(*net.TCPAddr)
	_, _ = fmt.Fprintf(os.Stderr, "Debug server listening on %s\nWaiting for editor to attach...\n", boundAddr)
	writer.WriteDebugListening(boundAddr.String(), boundAddr.Port)

	go func() {
		_ = srv.Serve()
		stop()
	}()

	// Per-iteration loop: each pass owns one engine.Session + Runner
	// + source.Run. A `restart` request triggers a fresh iteration; a
	// real disconnect (or signal) ends the loop after the in-flight
	// iteration unwinds. Both paths must ack any pending restart so
	// handleRestart's response goroutine can return - even on a
	// dying socket, the alternative is leaking it forever.
	for {
		if tw != nil {
			tw.RegisterCallbacks(session)
		}

		r := engine.NewRunner(engine.RunnerConfig{
			Feed:    engine.FeedFn(session.Feed),
			Session: session,
			Info:    info,
			Writer:  adapter.EventWriter(),
			History: store,
			Debug: &engine.DebugConfig{
				Session: session,
				Info:    info,
				OnBreak: adapter.HandleBreak,
			},
		})
		adapter.SetSession(session)
		adapter.SetRunner(r)

		// Wait for configurationDone, or fast-path restart-before-config,
		// or teardown.
		select {
		case <-adapter.Ready():
		case <-adapter.RestartRequested():
			session.Destroy()
			session, info, err = engine.CreateSession(proj, true)
			if err != nil {
				adapter.AckRestart()
				return silent(err)
			}
			adapter.ResetForRestart()
			adapter.AckRestart()
			continue
		case <-ctx.Done():
			session.Destroy()
			return nil
		}

		// Per-iteration ctx so a restart can cancel source.Run without
		// tearing down the outer process. The watcher goroutine signals
		// `restartCh` when it observed RestartRequested, so the main
		// loop can disambiguate "restart-driven exit" from "natural
		// completion" - a select on adapter.RestartRequested() here
		// would race with the watcher consuming the same value.
		innerCtx, innerCancel := context.WithCancel(ctx)
		iterDone := make(chan struct{})
		restartCh := make(chan struct{}, 1)
		go func() {
			select {
			case <-adapter.RestartRequested():
				select {
				case restartCh <- struct{}{}:
				default:
				}
				innerCancel()
				r.Unblock()
			case <-ctx.Done():
				innerCancel()
				r.Unblock()
			case <-iterDone:
				// source.Run returned naturally; nothing to do.
			}
		}()

		var caughtUp bool
		source, err := buildSource(opts, proj, info, adapter, stop, &caughtUp)
		if err != nil {
			innerCancel()
			close(iterDone)
			session.Destroy()
			return err
		}

		var lastEmit time.Time
		// Run on the source goroutine: ProjectionSession is not
		// thread-safe, so concurrent Feed/state calls would corrupt
		// internal projection state.
		process := func(eventJSON string) bool {
			stop := r.ProcessOne(eventJSON)
			if time.Since(lastEmit) >= statsEmitInterval {
				adapter.EmitStatsIfChanged()
				adapter.EmitStateIfChanged()
				lastEmit = time.Now()
			}
			return stop
		}

		srcErr := source.Run(innerCtx, process)
		close(iterDone)
		innerCancel()

		// Final flush regardless of how this iteration ended.
		adapter.EmitStatsIfChanged()
		adapter.EmitStateIfChanged()

		// Did this exit because the user hit restart? The watcher
		// goroutine writes to restartCh when it sees RestartRequested;
		// we also do a non-blocking drain to catch the rare race where
		// the request lands AFTER source.Run returned naturally.
		restartRequested := false
		select {
		case <-restartCh:
			restartRequested = true
		default:
		}
		if !restartRequested {
			select {
			case <-adapter.RestartRequested():
				restartRequested = true
			default:
			}
		}

		if restartRequested && ctx.Err() == nil {
			// Genuine restart: tear down this iteration, stand up a
			// fresh session, ack so handleRestart sends its response.
			session.Destroy()
			session, info, err = engine.CreateSession(proj, true)
			if err != nil {
				adapter.AckRestart()
				return silent(err)
			}
			adapter.ResetForRestart()
			adapter.AckRestart()
			if err := finalizeRun(ctx, caughtUp, srcErr, r, os.Stderr); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "ignoring iteration error during restart: %v\n", err)
			}
			continue
		}

		// Teardown path: real disconnect, signal, or natural source
		// completion (fixture exhausted, --until-caught-up). Ack any
		// raced restart so handleRestart's response goroutine isn't
		// pinned forever (the response may not reach the editor over
		// a closing socket, but that's fine - the goroutine exits
		// cleanly either way).
		if restartRequested {
			adapter.AckRestart()
		}

		adapter.SendTerminated()
		if err := finalizeRun(ctx, caughtUp, srcErr, r, os.Stderr); err != nil {
			session.Destroy()
			return err
		}

		summary := r.CollectState()
		writer.WriteSummary(r.Stats(), summary)
		session.Destroy()
		if r.Faulted() {
			if lastErr := r.LastError(); lastErr != nil {
				writer.WriteFatalError(toFatalError(lastErr, sourcePath))
			}
			return silent(fmt.Errorf("projection faulted"))
		}
		return nil
	}
}

// buildSource constructs the event source for one iteration. The
// caller owns `caughtUp`; buildSource just installs the OnCaughtUp
// callback that flips it when --until-caught-up fires. adapter may be
// nil for the non-debug single-run path.
func buildSource(
	opts *devOpts,
	proj *engine.Projection,
	info gafferruntime.ProjectionInfo,
	adapter *dapserver.DebugAdapter,
	stop context.CancelFunc,
	caughtUp *bool,
) (engine.EventSource, error) {
	if opts.Events != "" {
		events, err := engine.LoadEvents(opts.Events)
		if err != nil {
			return nil, err
		}
		return engine.NewFixtureSource(events), nil
	}
	connStr := resolveConnection(opts.Connection, proj.Config)
	if connStr == "" {
		return nil, fmt.Errorf("no event source: use --fixture <name>, --events <path>, or configure connection in gaffer.toml")
	}
	liveCfg := engine.LiveSourceConfig{
		ConnStr:       connStr,
		Root:          proj.Root,
		Info:          info,
		EngineVersion: proj.EngineVersion,
	}
	liveCfg.OnCaughtUp = func() {
		if opts.UntilCaughtUp {
			*caughtUp = true
			stop()
		}
		if adapter != nil {
			adapter.EmitCaughtUp()
		}
	}
	liveCfg.OnFellBehind = func() {
		if adapter != nil {
			adapter.EmitFellBehind()
		}
	}
	return engine.NewLiveSource(liveCfg), nil
}

// finalizeRun handles post-loop disposition. If the context was cancelled
// without catching up (user interrupt), prints a notice and clears the
// faulted state. Otherwise propagates any source error.
func finalizeRun(ctx context.Context, caughtUp bool, srcErr error, r *engine.Runner, stderr io.Writer) error {
	if ctx.Err() != nil && !caughtUp {
		_, _ = fmt.Fprint(stderr, "Interrupted\n\n")
		r.SetFaulted(false)
		return nil
	}
	return srcErr
}

func resolveConnection(override string, cfg *config.Config) string {
	if override != "" {
		return override
	}
	return cfg.Connection
}

type eventWriterAdapter struct {
	writer outputWriter
}

func (a *eventWriterAdapter) OnEvent(eventJSON string) {
	a.writer.WriteEvent(parseEventInfo(eventJSON))
}

func (a *eventWriterAdapter) OnResult(eventID string, result *gafferruntime.FeedResult) {
	a.writer.WriteResult(eventID, result)
}

func (a *eventWriterAdapter) OnError(eventID, code, description string) {
	a.writer.WriteError(eventID, code, description)
}
