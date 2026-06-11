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
	"github.com/kurrent-io/gaffer/cli/internal/project"
	"github.com/kurrent-io/gaffer/cli/internal/prompt"
	"github.com/kurrent-io/gaffer/cli/internal/telemetry"
)

const statsEmitInterval = 100 * time.Millisecond

type devOpts struct {
	Events                     string
	Fixture                    string
	JSON                       bool
	Env                        string
	Connection                 string
	Debug                      bool
	DebugPort                  int
	UntilCaughtUp              bool
	StartPausedIfNoBreakpoints bool
	Yes                        bool
}

func newDevCmd() *cobra.Command {
	opts := &devOpts{}

	cmd := &cobra.Command{
		Use:     "dev <projection>",
		Short:   "Run a projection locally",
		Example: "gaffer dev order-count",
		Long: "Run a projection locally against a fixture or live KurrentDB. " +
			"Run without <projection> on a terminal to pick one, and to pick a " +
			"source when none is given via --events / --fixture / --connection.",
		Args: maxArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// `gaffer dev` (cobra) maps to two telemetry event
			// variants: `dev` (single / fixture) and `debug` (DAP
			// server attached). Branch on opts.Debug so each
			// variant gets the right Tx + setters; both forms
			// share runDev's flag validation + setup.
			if opts.Debug {
				return runDevWithDebugTx(cmd, args, opts)
			}
			return runDevWithDevTx(cmd, args, opts)
		},
	}
	cmd.Flags().StringVar(&opts.Events, "events", "", "Path to a JSON events file (ad-hoc fixture)")
	cmd.Flags().StringVar(&opts.Fixture, "fixture", "", "Named fixture declared as fixtures.<name> in gaffer.toml")
	cmd.Flags().BoolVar(&opts.JSON, "json", false, "Output as NDJSON")
	cmd.Flags().StringVar(&opts.Env, "env", "", "Environment to run against, from gaffer.toml [env.<name>] (defaults to the env marked default)")
	cmd.Flags().StringVar(&opts.Connection, "connection", "", "KurrentDB connection string (overrides --env and config)")
	cmd.Flags().BoolVar(&opts.Debug, "debug", false, "Start DAP debug server")
	cmd.Flags().IntVar(&opts.DebugPort, "debug-port", 0, "DAP debug server port (0 = OS picks a free port; the actual bound port is reported on stderr and in --json output)")
	cmd.Flags().BoolVar(&opts.UntilCaughtUp, "until-caught-up", false, "Exit when subscription catches up (live mode only)")
	cmd.Flags().BoolVar(&opts.StartPausedIfNoBreakpoints, "start-paused-if-no-breakpoints", false, "Pause at the start of the first event when no breakpoints are set (debug mode only)")
	cmd.Flags().BoolVarP(&opts.Yes, "yes", "y", false, "Skip prompts (a projection and source must be resolvable without prompting)")
	// A run reads from an offline fixture or live from KurrentDB, never
	// both. The offline flags (--fixture, --events) and the live flags
	// (--env, --connection) are mutually exclusive pairwise, so cobra
	// rejects a contradictory mix with a usage error before RunE rather
	// than us silently dropping one. --fixture/--events are themselves
	// one-or-the-other; --env + --connection may combine (--connection
	// is a documented ad-hoc override of --env), so that pair is left
	// unmarked.
	cmd.MarkFlagsMutuallyExclusive("fixture", "events")
	cmd.MarkFlagsMutuallyExclusive("fixture", "env")
	cmd.MarkFlagsMutuallyExclusive("fixture", "connection")
	cmd.MarkFlagsMutuallyExclusive("events", "env")
	cmd.MarkFlagsMutuallyExclusive("events", "connection")
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

