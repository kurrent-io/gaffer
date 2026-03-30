package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	dapserver "github.com/kurrent-io/gaffer/cli/internal/dap"
	"github.com/kurrent-io/gaffer/cli/internal/env"
	"github.com/kurrent-io/gaffer/cli/internal/history"
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

type projectionInfo struct {
	AllStreams                  bool     `json:"AllStreams"`
	ByStreams                   bool     `json:"ByStreams"`
	ByCustomPartitions          bool     `json:"ByCustomPartitions"`
	IsBiState                   bool     `json:"IsBiState"`
	DefinesStateTransform       bool     `json:"DefinesStateTransform"`
	ProducesResults             bool     `json:"ProducesResults"`
	HandlesDeletedNotifications bool     `json:"HandlesDeletedNotifications"`
	IncludeLinks                bool     `json:"IncludeLinks"`
	Categories                  []string `json:"Categories"`
	Streams                     []string `json:"Streams"`
	Events                      []string `json:"Events"`
}

func runDev(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true

	ctx, err := loadProjection(args[0])
	if err != nil {
		return err
	}

	session, err := gafferruntime.NewSession(ctx.Source, buildSessionOptions(ctx.Config, ctx.Proj, devDebug))
	if err != nil {
		return handleSessionError(cmd, err)
	}
	defer session.Destroy()

	info := getProjectionInfo(session)
	version := ctx.Engine

	var writer outputWriter
	if devJSON {
		writer = newJSONWriter(os.Stdout)
	} else {
		tw := newTextWriter(os.Stdout)
		tw.RegisterCallbacks(session)
		writer = tw
	}

	writer.WriteInfo(ctx.Proj.Name, info, version)

	if devDebug {
		sourcePath, _ := filepath.Abs(filepath.Join(ctx.Root, ctx.Proj.Entry))
		return runDebugMode(cmd, session, info, version, ctx.Config, ctx.Root, writer, sourcePath)
	}

	if devEvents != "" {
		return runFixtureMode(cmd, session, info, writer)
	}

	connStr := resolveConnection(ctx.Config, ctx.Root)
	if connStr == "" {
		return fmt.Errorf("no event source: use --events for fixtures or configure connection in gaffer.toml")
	}

	return runLiveMode(cmd, session, info, version, connStr, ctx.Root, writer)
}

func resolveConnection(cfg *config.Config, root string) string {
	if devConnection != "" {
		return devConnection
	}
	return cfg.Connection
}

func runFixtureMode(cmd *cobra.Command, session *gafferruntime.Session, info projectionInfo, writer outputWriter) error {
	events, err := loadEvents(devEvents)
	if err != nil {
		return err
	}

	stats, partitions, faulted := processEvents(session, events, writer)

	summary := buildSummary(session, info, partitions)
	writer.WriteSummary(stats, summary)

	if faulted {
		cmd.SilenceErrors = true
		return fmt.Errorf("projection faulted")
	}

	return nil
}

