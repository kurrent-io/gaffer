package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var listEventsTool = &mcp.Tool{
	Name:        "list_events",
	Description: "Discover event types by reading events from the start of a stream or $all. Returns event types with counts and one example body per type. Reads up to 500 events by default (max 10000). System events ($-prefixed) are excluded. Requires a KurrentDB connection in gaffer.toml.",
}

type listEventsInput struct {
	Category string `json:"category,omitempty" jsonschema:"Filter to a specific category (e.g. 'order' reads streams like order-1, order-2)"`
	Stream   string `json:"stream,omitempty" jsonschema:"Filter to a specific stream"`
	Limit    int    `json:"limit,omitempty" jsonschema:"Maximum events to sample (default 500)"`
}

func (s *Server) handleListEvents(ctx context.Context, _ *mcp.CallToolRequest, input listEventsInput) (*mcp.CallToolResult, any, error) {
	client, err := s.connectToKurrentDB()
	if err != nil {
		return toolError("%v", err), nil, nil
	}
	defer func() { _ = client.Close() }()

	limit := input.Limit
	if limit <= 0 {
		limit = 500
	}
	if limit > 10000 {
		limit = 10000
	}

	events, err := s.readEvents(ctx, client, input, uint64(limit))
	if err != nil {
		return toolError("%v", err), nil, nil
	}

	return toolResult(map[string]any{
		"eventTypes":   events,
		"totalSampled": countTotal(events),
	}), nil, nil
}

type eventTypeSummary struct {
	EventType string `json:"eventType"`
	Count     int    `json:"count"`
	Example   any    `json:"example"`
}

func (s *Server) readEvents(ctx context.Context, client *kurrentdb.Client, input listEventsInput, limit uint64) ([]eventTypeSummary, error) {
	if input.Stream != "" {
		return s.readFromStream(ctx, client, input.Stream, limit)
	}
	if input.Category != "" {
		return s.readFromStream(ctx, client, "$ce-"+input.Category, limit)
	}
	return s.readFromAll(ctx, client, limit)
}

func (s *Server) readFromStream(ctx context.Context, client *kurrentdb.Client, stream string, limit uint64) ([]eventTypeSummary, error) {
	reader, err := client.ReadStream(ctx, stream, kurrentdb.ReadStreamOptions{
		Direction: kurrentdb.Forwards,
		From:      kurrentdb.Start{},
	}, limit)
	if err != nil {
		return nil, fmt.Errorf("reading stream %q: %w", stream, err)
	}
	defer reader.Close()

	return collectEventTypes(reader)
}

func (s *Server) readFromAll(ctx context.Context, client *kurrentdb.Client, limit uint64) ([]eventTypeSummary, error) {
	opts := kurrentdb.ReadAllOptions{
		Direction: kurrentdb.Forwards,
		From:      kurrentdb.Start{},
	}

	reader, err := client.ReadAll(ctx, opts, limit)
	if err != nil {
		return nil, fmt.Errorf("reading $all: %w", err)
	}
	defer reader.Close()

	return collectEventTypes(reader)
}

func collectEventTypes(reader *kurrentdb.ReadStream) ([]eventTypeSummary, error) {
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

func countTotal(events []eventTypeSummary) int {
	total := 0
	for _, e := range events {
		total += e.Count
	}
	return total
}
