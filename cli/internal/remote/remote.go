// Package remote wraps server-side projection operations against a connected
// KurrentDB: the management RPCs (create, update, delete, enable, disable,
// reset) and, in later slices, reading back deployed projection definitions
// and live status.
//
// It is the deployed-side counterpart to the local project package - project
// reads gaffer.toml (the source of truth on disk), remote reads and mutates
// what is actually running on the server. The status, diff, deploy and operate
// commands all go through this package, so the connection, error shapes and
// option mapping live in one place rather than being re-derived per command.
//
// A remote.Client wraps the *kurrentdb.Client that engine.Connect already
// builds (with basic, certificate or OAuth auth resolved); it never opens its
// own connection.
//
// Every method takes a context: callers should pass one with a deadline. A
// projections subsystem that is still starting replies to a management command
// with nothing, so the call blocks until the context deadline fires rather than
// returning promptly - an unbounded context would hang.
package remote

import (
	"context"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
)

// projectionAPI is the slice of *kurrentdb.ProjectionClient this package drives.
// Seaming it behind an interface keeps the option-mapping testable without a
// live database. *kurrentdb.ProjectionClient satisfies it.
type projectionAPI interface {
	Create(ctx context.Context, name, query string, opts kurrentdb.CreateProjectionOptions) error
	Update(ctx context.Context, name, query string, opts kurrentdb.UpdateProjectionOptions) error
	Delete(ctx context.Context, name string, opts kurrentdb.DeleteProjectionOptions) error
	Enable(ctx context.Context, name string, opts kurrentdb.GenericProjectionOptions) error
	Disable(ctx context.Context, name string, opts kurrentdb.GenericProjectionOptions) error
	Abort(ctx context.Context, name string, opts kurrentdb.GenericProjectionOptions) error
	Reset(ctx context.Context, name string, opts kurrentdb.ResetProjectionOptions) error
	ListContinuous(ctx context.Context, opts kurrentdb.GenericProjectionOptions) ([]kurrentdb.ProjectionStatus, error)
}

// Client performs projection operations against a connected KurrentDB.
type Client struct {
	// db backs the raw stream reads the read substrate needs (later slices:
	// reading the last $ProjectionUpdated to recover a deployed definition).
	// The management verbs only use proj.
	db   *kurrentdb.Client
	proj projectionAPI
}

// New wraps a connected *kurrentdb.Client (from engine.Connect) for projection
// operations. The client's lifecycle stays with the caller; New does not take
// ownership and remote.Client has no Close.
func New(db *kurrentdb.Client) *Client {
	return &Client{db: db, proj: kurrentdb.NewProjectionClientFromExistingClient(db)}
}

// CreateOptions are the create-time settings deploy derives from the local
// projection. Emit and TrackEmittedStreams come from source analysis and config.
type CreateOptions struct {
	// EngineVersion selects the projection engine: 1 or 2. Zero defaults to V1
	// server-side; deploy sets it explicitly from each projection's config.
	// Engine version is fixed at create - there is no Update equivalent, so
	// changing it means delete-and-recreate.
	EngineVersion       int
	Emit                bool
	TrackEmittedStreams bool
}

// UpdateOptions carries the update-time settings. Emit is a pointer because the
// server resets emit to false on any update that omits it, so deploy always
// sends it explicitly; nil leaves the server's current value untouched.
type UpdateOptions struct {
	Emit *bool
}

// DeleteOptions selects which derived streams a delete also removes. All default
// false: a bare delete removes the projection registration but leaves its state,
// checkpoint and emitted streams in place.
type DeleteOptions struct {
	DeleteEmittedStreams   bool
	DeleteStateStream      bool
	DeleteCheckpointStream bool
}

// Projection operations run against the cluster leader: writes are not safe on
// a follower, and the leader is the authoritative source for statistics (the
// projections subsystem runs there). Routing explicitly avoids a NotLeader
// round-trip on a multi-node cluster, and is harmless on a single node.
func leaderOpts() kurrentdb.GenericProjectionOptions {
	return kurrentdb.GenericProjectionOptions{RequiresLeader: true}
}

// Create registers a new continuous projection with the given query. A
// duplicate name is rejected by the projections subsystem with an unclassified
// error, not ErrAlreadyExists (see that sentinel), so callers check existence
// with a read before creating rather than racing a create against the error.
//
// EngineVersion 2 with TrackEmittedStreams is rejected by the client before any
// RPC (V2 does not support tracking emitted streams); deploy validates the
// combination from config rather than relying on that error.
func (c *Client) Create(ctx context.Context, name, query string, opts CreateOptions) error {
	return classify(c.proj.Create(ctx, name, query, kurrentdb.CreateProjectionOptions{
		RequiresLeader:      true,
		EngineVersion:       kurrentdb.ProjectionEngineVersion(opts.EngineVersion),
		Emit:                opts.Emit,
		TrackEmittedStreams: opts.TrackEmittedStreams,
	}))
}

// Update replaces an existing projection's query and emit setting.
func (c *Client) Update(ctx context.Context, name, query string, opts UpdateOptions) error {
	return classify(c.proj.Update(ctx, name, query, kurrentdb.UpdateProjectionOptions{
		RequiresLeader: true,
		Emit:           opts.Emit,
	}))
}

// Delete removes a projection. By default it leaves derived streams in place;
// set the DeleteOptions flags to remove them too.
//
// The projection must be disabled first: deleting an enabled projection is
// rejected by the server with an unclassified error. Callers Disable (and wait
// for it to stop) before Delete.
func (c *Client) Delete(ctx context.Context, name string, opts DeleteOptions) error {
	return classify(c.proj.Delete(ctx, name, kurrentdb.DeleteProjectionOptions{
		RequiresLeader:         true,
		DeleteEmittedStreams:   opts.DeleteEmittedStreams,
		DeleteStateStream:      opts.DeleteStateStream,
		DeleteCheckpointStream: opts.DeleteCheckpointStream,
	}))
}

// Enable starts (or resumes) a projection.
func (c *Client) Enable(ctx context.Context, name string) error {
	return classify(c.proj.Enable(ctx, name, leaderOpts()))
}

// Disable stops a projection, writing a final checkpoint so it resumes from
// where it stopped.
func (c *Client) Disable(ctx context.Context, name string) error {
	return classify(c.proj.Disable(ctx, name, leaderOpts()))
}

// Abort stops a projection without writing a checkpoint; on re-enable it
// resumes from its last persisted checkpoint, replaying anything since.
func (c *Client) Abort(ctx context.Context, name string) error {
	return classify(c.proj.Abort(ctx, name, leaderOpts()))
}

// Reset rewinds a projection to the beginning, discarding its state. The
// projection must be re-enabled to rebuild. writeCheckpoint controls whether
// the reset itself is checkpointed.
func (c *Client) Reset(ctx context.Context, name string, writeCheckpoint bool) error {
	return classify(c.proj.Reset(ctx, name, kurrentdb.ResetProjectionOptions{
		RequiresLeader:  true,
		WriteCheckpoint: writeCheckpoint,
	}))
}
