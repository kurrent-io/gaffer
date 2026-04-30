package subscription

import (
	"context"
	"regexp"
	"strings"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
)

func buildFilter(info gafferruntime.QuerySources, engineVersion int) *kurrentdb.SubscriptionFilter {
	sourceFilter := buildSourceFilter(info)

	if info.AllStreams && len(info.Events) > 0 {
		prefixes := make([]string, len(info.Events))
		copy(prefixes, info.Events)
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

func buildSourceFilter(info gafferruntime.QuerySources) *kurrentdb.SubscriptionFilter {
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

// Subscribe subscribes to $all with the correct filter and link resolution
// for the given projection source and engine version, per the subscription spec.
func Subscribe(ctx context.Context, client *kurrentdb.Client, info gafferruntime.QuerySources, engineVersion int) (*kurrentdb.Subscription, error) {
	opts := kurrentdb.SubscribeToAllOptions{
		From:           kurrentdb.Start{},
		ResolveLinkTos: resolveLinkTos(engineVersion),
	}
	if filter := buildFilter(info, engineVersion); filter != nil {
		opts.Filter = filter
	}
	return client.SubscribeToAll(ctx, opts)
}
