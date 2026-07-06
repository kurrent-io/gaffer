// Package apply executes a deploy plan - the write half of the read side
// the drift package computes. Shared by gaffer deploy and the MCP deploy
// tool so the RPC bindings, ledger stamping, and failure accounting can't
// drift between surfaces.
package apply

import (
	"context"

	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

// Manager is the slice of remote.Client the apply step needs: create and
// update, plus the disable/reset/enable a logic-change rebuild sequences.
// Seaming it lets the orchestration be tested without a live database;
// *remote.Client satisfies it.
type Manager interface {
	Create(ctx context.Context, name, query string, opts remote.CreateOptions) error
	Update(ctx context.Context, name, query string, opts remote.UpdateOptions) error
	Disable(ctx context.Context, name string) error
	Reset(ctx context.Context, name string, writeCheckpoint bool) error
	Enable(ctx context.Context, name string) error
}

// Plan executes a plan, reporting progress through the callbacks, and
// returns how many failed (an apply error) or were refused. It applies only
// create/update/reset items; skip/refuse/planning-error items stream their
// verdict unchanged. It continues past a failure so the summary is
// complete; the caller turns a non-zero count into a non-zero exit or tool
// error. onStart fires before an item's RPCs (nil to skip); onDone fires
// with every item's outcome.
func Plan(ctx context.Context, plan []drift.PlanItem, mgr Manager, ledger remote.Ledger, onStart func(name string, index, total int), onDone func(drift.Result)) (failed int) {
	total := len(plan)
	for i := range plan {
		item := plan[i]
		if ctx.Err() != nil {
			break
		}
		if onStart != nil {
			onStart(item.Name, i+1, total)
		}
		res := item.Result()
		if item.Err == nil && item.Action.Applies() {
			if err := Action(ctx, mgr, item.Name, item.Action, item.Cmp.Local, ledger); err != nil {
				res.Err = err
				// The apply failed, so nothing was overwritten - don't keep claiming
				// it did via externalChange.
				res.ExternalChange = false
			}
		}
		if res.Err != nil || res.Action == drift.ActionRefuse {
			failed++
		}
		onDone(res)
	}
	return failed
}

// rpc bounds one server call by the shared RPC timeout, so every step of a
// multi-step apply gets a full budget rather than sharing one.
func rpc(ctx context.Context, fn func(context.Context) error) error {
	rctx, cancel := context.WithTimeout(ctx, deploy.RPCTimeout)
	defer cancel()
	return fn(rctx)
}

// Action performs the create, update, or logic-change reset. Emit is always
// sent on update (as a non-nil pointer) because the server resets it to false on
// any update that omits it. A created continuous projection starts enabled
// server-side, so there is no separate enable step.
func Action(ctx context.Context, mgr Manager, name string, action drift.Action, local *deploy.Descriptor, ledger remote.Ledger) error {
	switch action {
	case drift.ActionCreate:
		return rpc(ctx, func(ctx context.Context) error {
			return mgr.Create(ctx, name, local.Query, remote.CreateOptions{
				EngineVersion:       local.EngineVersion,
				Emit:                local.Emit,
				TrackEmittedStreams: local.TrackEmittedStreams,
				Ledger:              &ledger,
			})
		})
	case drift.ActionUpdate:
		return rpc(ctx, func(ctx context.Context) error {
			return mgr.Update(ctx, name, local.Query, remote.UpdateOptions{Emit: emitPtr(local), Ledger: &ledger})
		})
	case drift.ActionReset:
		// The destructive Disable -> Update -> Reset -> Enable sequence, its
		// ordering and recovery messages, live in internal/deploy; here we only bind
		// each step to the client and its option mapping. emitPtr keeps the update's
		// explicit-emit guarantee.
		return deploy.Rebuild(ctx, name, deploy.RebuildSteps{
			Disable: func(ctx context.Context) error { return mgr.Disable(ctx, name) },
			Update: func(ctx context.Context) error {
				return mgr.Update(ctx, name, local.Query, remote.UpdateOptions{Emit: emitPtr(local), Ledger: &ledger})
			},
			Reset:  func(ctx context.Context) error { return mgr.Reset(ctx, name, true) },
			Enable: func(ctx context.Context) error { return mgr.Enable(ctx, name) },
		})
	default:
		return nil
	}
}

// emitPtr returns a non-nil pointer to the descriptor's derived emit flag, so an
// update always sends it explicitly (the server clears emit on any update that
// omits it).
func emitPtr(local *deploy.Descriptor) *bool {
	emit := local.Emit
	return &emit
}
