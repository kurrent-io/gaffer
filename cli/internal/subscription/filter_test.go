package subscription

import (
	"testing"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
)

func TestBuildFilter_FromAll_NoEvents(t *testing.T) {
	filter := buildFilter(gafferruntime.QuerySources{AllStreams: true}, 2)
	if filter != nil {
		t.Error("expected nil filter for fromAll with no event filter")
	}
}

func TestBuildFilter_FromAll_WithEvents(t *testing.T) {
	filter := buildFilter(gafferruntime.QuerySources{
		AllStreams: true,
		Events:     []string{"OrderPlaced", "OrderShipped"},
	}, 2)

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
	filter := buildFilter(gafferruntime.QuerySources{
		AllStreams:                  true,
		Events:                      []string{"OrderPlaced"},
		HandlesDeletedNotifications: true,
	}, 2)

	if filter == nil {
		t.Fatal("expected filter")
	}
	if len(filter.Prefixes) != 3 {
		t.Fatalf("expected 3 prefixes (event + $streamDeleted + $metadata), got %d", len(filter.Prefixes))
	}
}

func TestBuildFilter_FromCategory(t *testing.T) {
	filter := buildFilter(gafferruntime.QuerySources{
		Categories: []string{"order"},
	}, 2)

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
	filter := buildFilter(gafferruntime.QuerySources{
		Streams: []string{"order-1", "cart-1"},
	}, 2)

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
	filter := buildFilter(gafferruntime.QuerySources{
		Streams: []string{"$ce-order", "$ce-cart"},
	}, 2)

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

func TestLinkResolution(t *testing.T) {
	if resolveLinkTos(1) {
		t.Error("v1 should not resolve links")
	}
	if !resolveLinkTos(2) {
		t.Error("v2 should resolve links")
	}
}
