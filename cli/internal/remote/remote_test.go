package remote

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeProjAPI records the last call's name and options so the option-mapping
// can be asserted without a live database. Returns err from every method.
type fakeProjAPI struct {
	err error

	lastName    string
	lastQuery   string
	createOpts  kurrentdb.CreateProjectionOptions
	updateOpts  kurrentdb.UpdateProjectionOptions
	deleteOpts  kurrentdb.DeleteProjectionOptions
	resetOpts   kurrentdb.ResetProjectionOptions
	genericOpts kurrentdb.GenericProjectionOptions
	called      string
}

func (f *fakeProjAPI) Create(_ context.Context, name, query string, opts kurrentdb.CreateProjectionOptions) error {
	f.called, f.lastName, f.lastQuery, f.createOpts = "create", name, query, opts
	return f.err
}

func (f *fakeProjAPI) Update(_ context.Context, name, query string, opts kurrentdb.UpdateProjectionOptions) error {
	f.called, f.lastName, f.lastQuery, f.updateOpts = "update", name, query, opts
	return f.err
}

func (f *fakeProjAPI) Delete(_ context.Context, name string, opts kurrentdb.DeleteProjectionOptions) error {
	f.called, f.lastName, f.deleteOpts = "delete", name, opts
	return f.err
}

func (f *fakeProjAPI) Enable(_ context.Context, name string, opts kurrentdb.GenericProjectionOptions) error {
	f.called, f.lastName, f.genericOpts = "enable", name, opts
	return f.err
}

func (f *fakeProjAPI) Disable(_ context.Context, name string, opts kurrentdb.GenericProjectionOptions) error {
	f.called, f.lastName, f.genericOpts = "disable", name, opts
	return f.err
}

func (f *fakeProjAPI) Abort(_ context.Context, name string, opts kurrentdb.GenericProjectionOptions) error {
	f.called, f.lastName, f.genericOpts = "abort", name, opts
	return f.err
}

func (f *fakeProjAPI) Reset(_ context.Context, name string, opts kurrentdb.ResetProjectionOptions) error {
	f.called, f.lastName, f.resetOpts = "reset", name, opts
	return f.err
}

func TestCreatePassesOptions(t *testing.T) {
	fake := &fakeProjAPI{}
	c := &Client{proj: fake}

	if err := c.Create(context.Background(), "orders", "fromAll()", CreateOptions{Emit: true, TrackEmittedStreams: true}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if fake.called != "create" || fake.lastName != "orders" || fake.lastQuery != "fromAll()" {
		t.Fatalf("called %q name %q query %q", fake.called, fake.lastName, fake.lastQuery)
	}
	if !fake.createOpts.RequiresLeader || !fake.createOpts.Emit || !fake.createOpts.TrackEmittedStreams {
		t.Fatalf("create opts = %+v", fake.createOpts)
	}
}

func TestUpdatePassesEmit(t *testing.T) {
	for _, tc := range []struct {
		name string
		emit *bool
	}{
		{"nil leaves emit untouched", nil},
		{"explicit true", testutil.Ptr(true)},
		{"explicit false", testutil.Ptr(false)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeProjAPI{}
			c := &Client{proj: fake}
			if err := c.Update(context.Background(), "orders", "fromAll()", UpdateOptions{Emit: tc.emit}); err != nil {
				t.Fatalf("Update: %v", err)
			}
			if fake.lastQuery != "fromAll()" {
				t.Fatalf("query = %q", fake.lastQuery)
			}
			if !fake.updateOpts.RequiresLeader {
				t.Fatalf("RequiresLeader not set: %+v", fake.updateOpts)
			}
			if tc.emit == nil && fake.updateOpts.Emit != nil {
				t.Fatalf("emit should be nil, got %v", *fake.updateOpts.Emit)
			}
			if tc.emit != nil && (fake.updateOpts.Emit == nil || *fake.updateOpts.Emit != *tc.emit) {
				t.Fatalf("emit = %v, want %v", fake.updateOpts.Emit, *tc.emit)
			}
		})
	}
}

func TestDeletePassesFlags(t *testing.T) {
	fake := &fakeProjAPI{}
	c := &Client{proj: fake}
	opts := DeleteOptions{DeleteEmittedStreams: true, DeleteStateStream: true, DeleteCheckpointStream: true}
	if err := c.Delete(context.Background(), "orders", opts); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got := fake.deleteOpts
	if !got.RequiresLeader || !got.DeleteEmittedStreams || !got.DeleteStateStream || !got.DeleteCheckpointStream {
		t.Fatalf("delete opts = %+v", got)
	}
}

func TestResetWriteCheckpoint(t *testing.T) {
	for _, wc := range []bool{true, false} {
		fake := &fakeProjAPI{}
		c := &Client{proj: fake}
		if err := c.Reset(context.Background(), "orders", wc); err != nil {
			t.Fatalf("Reset: %v", err)
		}
		if fake.lastName != "orders" {
			t.Fatalf("name = %q", fake.lastName)
		}
		if !fake.resetOpts.RequiresLeader || fake.resetOpts.WriteCheckpoint != wc {
			t.Fatalf("reset opts = %+v, want WriteCheckpoint=%v", fake.resetOpts, wc)
		}
	}
}

