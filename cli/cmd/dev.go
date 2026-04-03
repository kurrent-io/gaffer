package cmd

import (
	"context"
	"fmt"
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

var devCmd = &cobra.Command{
	Use:   "dev [projection]",
	Short: "Run a projection locally",
	Args:  cobra.ExactArgs(1),
	RunE:  runDev,
}

var (
	devEvents     string
	devJSON       bool
	devConnection string
	devDebug      bool
	devDebugPort  int
)

func init() {
	devCmd.Flags().StringVar(&devEvents, "events", "", "Path to JSON events file")
	devCmd.Flags().BoolVar(&devJSON, "json", false, "Output as NDJSON")
	devCmd.Flags().StringVar(&devConnection, "connection", "", "KurrentDB connection string (overrides config)")
	devCmd.Flags().BoolVar(&devDebug, "debug", false, "Start DAP debug server")
	devCmd.Flags().IntVar(&devDebugPort, "debug-port", 4711, "DAP debug server port")
}

func runDev(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true

	projCtx, err := engine.LoadProjection(args[0])
	if err != nil {
		return err
	}

	session, info, err := engine.NewSession(projCtx, devDebug)
	if err != nil {
		return handleSessionError(cmd, err)
	}
	defer session.Destroy()

	version := projCtx.Engine

	var writer outputWriter
	if devJSON {
		writer = newJSONWriter(os.Stdout)
	} else {
		tw := newTextWriter(os.Stdout)
		tw.RegisterCallbacks(session)
		writer = tw
	}

	writer.WriteInfo(projCtx.Proj.Name, info, version)

	feed := engine.FeedFn(session.Feed)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	var afterRun func()

	if devDebug {
		store, err := history.New()
		if err != nil {
			return fmt.Errorf("creating history store: %w", err)
		}
		defer func() { _ = store.Close() }()

		sourcePath, _ := filepath.Abs(filepath.Join(projCtx.Root, projCtx.Proj.Entry))
		absRoot, _ := filepath.Abs(projCtx.Root)
		shape := dapserver.ProjectionShape{
			IsPartitioned:   info.ByStreams || info.ByCustomPartitions,
			IsBiState:       info.IsBiState,
			HasTransforms:   info.DefinesStateTransform,
			ProducesResults: info.ProducesResults,
		}
		adapter := dapserver.NewDebugAdapter(session, sourcePath, absRoot, store, shape)
		handler := adapter.Handler()

		addr := fmt.Sprintf("127.0.0.1:%d", devDebugPort)
		srv, err := dapserver.NewServer(addr, handler)
		if err != nil {
			return fmt.Errorf("starting debug server: %w", err)
		}
		defer func() { _ = srv.Close() }()
		adapter.SetServer(srv)

		_, _ = fmt.Fprintf(os.Stderr, "Debug server listening on %s\nWaiting for editor to attach...\n", srv.Addr())
		writer.WriteDebugListening(srv.Addr().String(), devDebugPort)

		go func() {
			_ = srv.Serve()
			stop()
		}()

		go func() {
			<-ctx.Done()
			session.ClearBreakpoints()
			defer func() { recover() }() //nolint:errcheck
			session.Continue()
		}()

		select {
		case <-adapter.Ready():
		case <-ctx.Done():
			return nil
		}

		feed = engine.FeedFn(adapter.FeedEvent)
		afterRun = func() { adapter.SendTerminated() }
	}

	r := engine.NewRunner(engine.RunnerConfig{
		Feed:    feed,
		Writer:  &eventWriterAdapter{writer: writer},
		History: nil,
	})

	var source engine.EventSource
	if devEvents != "" {
		events, err := engine.LoadEvents(devEvents)
		if err != nil {
			return err
		}
		source = engine.NewFixtureSource(events)
	} else {
		connStr := resolveConnection(projCtx.Config, projCtx.Root)
		if connStr == "" {
			return fmt.Errorf("no event source: use --events for fixtures or configure connection in gaffer.toml")
		}
		source = engine.NewLiveSource(connStr, projCtx.Root, info, version)
	}

	srcErr := source.Run(ctx, r.ProcessOne)

	if afterRun != nil {
		afterRun()
	}

	if ctx.Err() != nil {
		_, _ = fmt.Fprint(os.Stderr, "Interrupted\n\n")
		r.Faulted = false
	} else if srcErr != nil {
		return srcErr
	}

	summary := engine.CollectState(session, info, r.Partitions)
	writer.WriteSummary(r.Stats, summary)

	if r.Faulted {
		cmd.SilenceErrors = true
		return fmt.Errorf("projection faulted")
	}

	return nil
}

func resolveConnection(cfg *config.Config, root string) string {
	if devConnection != "" {
		return devConnection
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
