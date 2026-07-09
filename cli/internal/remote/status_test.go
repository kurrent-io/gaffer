package remote

import (
	"context"
	"errors"
	"testing"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestClassifyState(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want State
	}{
		{"Running", StateRunning},
		{"Stopped", StateStopped},
		{"Faulted", StateFaulted},
		{"Stopped/Faulted (Enabled)", StateFaulted}, // composite: faulted wins
		{"Stopped (Enabled)", StateStopped},
		{"Completed", StateStopped},
		{"Starting", StateStopped},         // does not contain "Running"
		{"Aborted", StateAborted},          // killed without a final checkpoint
		{"Aborted/Stopped", StateAborted},  // composite: aborted still wins over the trailing stopped
		{"Aborting", StateStopped},         // in-flight kill: settled "Aborted" only
		{"Aborting/Running", StateRunning}, // still running until the kill lands
		{"", StateUnknown},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			if got := classifyState(tc.raw); got != tc.want {
				t.Errorf("classifyState(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestListFiltersSystemProjectionsAndMaps(t *testing.T) {
	fake := &fakeProjAPI{listResult: []kurrentdb.ProjectionStatus{
		{Name: "$by_category", Status: "Running"},
		{Name: "orders", Status: "Running", Position: "C:120/P:118", Progress: 99.5, Mode: "Continuous"},
		{Name: "$", Status: "Running"}, // a bare "$" is still a system name
		{Name: "totals", Status: "Stopped"},
		{Name: "$streams", Status: "Running"},
	}}
	c := &Client{proj: fake}

	got, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !fake.genericOpts.RequiresLeader {
		t.Fatalf("List should route to leader: %+v", fake.genericOpts)
	}
	if len(got) != 2 || got[0].Name != "orders" || got[1].Name != "totals" {
		t.Fatalf("want [orders totals] in input order, got %+v", got)
	}
	o := got[0]
	if o.State != StateRunning || o.Raw != "Running" || o.Position != "C:120/P:118" || o.Progress != 99.5 || o.Mode != "Continuous" {
		t.Fatalf("mapped status = %+v", o)
	}
}

func TestListEmpty(t *testing.T) {
	c := &Client{proj: &fakeProjAPI{listResult: nil}}
	got, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("want empty non-nil slice, got %#v", got)
	}
}

func TestStatusFaultReason(t *testing.T) {
	fake := &fakeProjAPI{listResult: []kurrentdb.ProjectionStatus{
		{Name: "good", Status: "Running", StateReason: "should be ignored"},
		{Name: "bad", Status: "Faulted", StateReason: "TypeError: x is undefined"},
	}}
	c := &Client{proj: fake}

	bad, err := c.Status(context.Background(), "bad")
	if err != nil {
		t.Fatalf("Status(bad): %v", err)
	}
	if bad.State != StateFaulted || bad.Raw != "Faulted" || bad.FaultReason != "TypeError: x is undefined" {
		t.Fatalf("faulted status = %+v", bad)
	}

	good, err := c.Status(context.Background(), "good")
	if err != nil {
		t.Fatalf("Status(good): %v", err)
	}
	if good.FaultReason != "" {
		t.Fatalf("non-faulted projection should carry no fault reason, got %q", good.FaultReason)
	}
}

func TestStatusNotFound(t *testing.T) {
	fake := &fakeProjAPI{listResult: []kurrentdb.ProjectionStatus{{Name: "orders", Status: "Running"}}}
	c := &Client{proj: fake}

	_, err := c.Status(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestExists(t *testing.T) {
	fake := &fakeProjAPI{listResult: []kurrentdb.ProjectionStatus{{Name: "orders", Status: "Running"}}}
	c := &Client{proj: fake}

	if ok, err := c.Exists(context.Background(), "orders"); err != nil || !ok {
		t.Fatalf("Exists(orders) = %v, %v; want true, nil", ok, err)
	}
	if ok, err := c.Exists(context.Background(), "missing"); err != nil || ok {
		t.Fatalf("Exists(missing) = %v, %v; want false, nil", ok, err)
	}
}

func TestExistsPropagatesError(t *testing.T) {
	fake := &fakeProjAPI{err: status.New(codes.Unavailable, "node not ready").Err()}
	c := &Client{proj: fake}

	ok, err := c.Exists(context.Background(), "orders")
	if ok {
		t.Fatalf("Exists should be false on error, got true")
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
}

func TestStatusesClassifiesListError(t *testing.T) {
	fake := &fakeProjAPI{err: status.New(codes.Unavailable, "node not ready").Err()}
	c := &Client{proj: fake}

	if _, err := c.List(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
}
