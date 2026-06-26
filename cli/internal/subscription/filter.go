package subscription

import (
	"context"
	"regexp"
	"slices"
	"strings"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
)

func buildFilter(info gafferruntime.ProjectionInfo, engineVersion int) *kurrentdb.SubscriptionFilter {
	sourceFilter := buildSourceFilter(info)

	// AllEvents is set by a $any handler; in that case every event
	// type is handled, so no event-type filter applies. Events is
	// still populated with the specific handler names regardless,
	// so the AllEvents check has to come before the len(Events) one.
	if info.AllStreams && !info.AllEvents && len(info.Events) > 0 {
		prefixes := slices.Clone(info.Events)
		if info.HandlesDeletedNotifications {
			prefixes = append(prefixes, "$streamDeleted", "$metadata")
		}
		return &kurrentdb.SubscriptionFilter{
			Type:     kurrentdb.EventFilterType,
			Prefixes: prefixes,
		}
	}

	return sourceFilter
}

func buildSourceFilter(info gafferruntime.ProjectionInfo) *kurrentdb.SubscriptionFilter {
	if info.AllStreams {
		return nil
	}

	if len(info.Streams) > 0 {
		if allCategoryStreams(info.Streams) {
			categories := make([]string, len(info.Streams))
			for i, s := range info.Streams {
				categories[i] = strings.TrimPrefix(s, "$ce-") + "-"
			}
			return &kurrentdb.SubscriptionFilter{
				Type:     kurrentdb.StreamFilterType,
				Prefixes: categories,
			}
		}

		escaped := make([]string, len(info.Streams))
		for i, s := range info.Streams {
			escaped[i] = regexp.QuoteMeta(s)
		}
		return &kurrentdb.SubscriptionFilter{
			Type:  kurrentdb.StreamFilterType,
			Regex: "^(" + strings.Join(escaped, "|") + ")$",
		}
	}

	if len(info.Categories) > 0 {
		prefixes := make([]string, len(info.Categories))
		for i, c := range info.Categories {
			prefixes[i] = c + "-"
		}
		return &kurrentdb.SubscriptionFilter{
			Type:     kurrentdb.StreamFilterType,
			Prefixes: prefixes,
		}
	}

	return nil
}

func allCategoryStreams(streams []string) bool {
	for _, s := range streams {
		if !strings.HasPrefix(s, "$ce-") {
			return false
		}
	}
	return true
}

func resolveLinkTos(engineVersion int) bool {
	return engineVersion == 2
}

// BuildSubscribeOptions assembles the subscribe-to-$all options for a
// projection. Pure function - exposed separately from Subscribe so the
// options shape (including read-window tuning) is unit-testable.
//
// MaxSearchWindow + CheckpointInterval override the client defaults
// (32 / 1) which produce a checkpoint per ~32 events read. On a busy
// store with a narrow filter that means thousands of network round-
// trips before catch-up; verified empirically that the CLI never
// reaches CaughtUp against the demo cloud instance under defaults.
// 10000 / 10 lets the server scan multi-GB regions of $all in seconds.
func BuildSubscribeOptions(info gafferruntime.ProjectionInfo, engineVersion int) kurrentdb.SubscribeToAllOptions {
	opts := kurrentdb.SubscribeToAllOptions{
		From:               kurrentdb.Start{},
		ResolveLinkTos:     resolveLinkTos(engineVersion),
		MaxSearchWindow:    10000,
		CheckpointInterval: 10,
	}
	if filter := buildFilter(info, engineVersion); filter != nil {
		opts.Filter = filter
	}
	return opts
}

// Subscribe subscribes to $all with the correct filter and link resolution
// for the given projection source and engine version, per the subscription spec.
func Subscribe(ctx context.Context, client *kurrentdb.Client, info gafferruntime.ProjectionInfo, engineVersion int) (*kurrentdb.Subscription, error) {
	return client.SubscribeToAll(ctx, BuildSubscribeOptions(info, engineVersion))
}
