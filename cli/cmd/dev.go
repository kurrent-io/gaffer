package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	dapserver "github.com/kurrent-io/gaffer/cli/internal/dap"
	"github.com/kurrent-io/gaffer/cli/internal/env"
	"github.com/kurrent-io/gaffer/cli/internal/history"
	"github.com/kurrent-io/gaffer/cli/internal/projection"
	"github.com/kurrent-io/gaffer/cli/internal/subscription"
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

	projCtx, err := loadProjection(args[0])
	if err != nil {
		return err
	}

	session, err := gafferruntime.NewSession(projCtx.Source, projection.BuildSessionOptions(projCtx.Config, projCtx.Proj, devDebug))
	if err != nil {
		return handleSessionError(cmd, err)
	}
	defer session.Destroy()

	info := session.GetSources()
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

	feed := feedFn(session.Feed)
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

		feed = adapter.FeedEvent
		afterRun = func() { adapter.SendTerminated() }
	}

	r := newRunner(feed, writer)

	var source eventSource
	if devEvents != "" {
		events, err := projection.LoadEvents(devEvents)
		if err != nil {
			return err
		}
		source = &fixtureSource{events: events}
	} else {
		connStr := resolveConnection(projCtx.Config, projCtx.Root)
		if connStr == "" {
			return fmt.Errorf("no event source: use --events for fixtures or configure connection in gaffer.toml")
		}
		source = &liveSource{connStr: connStr, root: projCtx.Root, info: info, version: version}
	}

	srcErr := source.Run(ctx, r.processOne)

	if afterRun != nil {
		afterRun()
	}

	if ctx.Err() != nil {
		_, _ = fmt.Fprint(os.Stderr, "Interrupted\n\n")
		r.faulted = false
	} else if srcErr != nil {
		return srcErr
	}

	summary := buildSummary(session, info, r.partitions)
	writer.WriteSummary(r.stats, summary)

	if r.faulted {
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

type eventSource interface {
	Run(ctx context.Context, process func(string) bool) error
}

type fixtureSource struct {
	events []string
}

func (f *fixtureSource) Run(ctx context.Context, process func(string) bool) error {
	for _, evt := range f.events {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if process(evt) {
			break
		}
	}
	return nil
}

type liveSource struct {
	connStr string
	root    string
	info    gafferruntime.QuerySources
	version string
}

func (l *liveSource) Run(ctx context.Context, process func(string) bool) error {
	if err := env.Load(l.root, ""); err != nil {
		return fmt.Errorf("loading .env: %w", err)
	}

	dbConfig, err := kurrentdb.ParseConnectionString(l.connStr)
	if err != nil {
		return fmt.Errorf("invalid connection string: %w", err)
	}

	username, password := env.Credentials()
	if username != "" {
		dbConfig.Username = username
		dbConfig.Password = password
	}
	dbConfig.Logger = kurrentdb.NoopLogging()

	client, err := kurrentdb.NewClient(dbConfig)
	if err != nil {
		return fmt.Errorf("connecting to KurrentDB: %w", err)
	}
	defer func() { _ = client.Close() }()

	filter := subscription.BuildFilter(l.info, l.version)

	opts := kurrentdb.SubscribeToAllOptions{
		From:           kurrentdb.Start{},
		ResolveLinkTos: subscription.ResolveLinkTos(l.version),
	}
	if filter != nil {
		opts.Filter = filter
	}

	sub, err := client.SubscribeToAll(ctx, opts)
	if err != nil {
		return fmt.Errorf("subscribing: %w", err)
	}
	defer func() { _ = sub.Close() }()

	for {
		subEvent := sub.Recv()

		if subEvent.SubscriptionDropped != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("subscription dropped: %w", subEvent.SubscriptionDropped.Error)
		}

		if subEvent.EventAppeared == nil {
			continue
		}

		eventJSON, err := subscription.MapEvent(subEvent.EventAppeared)
		if err != nil || eventJSON == "" {
			continue
		}

		if process(eventJSON) {
			return nil
		}
	}
}

type feedFn func(string) (*gafferruntime.FeedResult, error)

type runner struct {
	feed       feedFn
	writer     outputWriter
	stats      eventStats
	partitions map[string]bool
	faulted    bool
}

func newRunner(feed feedFn, writer outputWriter) *runner {
	return &runner{
		feed:       feed,
		writer:     writer,
		partitions: make(map[string]bool),
	}
}

func (r *runner) processOne(eventJSON string) (stop bool) {
	event := parseEventInfo(eventJSON)
	r.writer.WriteEvent(event)

	result, err := r.feed(eventJSON)
	if err != nil {
		code, desc := classifyError(err)
		r.writer.WriteError(event.id(), code, desc)
		r.stats.errors++
		r.faulted = true
		return true
	}
	if result == nil {
		return false
	}

	r.writer.WriteResult(event.id(), result)
	if result.Status == "skipped" {
		r.stats.skipped++
	} else {
		r.stats.handled++
		if result.Partition != "" {
			r.partitions[result.Partition] = true
		}
	}
	return false
}

func classifyError(err error) (code, description string) {
	if projErr, ok := err.(gafferruntime.ProjectionError); ok {
		return projErr.ErrorCode(), projErr.ErrorDescription()
	}
	return "unexpected-error", err.Error()
}

func buildSummary(session *gafferruntime.Session, info gafferruntime.QuerySources, partitions map[string]bool) summaryState {
	isPartitioned := info.ByStreams || info.ByCustomPartitions

	summary := summaryState{
		partitioned:   isPartitioned,
		hasTransforms: info.DefinesStateTransform,
		hasBiState:    info.IsBiState,
	}

	if isPartitioned {
		summary.partitions = make(map[string]partitionData)
		for partition := range partitions {
			data := partitionData{}
			if state := session.GetState(&partition); state != nil {
				data.state = json.RawMessage(*state)
			}
			if info.DefinesStateTransform {
				if result, err := session.GetResult(&partition); err == nil && result != nil {
					data.result = json.RawMessage(*result)
				}
			}
			summary.partitions[partition] = data
		}
	} else {
		if state := session.GetState(nil); state != nil {
			summary.state = json.RawMessage(*state)
		}
		if info.DefinesStateTransform {
			if result, err := session.GetResult(nil); err == nil && result != nil {
				summary.result = json.RawMessage(*result)
			}
		}
	}

	if info.IsBiState {
		if shared := session.GetSharedState(); shared != nil {
			summary.sharedState = json.RawMessage(*shared)
		}
	}

	return summary
}