// resolveDevName returns the projection to run. When the positional was
// given it's used verbatim. Otherwise, on a terminal, the user picks
// from the projections declared in gaffer.toml; non-interactively the
// positional is required, mirroring the pre-prompt exactArgs(1) error.
func resolveDevName(cmd *cobra.Command, args []string, opts *devOpts) (string, error) {
	return resolveRequiredArg(cmd, args, prompt.Enabled(opts.Yes), func() (string, error) {
		root := project.FindRoot()
		if root == "" {
			return "", project.ErrNotInProject
		}
		cfg, err := config.Load(project.ConfigPath(root))
		if err != nil {
			return "", err
		}
		names := make([]prompt.Option, 0, len(cfg.Projection))
		for _, p := range cfg.Projection {
			names = append(names, prompt.Opt(p.Name))
		}
		if len(names) == 0 {
			return "", fmt.Errorf("no projections declared in gaffer.toml")
		}
		return prompt.Select("Projection", names, names[0].Value)
	})
}

// devEnvPrefix tags a source choice's Value as a live environment (by
// name) rather than a fixture, so applyDevSource can tell them apart.
const devEnvPrefix = "\x00env:"

// devSourceChoices lists the selectable event sources for a dev run: the
// projection's declared fixtures, then one live option per configured
// environment (so a non-default env is reachable without editing
// gaffer.toml). The default env, if any, is labelled as such.
func devSourceChoices(fixtures []string, cfg *config.Config) []prompt.Option {
	choices := make([]prompt.Option, 0, len(fixtures)+len(cfg.Env))
	// Pad the tag so the values line up in a column; "Fixture:" is widest.
	const tagWidth = len("Fixture:")
	for _, f := range fixtures {
		choices = append(choices, prompt.Option{Label: fmt.Sprintf("%-*s %s", tagWidth, "Fixture:", f), Value: f})
	}
	for _, name := range cfg.EnvNames() {
		label := fmt.Sprintf("%-*s %s", tagWidth, "Env:", name)
		if cfg.Env[name].Default {
			label += " [default]"
		}
		choices = append(choices, prompt.Option{Label: label, Value: devEnvPrefix + name})
	}
	return choices
}

// applyDevSource records a picked source on opts: a live env (carrying
// devEnvPrefix) onto opts.Env, otherwise a fixture name onto opts.Fixture.
func applyDevSource(opts *devOpts, sel string) {
	if env, ok := strings.CutPrefix(sel, devEnvPrefix); ok {
		opts.Env = env
	} else {
		opts.Fixture = sel
	}
}

// maybePromptDevSource picks an event source interactively when the user
// pinned none via --events / --fixture / --connection / --env. It offers
// the projection's fixtures plus one live option per configured
// environment. With a single possible source it's selected without
// prompting (and echoed to stderr, since the user wasn't asked); with
// none it's a no-op so buildSource emits its usual guidance.
func maybePromptDevSource(cmd *cobra.Command, proj *engine.Projection, opts *devOpts) error {
	if opts.Events != "" || opts.Fixture != "" || opts.Connection != "" || opts.Env != "" {
		return nil
	}
	if !prompt.Enabled(opts.Yes) {
		return nil
	}

	choices := devSourceChoices(proj.Def.FixtureNames(), proj.Config)
	switch len(choices) {
	case 0:
		return nil
	case 1:
		// Only one possible source: use it without asking, but echo the
		// choice so the run isn't reading from a source the user never
		// saw chosen.
		applyDevSource(opts, choices[0].Value)
		if opts.Env != "" {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Using live connection: %s\n", opts.Env)
		} else {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Using fixture: %s\n", opts.Fixture)
		}
		return nil
	}

	preselect := choices[0].Value
	if def, ok := proj.Config.DefaultEnv(); ok {
		preselect = devEnvPrefix + def.Name
	}
	sel, err := prompt.Select("Event source", choices, preselect)
	if err != nil {
		return err
	}
	applyDevSource(opts, sel)
	return nil
}

// devConnectedToDB reports whether the resolved dev run used a live
// KurrentDB source, for the connected_to_db telemetry property. It's not
// just "was --connection passed": runDev folds a --fixture (or an
// interactively-picked one) into opts.Events, so a live run is one with
// no fixture/events file in play and a connection that resolves (flag or
// gaffer.toml). opts.Fixture is checked too because a failed fixture
// lookup leaves opts.Fixture set while opts.Events stays empty - that's a
// fixture invocation, not live. Falls back to the flag when the
// projection didn't load (proj is nil), where opts isn't fully resolved.
func devConnectedToDB(opts *devOpts, proj *engine.Projection) bool {
	if proj == nil || proj.Config == nil {
		return opts.Connection != ""
	}
	resolved, err := resolveConnection(opts, proj.Config)
	return opts.Events == "" && opts.Fixture == "" && err == nil && resolved.Connection != ""
}

