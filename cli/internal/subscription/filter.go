package subscription

import (
	"regexp"
	"strings"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
)

type SourceInfo struct {
	AllStreams                  bool
	Categories                  []string
	Streams                     []string
	Events                      []string
	HandlesDeletedNotifications bool
}

func BuildFilter(info SourceInfo, version string) *kurrentdb.SubscriptionFilter {
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

func buildSourceFilter(info SourceInfo) *kurrentdb.SubscriptionFilter {
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

func ResolveLinkTos(version string) bool {
	return version == "v2"
}
