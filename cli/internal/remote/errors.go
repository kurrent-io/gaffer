package remote

import (
	"errors"
	"fmt"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Typed outcomes callers branch on with errors.Is, instead of pattern-matching
// formatted error strings. Each classified error wraps one of these and keeps
// the server's original message as text for display.
var (
	// ErrNotFound: the named projection does not exist on the server.
	ErrNotFound = errors.New("projection not found")
	// ErrAlreadyExists: a create targeted a projection that already exists.
	//
	// The projections subsystem does not actually report a duplicate create as
	// AlreadyExists - it replies Conflict, which the gRPC layer surfaces as an
	// unclassified Unknown. So this sentinel will not fire for a duplicate
	// Create today; callers must determine existence with a read before
	// creating rather than relying on it. Kept because the mapping is correct
	// should a server version ever return the code.
	ErrAlreadyExists = errors.New("projection already exists")
	// ErrUnavailable: the server or its projections subsystem can't serve the
	// request (node not ready, subsystem stopped, no reachable leader).
	ErrUnavailable = errors.New("KurrentDB unavailable")
	// ErrAccessDenied: the request was rejected for authentication or ACL reasons.
	ErrAccessDenied = errors.New("access denied")
)

// classify maps a projection-operation error to a typed sentinel where it
// recognises one, leaving unrecognised errors wrapped unchanged.
//
// Two error shapes reach here. A typed *kurrentdb.Error comes from the
// Statistics read, from a stream read (a missing stream is ResourceNotFound),
// and from a mutation that fails before its gRPC round-trip (a closed
// connection, surfaced by getConnectionHandle). A mutation that reaches the
// server and is rejected returns a raw gRPC status. classify checks the typed
// error first, then falls back to the gRPC code.
func classify(err error) error {
	if err == nil {
		return nil
	}

	var kerr *kurrentdb.Error
	if errors.As(err, &kerr) {
		if sentinel := fromKurrentCode(kerr.Code()); sentinel != nil {
			return fmt.Errorf("%w: %s", sentinel, kurrentMessage(kerr))
		}
		return err
	}

	if st, ok := status.FromError(err); ok {
		if sentinel := fromGRPCCode(st.Code()); sentinel != nil {
			return fmt.Errorf("%w: %s", sentinel, st.Message())
		}
	}

	return err
}

// kurrentMessage extracts the cleanest message from a *kurrentdb.Error: the
// underlying error when present (the server's own text), otherwise the typed
// error's bracketed description.
func kurrentMessage(kerr *kurrentdb.Error) string {
	if u := kerr.Err(); u != nil {
		return u.Error()
	}
	return kerr.Error()
}

func fromKurrentCode(code kurrentdb.ErrorCode) error {
	switch code {
	case kurrentdb.ErrorCodeResourceNotFound, kurrentdb.ErrorCodeStreamDeleted:
		return ErrNotFound
	case kurrentdb.ErrorCodeResourceAlreadyExists:
		return ErrAlreadyExists
	case kurrentdb.ErrorUnavailable, kurrentdb.ErrorCodeNotLeader, kurrentdb.ErrorCodeConnectionClosed:
		return ErrUnavailable
	case kurrentdb.ErrorCodeAccessDenied, kurrentdb.ErrorCodeUnauthenticated:
		return ErrAccessDenied
	default:
		return nil
	}
}

func fromGRPCCode(code codes.Code) error {
	switch code {
	case codes.NotFound:
		return ErrNotFound
	case codes.AlreadyExists:
		return ErrAlreadyExists
	case codes.Unavailable:
		return ErrUnavailable
	case codes.PermissionDenied, codes.Unauthenticated:
		return ErrAccessDenied
	default:
		return nil
	}
}