// runDevWithDevTx is the cobra wrapper for the non-debug path:
// opens a DevTx, calls runDev, drains the dev-side setters that
// are knowable here. Defer-direct per the Tx contract.
//
// Manifest-derived props (features / counts) are populated post-hoc
// from the loaded *engine.Projection. LoadProjection already parsed
// gaffer.toml, so runDev returns the projection alongside the error
// and the wrapper stamps from there.
func runDevWithDevTx(cmd *cobra.Command, args []string, opts *devOpts) error {
	tx := telemetry.BeginDev(cmd.Context())
	defer tx.End(cmd.Context())

	tracker := newProjErrTracker()
	diagTracker := newDiagSeenTracker()
	proj, err := runDev(cmd, args, opts, nil, runObservers{
		onProjectionError: tracker.Record,
		onConnected:       tx.SetDBVersion,
		onDiagnostic:      diagTracker.Record,
	})
	if proj != nil && proj.Config != nil {
		tx.SetManifestFeaturesUsed(telemetry.ManifestFeaturesOf(proj.Config))
		tx.SetProjectionCount(proj.Config.ProjectionCount())
		tx.SetFixtureCount(proj.Config.FixtureCount())
	}
	tx.SetConnectedToDB(devConnectedToDB(opts, proj))
	if seen := tracker.Sorted(); len(seen) > 0 {
		tx.SetProjectionErrorsSeen(seen)
	}
	if seen := diagTracker.Sorted(); len(seen) > 0 {
		tx.SetDiagnosticsSeen(seen)
	}
	out, ok := classifyOutcome(outcomeInputs{err: err, tracker: tracker})
	if !ok {
		out = telemetry.OutcomeUserError
	}
	tx.SetOutcome(out)
	return err
}

// runDevWithDebugTx is the cobra wrapper for the debug path:
// opens a DebugTx, calls runDev with a stats out-param the inner
// runDevDebug populates from the DAP server before returning,
// then drains the counters into typed setters. Defer-direct.
//
// The debug variant's schema doesn't carry manifest-derived props,
// so the returned *engine.Projection is ignored here - dev mode is
// the only variant that surfaces them.
func runDevWithDebugTx(cmd *cobra.Command, args []string, opts *devOpts) error {
	tx := telemetry.BeginDebug(cmd.Context())
	defer tx.End(cmd.Context())

	var dapStats dapserver.Stats
	tracker := newProjErrTracker()
	diagTracker := newDiagSeenTracker()
	var fixtureEvents int
	_, err := runDev(cmd, args, opts, &dapStats, runObservers{
		onProjectionError: tracker.Record,
		onFixtureEvent:    func() { fixtureEvents++ },
		onDiagnostic:      diagTracker.Record,
	})

	tx.SetBreakpointCount(dapStats.BreakpointCount)
	tx.SetStepCount(dapStats.StepCount)
	tx.SetPauseCount(dapStats.PauseCount)
	tx.SetRestartCount(dapStats.RestartCount)
	if fixtureEvents > 0 {
		tx.SetFixtureEventCount(fixtureEvents)
	}

	if seen := tracker.Sorted(); len(seen) > 0 {
		tx.SetProjectionErrorsSeen(seen)
	}
	if seen := diagTracker.Sorted(); len(seen) > 0 {
		tx.SetDiagnosticsSeen(seen)
	}
	out, ok := classifyOutcome(outcomeInputs{
		err:            err,
		tracker:        tracker,
		dapProtocolErr: dapStats.ProtocolError,
	})
	if !ok {
		out = telemetry.OutcomeUserError
	}
	tx.SetOutcome(out)
	return err
}

