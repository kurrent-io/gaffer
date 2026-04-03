package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/subscription"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var listEventsTool = &mcp.Tool{
	Name:        "list_events",
	Description: "Discover what event types a projection processes by sampling real events from KurrentDB. Uses the projection's source definition to read from the right streams/categories. Returns event types with counts and one example body per type. Requires a KurrentDB connection in gaffer.toml.",
}

type listEventsInput struct {
	Name  string `json:"name" jsonschema:"Projection name from gaffer.toml"`
	Limit int    `json:"limit,omitempty" jsonschema:"Maximum events to sample (default 200, max 2000)"`
}

func (s *Server) handleListEvents(ctx context.Context, _ *mcp.CallToolRequest, input listEventsInput) (*mcp.CallToolResult, any, error) {
	proj := s.cfg.FindProjection(input.Name)
	if proj == nil {
		return toolError("projection %q not found in gaffer.toml", input.Name), nil, nil
	}

	source, err := engine.ReadSource(s.root, proj.Entry)
	if err != nil {
		return toolError("%v", err), nil, nil
	}

	lp := engine.NewProjection(s.root, s.cfg, proj, source)
	session, info, err := engine.CreateSession(lp, false)
	if err != nil {
		return toolError("compiling projection: %v", err), nil, nil
	}
	defer session.Destroy()

	client, err := s.connectToKurrentDB()
	if err != nil {
		return toolError("%v", err), nil, nil
	}
	defer func() { _ = client.Close() }()

	limit := input.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 2000 {
		limit = 2000
	}

	events, err := s.sampleProjectionEvents(ctx, client, info, proj.EffectiveEngine(), uint64(limit))
	if err != nil {
		return toolError("%v", err), nil, nil
	}

	result := map[string]any{
		"projection":   input.Name,
		"eventTypes":   events,
		"totalSampled": countTotal(events),
	}

	src := describeSource(info)
	result["source"] = src
	if len(info.Events) > 0 {
		result["handledEvents"] = info.Events
	}

	return toolResult(result), nil, nil
}

type eventTypeSummary struct {
	EventType string `json:"eventType"`
	Count     int    `json:"count"`
	Example   any    `json:"example"`
}

func (s *Server) sampleProjectionEvents(ctx context.Context, client *kurrentdb.Client, info gafferruntime.QuerySources, engine string, limit uint64) ([]eventTypeSummary, error) {
	filter := subscription.BuildFilter(info, engine)

	// For stream-specific sources, read from those streams directly
	if len(info.Streams) > 0 && !info.AllStreams {
		return s.sampleFromStreams(ctx, client, info.Streams, limit)
	}

	// For category sources, read from $ce-{category} streams
	if len(info.Categories) > 0 && !info.AllStreams {
		return s.sampleFromCategories(ctx, client, info.Categories, limit)
	}

	// For fromAll, use $all with the subscription filter
	return s.sampleFromAll(ctx, client, filter, limit)
}

func (s *Server) sampleFromStreams(ctx context.Context, client *kurrentdb.Client, streams []string, limit uint64) ([]eventTypeSummary, error) {
	perStream := limit / uint64(len(streams))
	if perStream < 10 {
		perStream = 10
	}

	all := []eventTypeSummary{}
	for _, stream := range streams {
		reader, err := client.ReadStream(ctx, stream, kurrentdb.ReadStreamOptions{
			Direction: kurrentdb.Forwards,
			From:      kurrentdb.Start{},
		}, perStream)
		if err != nil {
			all = mergeEventTypes(all, []eventTypeSummary{{
				EventType: fmt.Sprintf("[stream %q not found or not readable]", stream),
				Count:     0,
			}})
			continue
		}

		types, _ := collectEventTypes(reader)
		reader.Close()
		all = mergeEventTypes(all, types)
	}
	return all, nil
}

