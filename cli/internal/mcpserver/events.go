package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
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
	session, info, err := engine.CreateSession(lp, false, false)
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

	events, err := s.sampleProjectionEvents(ctx, client, info, s.cfg.EffectiveEngineVersion(proj), limit)
	if err != nil {
		return toolError("%v", err), nil, nil
	}

	result := map[string]any{
		"projection":   input.Name,
		"eventTypes":   events,
		"totalSampled": countTotal(events),
	}

	src := engine.DescribeSource(info)
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

func (s *Server) sampleProjectionEvents(ctx context.Context, client *kurrentdb.Client, info gafferruntime.ProjectionInfo, engineVersion int, limit int) ([]eventTypeSummary, error) {
	sub, err := subscription.Subscribe(ctx, client, info, engineVersion)
	if err != nil {
		return nil, fmt.Errorf("subscribing: %w", err)
	}
	defer func() { _ = sub.Close() }()

	types := map[string]*eventTypeSummary{}
	order := []string{}
	count := 0

	for count < limit {
		subEvent := sub.Recv()

		if subEvent.SubscriptionDropped != nil {
			return nil, fmt.Errorf("subscription dropped: %w", subEvent.SubscriptionDropped.Error)
		}
		if subEvent.CaughtUp != nil {
			break
		}
		if subEvent.EventAppeared == nil {
			continue
		}

		event := subEvent.EventAppeared.Event
		if event == nil || strings.HasPrefix(event.EventType, "$") {
			continue
		}

		count++
		existing, ok := types[event.EventType]
		if !ok {
			var example any
			if event.ContentType == "application/json" && len(event.Data) > 0 {
				_ = json.Unmarshal(event.Data, &example)
			}
			types[event.EventType] = &eventTypeSummary{
				EventType: event.EventType,
				Count:     1,
				Example:   example,
			}
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