// runObservers carries the per-iteration telemetry callbacks the
// cobra wrappers feed into runDev. Zero-value (nil callbacks) means
// "no observation" - non-debug callers pass an empty struct because
// the DevTx schema doesn't carry the fields debug ones populate.
//
// Single-goroutine-owned: callbacks run synchronously on the cobra goroutine
// driving source.Run (onDiagnostic too - the runner fires it from ProcessOne,
// which source.Run calls inline), so the wrapper's tracker state needs no
// locking.
type runObservers struct {
	// onProjectionError fires with an FFI projection error, either
	// when a runner faults mid-iteration (recordProjectionFault) or
	// when engine.CreateSession fails to compile/load the projection
	// (recordCompileFault). Wrappers use it to accumulate
	// projection_errors_seen across DAP restart iterations and
	// across compile/runtime phases of a session.
	onProjectionError func(error)
	// onFixtureEvent fires per fixture event processed (post
	// r.ProcessOne, regardless of fault). Only invoked when the
	// source is a fixture; live-mode events are not counted here.
	onFixtureEvent func()
	// onConnected fires once when the live source's underlying
	// kurrentdb client connects, with the server's reported
	// major.minor version (or "unknown" when the probe fails).
	// Only invoked in live mode; never fires for fixture runs.
	onConnected func(dbVersion string)
	// onDiagnostic fires with each diagnostic code seen during the run: the
	// compile-time set off ProjectionInfo (recordCompileDiagnostics, once per
	// CreateSession) and the runtime quirks the runner reports off each
	// FeedResult / faulting error (RunnerConfig.OnDiagnostic). Wrappers
	// accumulate them into diagnostics_seen, independent of the output writer.
	onDiagnostic func(code string)
}

