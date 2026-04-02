package subscription

import (
	"testing"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	"github.com/kurrent-io/gaffer/cli/internal/projection"
)

func TestBuildFilter_FromAll_NoEvents(t *testing.T) {
	filter := BuildFilter(projection.Info{AllStreams: true}, "v2")
	if filter != nil {
		t.Error("expected nil filter for fromAll with no event filter")
	}
}

func TestBuildFilter_FromAll_WithEvents(t *testing.T) {
	filter := BuildFilter(projection.Info{
		AllStreams: true,
		Events:     []string{"OrderPlaced", "OrderShipped"},
	}, "v2")

	if filter == nil {
		t.Fatal("expected filter")
	}
	if filter.Type != kurrentdb.EventFilterType {
		t.Error("expected event filter type")
	}
	if len(filter.Prefixes) != 2 {
		t.Fatalf("expected 2 prefixes, got %d", len(filter.Prefixes))
	}
}

func TestBuildFilter_FromAll_WithEvents_DeleteHandler(t *testing.T) {
	filter := BuildFilter(projection.Info{
		AllStreams:                  true,
		Events:                      []string{"OrderPlaced"},
		HandlesDeletedNotifications: true,
	}, "v2")

	if filter == nil {
		t.Fatal("expected filter")
	}
	if len(filter.Prefixes) != 3 {
		t.Fatalf("expected 3 prefixes (event + $streamDeleted + $metadata), got %d", len(filter.Prefixes))
	}
}

func TestBuildFilter_FromCategory(t *testing.T) {
	filter := BuildFilter(projection.Info{
		Categories: []string{"order"},
	}, "v2")

	if filter == nil {
		t.Fatal("expected filter")
	}
	if filter.Type != kurrentdb.StreamFilterType {
		t.Error("expected stream filter type")
	}
	if len(filter.Prefixes) != 1 || filter.Prefixes[0] != "order-" {
		t.Errorf("expected prefix 'order-', got %v", filter.Prefixes)
	}
}

func TestBuildFilter_FromStreams(t *testing.T) {
	filter := BuildFilter(projection.Info{
		Streams: []string{"order-1", "cart-1"},
	}, "v2")

	if filter == nil {
		t.Fatal("expected filter")
	}
	if filter.Type != kurrentdb.StreamFilterType {
		t.Error("expected stream filter type")
	}
	if filter.Regex == "" {
		t.Error("expected regex filter for named streams")
	}
}

func TestBuildFilter_FromCategoryMultiArg(t *testing.T) {
	filter := BuildFilter(projection.Info{
		Streams: []string{"$ce-order", "$ce-cart"},
	}, "v2")

	if filter == nil {
		t.Fatal("expected filter")
	}
	if filter.Type != kurrentdb.StreamFilterType {
		t.Error("expected stream filter type")
	}
	if len(filter.Prefixes) != 2 {
		t.Fatalf("expected 2 prefixes, got %d", len(filter.Prefixes))
	}
	if filter.Prefixes[0] != "order-" || filter.Prefixes[1] != "cart-" {
		t.Errorf("expected category prefixes, got %v", filter.Prefixes)
	}
}

func TestResolveLinkTos(t *testing.T) {
	if ResolveLinkTos("v1") {
		t.Error("v1 should not resolve links")
	}
	if !ResolveLinkTos("v2") {
		t.Error("v2 should resolve links")
	}
}