func runLiveMode(cmd *cobra.Command, session *gafferruntime.Session, info projectionInfo, version, connStr, root string, writer outputWriter) error {
	if err := env.Load(root, ""); err != nil {
		return fmt.Errorf("loading .env: %w", err)
	}

	dbConfig, err := kurrentdb.ParseConnectionString(connStr)
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	filter := subscription.BuildFilter(subscription.SourceInfo{
		AllStreams:                  info.AllStreams,
		Categories:                  info.Categories,
		Streams:                     info.Streams,
		Events:                      info.Events,
		HandlesDeletedNotifications: info.HandlesDeletedNotifications,
	}, version)

	opts := kurrentdb.SubscribeToAllOptions{
		From:           kurrentdb.Start{},
		ResolveLinkTos: subscription.ResolveLinkTos(version),
	}
	if filter != nil {
		opts.Filter = filter
	}

	sub, err := client.SubscribeToAll(ctx, opts)
	if err != nil {
		return fmt.Errorf("subscribing: %w", err)
	}
	defer func() { _ = sub.Close() }()

	var stats eventStats
	partitions := make(map[string]bool)
	var faulted bool

	for {
		subEvent := sub.Recv()

		if subEvent.SubscriptionDropped != nil {
			if ctx.Err() != nil {
				_, _ = fmt.Fprint(os.Stderr, "Interrupted\n\n")
				break
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

		event := parseEventInfo(eventJSON)
		writer.WriteEvent(event)

		result, feedErr := session.Feed(eventJSON)
		if feedErr != nil {
			code, desc := classifyError(feedErr)
			writer.WriteError(event.id(), code, desc)
			stats.errors++
			faulted = true
			break
		}

		writer.WriteResult(event.id(), result)

		if result.Status == "skipped" {
			stats.skipped++
		} else {
			stats.handled++
			if result.Partition != "" {
				partitions[result.Partition] = true
			}
		}
	}

	summary := buildSummary(session, info, partitions)
	writer.WriteSummary(stats, summary)

	if faulted {
		cmd.SilenceErrors = true
		return fmt.Errorf("projection faulted")
	}

	return nil
}

func runDebugMode(cmd *cobra.Command, session *gafferruntime.Session, info projectionInfo, version string, cfg *config.Config, root string, writer outputWriter, sourcePath string) error {
	store, err := history.New()
	if err != nil {
		return fmt.Errorf("creating history store: %w", err)
	}
	defer func() { _ = store.Close() }()

	absRoot, _ := filepath.Abs(root)
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	go func() {
		_ = srv.Serve()
		stop()
	}()

	// On interrupt or editor disconnect, clear breakpoints and continue so Feed unblocks.
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

	if devEvents != "" {
		events, err := loadEvents(devEvents)
		if err != nil {
			return err
		}

		var stats eventStats
		partitions := make(map[string]bool)
		var faulted bool

		for _, evt := range events {
			if ctx.Err() != nil {
				_, _ = fmt.Fprint(os.Stderr, "Interrupted\n\n")
				break
			}
			event := parseEventInfo(evt)
			writer.WriteEvent(event)

			result, feedErr := adapter.FeedEvent(evt)
			if feedErr != nil {
				if ctx.Err() != nil {
					_, _ = fmt.Fprint(os.Stderr, "Interrupted\n\n")
					break
				}
				code, desc := classifyError(feedErr)
				writer.WriteError(event.id(), code, desc)
				stats.errors++
				faulted = true
				break
			}

			writer.WriteResult(event.id(), result)
			if result.Status == "skipped" {
				stats.skipped++
			} else {
				stats.handled++
				if result.Partition != "" {
					partitions[result.Partition] = true
				}
			}
		}

		adapter.SendTerminated()
		summary := buildSummary(session, info, partitions)
		writer.WriteSummary(stats, summary)

		if faulted {
			cmd.SilenceErrors = true
			return fmt.Errorf("projection faulted")
		}
		return nil
	}

	connStr := resolveConnection(cfg, root)
	if connStr == "" {
		return fmt.Errorf("no event source: use --events for fixtures or configure connection in gaffer.toml")
	}

	if err := env.Load(root, ""); err != nil {
		return fmt.Errorf("loading .env: %w", err)
	}

	dbConfig, err := kurrentdb.ParseConnectionString(connStr)
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

	filter := subscription.BuildFilter(subscription.SourceInfo{
		AllStreams:                  info.AllStreams,
		Categories:                  info.Categories,
		Streams:                     info.Streams,
		Events:                      info.Events,
		HandlesDeletedNotifications: info.HandlesDeletedNotifications,
	}, version)

	subOpts := kurrentdb.SubscribeToAllOptions{
		From:           kurrentdb.Start{},
		ResolveLinkTos: subscription.ResolveLinkTos(version),
	}
	if filter != nil {
		subOpts.Filter = filter
	}

	sub, err := client.SubscribeToAll(ctx, subOpts)
	if err != nil {
		return fmt.Errorf("subscribing: %w", err)
	}
	defer func() { _ = sub.Close() }()

	var stats eventStats
	partitions := make(map[string]bool)
	var faulted bool

	for {
		subEvent := sub.Recv()

		if subEvent.SubscriptionDropped != nil {
			if ctx.Err() != nil {
				_, _ = fmt.Fprint(os.Stderr, "Interrupted\n\n")
				break
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

		event := parseEventInfo(eventJSON)
		writer.WriteEvent(event)

		result, feedErr := adapter.FeedEvent(eventJSON)
		if feedErr != nil {
			code, desc := classifyError(feedErr)
			writer.WriteError(event.id(), code, desc)
			stats.errors++
			faulted = true
			break
		}

		writer.WriteResult(event.id(), result)
		if result.Status == "skipped" {
			stats.skipped++
		} else {
			stats.handled++
			if result.Partition != "" {
				partitions[result.Partition] = true
			}
		}
	}

	adapter.SendTerminated()
	summary := buildSummary(session, info, partitions)
	writer.WriteSummary(stats, summary)

	if faulted {
		cmd.SilenceErrors = true
		return fmt.Errorf("projection faulted")
	}
	return nil
}

func processEvents(session *gafferruntime.Session, events []string, writer outputWriter) (eventStats, map[string]bool, bool) {
	var stats eventStats
	partitions := make(map[string]bool)

	for _, evt := range events {
		event := parseEventInfo(evt)
		writer.WriteEvent(event)

		result, feedErr := session.Feed(evt)
		if feedErr != nil {
			code, desc := classifyError(feedErr)
			writer.WriteError(event.id(), code, desc)
			stats.errors++
			return stats, partitions, true
		}

		writer.WriteResult(event.id(), result)

		if result.Status == "skipped" {
			stats.skipped++
		} else {
			stats.handled++
			if result.Partition != "" {
				partitions[result.Partition] = true
			}
		}
	}

	return stats, partitions, false
}

func classifyError(err error) (code, description string) {
	if projErr, ok := err.(gafferruntime.ProjectionError); ok {
		return projErr.ErrorCode(), projErr.ErrorDescription()
	}
	return "unexpected-error", err.Error()
}

func getProjectionInfo(session *gafferruntime.Session) projectionInfo {
	sourcesJSON := session.GetSources()
	if sourcesJSON == nil {
		return projectionInfo{}
	}

	var info projectionInfo
	if err := json.Unmarshal([]byte(*sourcesJSON), &info); err != nil {
		return projectionInfo{}
	}

	return info
}

func buildSessionOptions(cfg *config.Config, proj *config.Projection, debug bool) *string {
	opts := map[string]any{}

	if debug {
		opts["debug"] = true
	}

	if proj.Engine != "" {
		opts["version"] = proj.Engine
	}

	if proj.ExecutionTimeout != nil && *proj.ExecutionTimeout > 0 {
		opts["executionTimeoutMs"] = *proj.ExecutionTimeout
	} else if cfg.ExecutionTimeout != nil && *cfg.ExecutionTimeout > 0 {
		opts["executionTimeoutMs"] = *cfg.ExecutionTimeout
	}

	if cfg.CompilationTimeout != nil && *cfg.CompilationTimeout > 0 {
		opts["compilationTimeoutMs"] = *cfg.CompilationTimeout
	}

	if len(opts) == 0 {
		return nil
	}

	data, err := json.Marshal(opts)
	if err != nil {
		return nil
	}
	s := string(data)
	return &s
}

func buildSummary(session *gafferruntime.Session, info projectionInfo, partitions map[string]bool) summaryState {
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

const zeroUUID = "00000000-0000-0000-0000-000000000000"

func loadEvents(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading events file: %w", err)
	}

	var events []json.RawMessage
	if err := json.Unmarshal(data, &events); err != nil {
		return nil, fmt.Errorf("parsing events file (expected JSON array): %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	result := make([]string, len(events))
	for i, evt := range events {
		var obj map[string]any
		if err := json.Unmarshal(evt, &obj); err != nil {
			return nil, fmt.Errorf("event %d: %w", i+1, err)
		}

		if _, ok := obj["sequenceNumber"]; !ok {
			obj["sequenceNumber"] = i
		}
		if _, ok := obj["isJson"]; !ok {
			obj["isJson"] = true
		}
		if _, ok := obj["eventId"]; !ok {
			obj["eventId"] = zeroUUID
		}
		if _, ok := obj["created"]; !ok {
			obj["created"] = now
		}

		normalized, err := json.Marshal(obj)
		if err != nil {
			return nil, fmt.Errorf("event %d: %w", i+1, err)
		}
		result[i] = string(normalized)
	}

	return result, nil
}