// runDev parses the dev flags, loads the projection, and dispatches
// to runDevSingle or runDevDebug. Returns the loaded *engine.Projection
// alongside the error so the cobra wrappers can drain telemetry
// properties off it; nil when LoadProjection failed.
func runDev(cmd *cobra.Command, args []string, opts *devOpts, dapStats *dapserver.Stats, obs runObservers) (*engine.Projection, error) {
	if opts.StartPausedIfNoBreakpoints && !opts.Debug {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "warning: --start-paused-if-no-breakpoints requires --debug; ignoring")
	}

	name, err := resolveDevName(cmd, args, opts)
	if err != nil {
		return nil, err
	}

	proj, err := engine.LoadProjection(name)
	if err != nil {
		return nil, err
	}

	if err := maybePromptDevSource(cmd, proj, opts); err != nil {
		return proj, err
	}

	// --fixture is a layer on top of --events: resolve the named
	// fixture's path through the loaded config, then everything
	// downstream uses opts.Events as the path. The two are mutually
	// exclusive (MarkFlagsMutuallyExclusive on the command), so we
	// never reach here with both set.
	if opts.Fixture != "" {
		path, ok := proj.Def.FindFixture(opts.Fixture)
		if !ok {
			names := proj.Def.FixtureNames()
			if len(names) == 0 {
				return proj, fmt.Errorf("projection %q has no fixtures declared in gaffer.toml", name)
			}
			return proj, fmt.Errorf("projection %q has no fixture named %q (available: %s)", name, opts.Fixture, strings.Join(names, ", "))
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
		return proj, runDevDebug(ctx, stop, proj, sourcePath, writer, tw, opts, dapStats, obs)
	}
	return proj, runDevSingle(ctx, stop, proj, sourcePath, writer, tw, opts, obs)
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
	obs runObservers,
) error {
	// includeShape: walk the AST for projection_shape telemetry
	// only when the Client is on ctx (i.e. telemetry isn't opted
	// out). EmitProjectionShape is nil-safe + dedup-aware so it's
	// fine to invoke unconditionally after the session creation.
	includeShape := telemetry.ShouldIncludeShape(ctx)
	session, info, err := engine.CreateSession(proj, false, includeShape)
	if err != nil {
		writer.WriteFatalError(toFatalError(err, sourcePath))
		recordCompileFault(err, obs.onProjectionError)
		return silent(err)
	}
	defer session.Destroy()
	recordCompileDiagnostics(info, obs.onDiagnostic)

	telemetry.EmitProjectionShape(ctx, sourcePath, info)

	if tw != nil {
		tw.RegisterCallbacks(session)
	}

	writer.WriteInfo(proj, info)

	r := engine.NewRunner(engine.RunnerConfig{
		Feed:         engine.FeedFn(session.Feed),
		Session:      session,
		Info:         info,
		Writer:       &eventWriterAdapter{writer: writer},
		OnDiagnostic: obs.onDiagnostic,
	})

	var caughtUp bool
	source, err := buildSource(opts, proj, info, nil, stop, &caughtUp, obs)
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
		recordProjectionFault(r, obs.onProjectionError)
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
	obs runObservers,
) error {
	store, err := history.New()
	if err != nil {
		return fmt.Errorf("creating history store: %w", err)
	}
	defer func() { _ = store.Close() }()

	absRoot, _ := filepath.Abs(proj.Root)

	// See runDevSingle for the includeShape rationale.
	includeShape := telemetry.ShouldIncludeShape(ctx)

	// Bootstrap session for the first iteration. Provides the initial
	// session ref to the adapter so OnLog/OnEmit wiring has something
	// to bind to before the loop's first iteration explicitly rebinds.
	session, info, err := engine.CreateSession(proj, true, includeShape)
	if err != nil {
		writer.WriteFatalError(toFatalError(err, sourcePath))
		recordCompileFault(err, obs.onProjectionError)
		return silent(err)
	}
	recordCompileDiagnostics(info, obs.onDiagnostic)

	telemetry.EmitProjectionShape(ctx, sourcePath, info)

	writer.WriteInfo(proj, info)

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
			Feed:         engine.FeedFn(session.Feed),
			Session:      session,
			Info:         info,
			Writer:       adapter.EventWriter(),
			History:      store,
			OnDiagnostic: obs.onDiagnostic,
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
			session, info, err = engine.CreateSession(proj, true, includeShape)
			if err != nil {
				adapter.AckRestart()
				recordCompileFault(err, obs.onProjectionError)
				return silent(err)
			}
			recordCompileDiagnostics(info, obs.onDiagnostic)
			// Re-emit on restart: the shape cache dedupes if
			// nothing structurally drifted; if the user edited
			// the source between iterations, the new hash
			// triggers a fresh envelope.
			telemetry.EmitProjectionShape(ctx, sourcePath, info)
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
		source, err := buildSource(opts, proj, info, adapter, stop, &caughtUp, obs)
		if err != nil {
			innerCancel()
			close(iterDone)
			session.Destroy()
			return err
		}

		var lastEmit time.Time
		// fixture_event_count is only meaningful in fixture mode;
		// live-mode events come from KurrentDB and are tracked
		// elsewhere if needed.
		countFixtureEvents := opts.Events != "" && obs.onFixtureEvent != nil
		// Run on the source goroutine: ProjectionSession is not
		// thread-safe, so concurrent Feed/state calls would corrupt
		// internal projection state.
		process := func(eventJSON string) bool {
			stop := r.ProcessOne(eventJSON)
			if countFixtureEvents {
				obs.onFixtureEvent()
			}
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
			//
			// Record the outgoing iteration's projection fault (if
			// any) BEFORE we lose the runner - without this,
			// restart-driven sessions silently drop the fault from
			// projection_errors_seen. The teardown-path branch below
			// has its own r.Faulted() check; this one matches it.
			recordProjectionFault(r, obs.onProjectionError)
			session.Destroy()
			session, info, err = engine.CreateSession(proj, true, includeShape)
			if err != nil {
				adapter.AckRestart()
				recordCompileFault(err, obs.onProjectionError)
				return silent(err)
			}
			recordCompileDiagnostics(info, obs.onDiagnostic)
			telemetry.EmitProjectionShape(ctx, sourcePath, info)
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
			lastErr := r.LastError()
			if lastErr != nil {
				writer.WriteFatalError(toFatalError(lastErr, sourcePath))
				recordProjectionFault(r, obs.onProjectionError)
			}
			return silent(fmt.Errorf("projection faulted"))
		}
		return nil
	}
}

// recordProjectionFault forwards a runner's last error to the
// projection-error callback if the runner faulted and the callback
// is wired. Used at every "this iteration is done" exit point so
// projection_errors_seen captures the fault before the runner goes
// out of scope.
func recordProjectionFault(r *engine.Runner, onProjectionError func(error)) {
	if onProjectionError == nil || !r.Faulted() {
		return
	}
	if lastErr := r.LastError(); lastErr != nil {
		onProjectionError(lastErr)
	}
}