func (s *Server) sampleFromCategories(ctx context.Context, client *kurrentdb.Client, categories []string, limit uint64) ([]eventTypeSummary, error) {
	perCategory := limit / uint64(len(categories))
	if perCategory < 10 {
		perCategory = 10
	}

	all := []eventTypeSummary{}
	for _, cat := range categories {
		reader, err := client.ReadStream(ctx, "$ce-"+cat, kurrentdb.ReadStreamOptions{
			Direction: kurrentdb.Forwards,
			From:      kurrentdb.Start{},
		}, perCategory)
		if err != nil {
			all = mergeEventTypes(all, []eventTypeSummary{{
				EventType: fmt.Sprintf("[category %q not found - ensure $by_category system projection is enabled]", cat),
				Count:     0,
			}})
			continue
		}

		types, _ := collectEventTypes(reader)
		reader.Close()
		all = mergeEventTypes(all, types)
	}
	return all, nil
}

func (s *Server) sampleFromAll(ctx context.Context, client *kurrentdb.Client, filter *kurrentdb.SubscriptionFilter, limit uint64) ([]eventTypeSummary, error) {
	opts := kurrentdb.ReadAllOptions{
		Direction: kurrentdb.Forwards,
		From:      kurrentdb.Start{},
	}

	reader, err := client.ReadAll(ctx, opts, limit)
	if err != nil {
		return nil, fmt.Errorf("reading $all: %w", err)
	}
	defer reader.Close()

	// ReadAll doesn't accept a filter, so we filter client-side
	return collectEventTypesWithFilter(reader, filter)
}

func collectEventTypes(reader *kurrentdb.ReadStream) ([]eventTypeSummary, error) {
	return collectEventTypesWithFilter(reader, nil)
}

func collectEventTypesWithFilter(reader *kurrentdb.ReadStream, filter *kurrentdb.SubscriptionFilter) ([]eventTypeSummary, error) {
	types := map[string]*eventTypeSummary{}
	order := []string{}

	for {
		resolved, err := reader.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading events: %w", err)
		}

		event := resolved.Event
		if event == nil {
			continue
		}

		if strings.HasPrefix(event.EventType, "$") {
			continue
		}

		if filter != nil && !matchesFilter(event, filter) {
			continue
		}

		existing, ok := types[event.EventType]
		if !ok {
			var example any
			if event.ContentType == "application/json" && len(event.Data) > 0 {
				_ = json.Unmarshal(event.Data, &example)
			}

			summary := &eventTypeSummary{
				EventType: event.EventType,
				Count:     1,
				Example:   example,
			}
			types[event.EventType] = summary
			order = append(order, event.EventType)
		} else {
			existing.Count++
		}
	}

	result := make([]eventTypeSummary, len(order))
	for i, name := range order {
		result[i] = *types[name]
	}
	return result, nil
}

func matchesFilter(event *kurrentdb.RecordedEvent, filter *kurrentdb.SubscriptionFilter) bool {
	var value string
	if filter.Type == kurrentdb.EventFilterType {
		value = event.EventType
	} else {
		value = event.StreamID
	}

	if len(filter.Prefixes) > 0 {
		for _, prefix := range filter.Prefixes {
			if strings.HasPrefix(value, prefix) {
				return true
			}
		}
		return false
	}

	if filter.Regex != "" {
		matched, err := regexp.MatchString(filter.Regex, value)
		return err == nil && matched
	}

	return true
}

func mergeEventTypes(a, b []eventTypeSummary) []eventTypeSummary {
	index := map[string]int{}
	for i, et := range a {
		index[et.EventType] = i
	}
	for _, et := range b {
		if idx, ok := index[et.EventType]; ok {
			a[idx].Count += et.Count
		} else {
			index[et.EventType] = len(a)
			a = append(a, et)
		}
	}
	return a
}

func countTotal(events []eventTypeSummary) int {
	total := 0
	for _, e := range events {
		total += e.Count
	}
	return total
}