func TestLifecycleVerbsRouteToLeader(t *testing.T) {
	for _, tc := range []struct {
		verb string
		call func(*Client) error
	}{
		{"enable", func(c *Client) error { return c.Enable(context.Background(), "orders") }},
		{"disable", func(c *Client) error { return c.Disable(context.Background(), "orders") }},
		{"abort", func(c *Client) error { return c.Abort(context.Background(), "orders") }},
	} {
		t.Run(tc.verb, func(t *testing.T) {
			fake := &fakeProjAPI{}
			c := &Client{proj: fake}
			if err := tc.call(c); err != nil {
				t.Fatalf("%s: %v", tc.verb, err)
			}
			if fake.called != tc.verb || !fake.genericOpts.RequiresLeader {
				t.Fatalf("called %q opts %+v", fake.called, fake.genericOpts)
			}
		})
	}
}

func TestMethodsClassifyErrors(t *testing.T) {
	verbs := map[string]func(*Client) error{
		"create":  func(c *Client) error { return c.Create(context.Background(), "orders", "q", CreateOptions{}) },
		"update":  func(c *Client) error { return c.Update(context.Background(), "orders", "q", UpdateOptions{}) },
		"delete":  func(c *Client) error { return c.Delete(context.Background(), "orders", DeleteOptions{}) },
		"enable":  func(c *Client) error { return c.Enable(context.Background(), "orders") },
		"disable": func(c *Client) error { return c.Disable(context.Background(), "orders") },
		"abort":   func(c *Client) error { return c.Abort(context.Background(), "orders") },
		"reset":   func(c *Client) error { return c.Reset(context.Background(), "orders", true) },
	}
	for verb, call := range verbs {
		t.Run(verb, func(t *testing.T) {
			fake := &fakeProjAPI{err: status.New(codes.NotFound, "no such projection").Err()}
			err := call(&Client{proj: fake})
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("want ErrNotFound, got %v", err)
			}
			if !strings.Contains(err.Error(), "no such projection") {
				t.Fatalf("server message dropped: %q", err.Error())
			}
		})
	}
}

func TestClassify(t *testing.T) {
	t.Run("nil passes through", func(t *testing.T) {
		if classify(nil) != nil {
			t.Fatal("classify(nil) should be nil")
		}
	})

	t.Run("unknown grpc code returned unchanged", func(t *testing.T) {
		orig := status.New(codes.Internal, "boom").Err()
		got := classify(orig)
		if !errors.Is(got, orig) {
			t.Fatalf("unknown error should be returned unchanged, got %v", got)
		}
	})

	t.Run("non-status error returned unchanged", func(t *testing.T) {
		orig := errors.New("plain")
		if got := classify(orig); !errors.Is(got, orig) {
			t.Fatalf("plain error should be returned unchanged, got %v", got)
		}
	})

	// FromError wraps an arbitrary error as a *kurrentdb.Error with an unknown
	// code: the only way (without an unexported constructor) to drive classify
	// through the typed errors.As branch. An unknown code stays unwrapped.
	t.Run("typed kurrentdb error with unmapped code passes through", func(t *testing.T) {
		orig := errors.New("opaque server failure")
		kerr, _ := kurrentdb.FromError(orig)
		got := classify(kerr)
		if !errors.Is(got, orig) {
			t.Fatalf("unmapped typed error should preserve the original, got %v", got)
		}
		if errors.Is(got, ErrNotFound) || errors.Is(got, ErrUnavailable) {
			t.Fatalf("unmapped typed error should not classify to a sentinel, got %v", got)
		}
	})

	for _, tc := range []struct {
		code     codes.Code
		sentinel error
	}{
		{codes.NotFound, ErrNotFound},
		{codes.AlreadyExists, ErrAlreadyExists},
		{codes.Unavailable, ErrUnavailable},
		{codes.PermissionDenied, ErrAccessDenied},
		{codes.Unauthenticated, ErrAccessDenied},
	} {
		t.Run(tc.code.String(), func(t *testing.T) {
			err := classify(status.New(tc.code, "server message").Err())
			if !errors.Is(err, tc.sentinel) {
				t.Fatalf("code %v: want %v, got %v", tc.code, tc.sentinel, err)
			}
			if got := err.Error(); !strings.Contains(got, "server message") {
				t.Fatalf("classified error dropped server message: %q", got)
			}
		})
	}
}

func TestFromKurrentCode(t *testing.T) {
	for _, tc := range []struct {
		code     kurrentdb.ErrorCode
		sentinel error
	}{
		{kurrentdb.ErrorCodeResourceNotFound, ErrNotFound},
		{kurrentdb.ErrorCodeResourceAlreadyExists, ErrAlreadyExists},
		{kurrentdb.ErrorUnavailable, ErrUnavailable},
		{kurrentdb.ErrorCodeNotLeader, ErrUnavailable},
		{kurrentdb.ErrorCodeConnectionClosed, ErrUnavailable},
		{kurrentdb.ErrorCodeAccessDenied, ErrAccessDenied},
		{kurrentdb.ErrorCodeUnauthenticated, ErrAccessDenied},
		{kurrentdb.ErrorCodeUnknown, nil},
	} {
		if got := fromKurrentCode(tc.code); !errors.Is(got, tc.sentinel) {
			t.Fatalf("code %v: want %v, got %v", tc.code, tc.sentinel, got)
		}
	}
}