// recordCompileFault forwards a CreateSession failure (always an FFI
// projection error - InvalidProjection, CompilationTimeout,
// InvalidArgument, ...) to the projection-error callback. Sibling of
// recordProjectionFault for the pre-iteration compile path; lets the
// dev wrapper feed projection_errors_seen + classifyOutcome so a
// broken projection ships as projection_compile_error rather than
// dropping to user_error.
//
// It deliberately does not feed onDiagnostic: the runtime only scans for
// diagnostics after compilation succeeds, so a compile-failure exception carries
// none. (Runtime faults differ - the runner pulls ErrorDiagnostics off those.)
func recordCompileFault(err error, onProjectionError func(error)) {
	if onProjectionError == nil || err == nil {
		return
	}
	onProjectionError(err)
}

// recordCompileDiagnostics feeds a freshly-created session's compile-time
// diagnostics (off ProjectionInfo) into the telemetry observer. The runtime
// quirks are collected separately by the runner (RunnerConfig.OnDiagnostic) off
// each FeedResult / faulting error, so collection stays independent of the output
// writer and doesn't fight the single-slot session.OnDiagnostic the text writer
// and DAP adapter already own. Idempotent across DAP restarts (the tracker
// dedupes); no-op when the observer is unset.
func recordCompileDiagnostics(info gafferruntime.ProjectionInfo, onDiagnostic func(string)) {
	if onDiagnostic == nil {
		return
	}
	for _, d := range info.Diagnostics {
		onDiagnostic(d.Code)
	}
}

// buildSource constructs the event source for one iteration. The
// caller owns `caughtUp`; buildSource just installs the OnCaughtUp
// callback that flips it when --until-caught-up fires. adapter may be
// nil for the non-debug single-run path. obs forwards the wrapper's
// telemetry callbacks (currently onConnected) into the live source.
func buildSource(
	opts *devOpts,
	proj *engine.Projection,
	info gafferruntime.ProjectionInfo,
	adapter *dapserver.DebugAdapter,
	stop context.CancelFunc,
	caughtUp *bool,
	obs runObservers,
) (engine.EventSource, error) {
	if opts.Events != "" {
		events, err := engine.LoadEvents(opts.Events)
		if err != nil {
			return nil, err
		}
		return engine.NewFixtureSource(events), nil
	}
	resolved, err := resolveConnection(opts, proj.Config)
	if err != nil {
		return nil, err
	}
	if resolved.Connection == "" {
		return nil, noSourceErr(proj.Config)
	}
	liveCfg := engine.LiveSourceConfig{
		ConnStr:       resolved.Connection,
		Root:          proj.Root,
		EnvName:       resolved.Name,
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
	liveCfg.OnConnected = obs.onConnected
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

// noSourceErr explains why a dev run resolved no event source, tailored
// to whether gaffer.toml has environments to point --env at. With envs
// configured, the user just hasn't selected one (no default, no --env);
// with none, they need a fixture, an events file, or an [env.<name>].
func noSourceErr(cfg *config.Config) error {
	if len(cfg.Env) > 0 {
		return fmt.Errorf(
			"no environment selected: pass --env <name> (available: %s) or set default = true in gaffer.toml; or use --fixture <name> / --events <path>",
			strings.Join(cfg.EnvNames(), ", "),
		)
	}
	return fmt.Errorf("no event source: use --fixture <name>, --events <path>, or add an [env.<name>] to gaffer.toml")
}

// resolveConnection determines the live target for a dev run, returning
// the resolved env (its name drives .env.<env> overlay at connect time).
// --connection wins outright (ad-hoc, no env name). An explicit --env
// must resolve or it's an error (the user named a target that isn't
// there). With neither flag, the default env is used if one exists; a
// project with no default env yields an empty connection and no error -
// dev falls back to fixtures.
func resolveConnection(opts *devOpts, cfg *config.Config) (config.ResolvedEnv, error) {
	if opts.Connection != "" {
		return config.ResolvedEnv{Connection: opts.Connection}, nil
	}
	if opts.Env != "" {
		return cfg.ResolveEnv(opts.Env)
	}
	if env, ok := cfg.DefaultEnv(); ok {
		return env, nil
	}
	return config.ResolvedEnv{}, nil
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
