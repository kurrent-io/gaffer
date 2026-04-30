package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	dapserver "github.com/kurrent-io/gaffer/cli/internal/dap"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/history"
	"github.com/spf13/cobra"
)

type devOpts struct {
	Events                     string
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
			return runDev(cmd, args[0], opts)
		},
	}
	cmd.Flags().StringVar(&opts.Events, "events", "", "Path to JSON events file")
	cmd.Flags().BoolVar(&opts.JSON, "json", false, "Output as NDJSON")
	cmd.Flags().StringVar(&opts.Connection, "connection", "", "KurrentDB connection string (overrides config)")
	cmd.Flags().BoolVar(&opts.Debug, "debug", false, "Start DAP debug server")
	cmd.Flags().IntVar(&opts.DebugPort, "debug-port", 4711, "DAP debug server port")
	cmd.Flags().BoolVar(&opts.UntilCaughtUp, "until-caught-up", false, "Exit when subscription catches up (live mode only)")
	cmd.Flags().BoolVar(&opts.StartPausedIfNoBreakpoints, "start-paused-if-no-breakpoints", false, "Pause at the start of the first event when no breakpoints are set (debug mode only)")
	return cmd
}

func runDev(cmd *cobra.Command, name string, opts *devOpts) error {
	if opts.StartPausedIfNoBreakpoints && !opts.Debug {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "warning: --start-paused-if-no-breakpoints requires --debug; ignoring")
	}

	proj, err := engine.LoadProjection(name)
	if err != nil {
		return err
	}

	sourcePath, _ := filepath.Abs(filepath.Join(proj.Root, proj.Def.Entry))

	var writer outputWriter
	var tw *textWriter
	if opts.JSON {
		writer = newJSONWriter(os.Stdout)
	} else {
		tw = newTextWriter(os.Stdout, os.Stderr)
		writer = tw
	}

	session, info, err := engine.CreateSession(proj, opts.Debug)
	if err != nil {
		writer.WriteFatalError(toFatalError(err, sourcePath))
		return silent(err)
	}
	defer session.Destroy()

	engineVersion := proj.EngineVersion

	if tw != nil {
		tw.RegisterCallbacks(session)
	}

	writer.WriteInfo(proj.Def.Name, info, engineVersion)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	var afterRun func()
	var r *engine.Runner

	if opts.Debug {
		store, err := history.New()
		if err != nil {
			return fmt.Errorf("creating history store: %w", err)
		}

		absRoot, _ := filepath.Abs(proj.Root)

		adapter := dapserver.NewDebugAdapter(session, sourcePath, absRoot)
		adapter.SetStartPausedIfNoBreakpoints(opts.StartPausedIfNoBreakpoints)

		r = engine.NewRunner(engine.RunnerConfig{
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
		adapter.SetRunner(r)

		handler := adapter.Handler()
		addr := fmt.Sprintf("127.0.0.1:%d", opts.DebugPort)
		srv, err := dapserver.NewServer(addr, handler)
		if err != nil {
			return fmt.Errorf("starting debug server: %w", err)
		}
		defer func() { _ = srv.Close() }()
		adapter.SetServer(srv)

		_, _ = fmt.Fprintf(os.Stderr, "Debug server listening on %s\nWaiting for editor to attach...\n", srv.Addr())
		writer.WriteDebugListening(srv.Addr().String(), opts.DebugPort)

		go func() {
			_ = srv.Serve()
			stop()
		}()

		go func() {
			<-ctx.Done()
			r.Destroy()
		}()

		select {
		case <-adapter.Ready():
		case <-ctx.Done():
			return nil
		}

		afterRun = func() { adapter.SendTerminated() }
	} else {
		r = engine.NewRunner(engine.RunnerConfig{
			Feed:    engine.FeedFn(session.Feed),
			Session: session,
			Info:    info,
			Writer:  &eventWriterAdapter{writer: writer},
		})
	}

	var source engine.EventSource
	var caughtUp bool
	if opts.Events != "" {
		events, err := engine.LoadEvents(opts.Events)
		if err != nil {
			return err
		}
		source = engine.NewFixtureSource(events)
	} else {
		connStr := resolveConnection(opts.Connection, proj.Config)
		if connStr == "" {
			return fmt.Errorf("no event source: use --events for fixtures or configure connection in gaffer.toml")
		}
		liveCfg := engine.LiveSourceConfig{
			ConnStr:       connStr,
			Root:          proj.Root,
			Info:          info,
			EngineVersion: engineVersion,
		}
		if opts.UntilCaughtUp {
			liveCfg.OnCaughtUp = func() {
				caughtUp = true
				stop()
			}
		}
		source = engine.NewLiveSource(liveCfg)
	}

	srcErr := source.Run(ctx, r.ProcessOne)

	if afterRun != nil {
		afterRun()
	}

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
