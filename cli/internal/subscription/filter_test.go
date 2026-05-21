package subscription

import (
	"testing"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
)

func TestBuildFilter_FromAll_NoEvents(t *testing.T) {
	filter := buildFilter(gafferruntime.ProjectionInfo{AllStreams: true}, 2)
	if filter != nil {
		t.Error("expected nil filter for fromAll with no event filter")
	}
}

func TestBuildFilter_FromAll_WithEvents(t *testing.T) {
	filter := buildFilter(gafferruntime.ProjectionInfo{
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

func TestBuildFilter_FromAll_AllEvents_NoFilter(t *testing.T) {
	// $any handler sets AllEvents=true. The runtime still populates
	// Events with the names of the specific handlers ($UserCreated etc.)
	// alongside $any, so the filter must check AllEvents first - else
	// it narrows to the specific handlers and silently misses every
	// event type the projection meant to catch via $any.
	filter := buildFilter(gafferruntime.ProjectionInfo{
		AllStreams: true,
		AllEvents:  true,
		Events:     []string{"$UserCreated", "$ProjectionCreated"},
	}, 2)

	if filter != nil {
		t.Errorf("expected nil filter (AllEvents=true), got %+v", filter)
	}
}

func TestBuildFilter_FromAll_WithEvents_DeleteHandler(t *testing.T) {
	filter := buildFilter(gafferruntime.ProjectionInfo{
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
	filter := buildFilter(gafferruntime.ProjectionInfo{
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
	filter := buildFilter(gafferruntime.ProjectionInfo{
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
	filter := buildFilter(gafferruntime.ProjectionInfo{
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

func TestBuildSubscribeOptions_ReadWindow(t *testing.T) {
	// Client defaults (32 / 1) make CaughtUp effectively never fire on
	// a busy store. Every Subscribe should override them - see
	// specs/subscription.md "Subscription read parameters".
	cases := []struct {
		name string
		info gafferruntime.ProjectionInfo
	}{
		{"AllStreams", gafferruntime.ProjectionInfo{AllStreams: true}},
		{"AllStreams+Events", gafferruntime.ProjectionInfo{
			AllStreams: true,
			Events:     []string{"OrderPlaced"},
		}},
		{"Categories", gafferruntime.ProjectionInfo{
			Categories: []string{"order"},
		}},
		{"Streams", gafferruntime.ProjectionInfo{
			Streams: []string{"order-1"},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opts := BuildSubscribeOptions(c.info, 2)
			if opts.MaxSearchWindow <= 32 {
				t.Errorf("MaxSearchWindow=%d, want > 32 (client default)", opts.MaxSearchWindow)
			}
			if opts.CheckpointInterval <= 1 {
				t.Errorf("CheckpointInterval=%d, want > 1 (client default)", opts.CheckpointInterval)
			}
		})
	}
}
