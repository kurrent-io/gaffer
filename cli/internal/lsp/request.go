package lsp

import (
	"errors"
	"fmt"
	"strings"

	"github.com/sourcegraph/jsonrpc2"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/target"
)

// Shared substrate for the warm-connection request handlers (diff, operate, and
// history to come): the ones that borrow the env connection and do a bounded
// network read/write off the read loop. Auth classification and the panic guard
// are identical across them.

// dialError classifies a dial/connect failure: a missing or locked token the dial
// can't satisfy needs sign-in (CodeAuthRequired); anything else is a generic
// internal error carrying a scrubbed, bounded message. Mirrors dialErrStatus on
// the status path.
func dialError(cfg *config.Config, root, env string, err error) *jsonrpc2.Error {
	var authErr *target.AuthRequiredError
	if errors.As(err, &authErr) {
		return authRequiredError(env)
	}
	return &jsonrpc2.Error{Code: jsonrpc2.CodeInternalError, Message: userFacingError(cfg, root, env, err)}
}

// maxUserErrorLen bounds a scrubbed error before it reaches the editor as a
// tooltip or toast: enough to be diagnostic, short enough not to be a wall.
const maxUserErrorLen = 200

// userFacingError renders err for display in the editor - a status tooltip or a
// diff/operate toast. A raw err.Error() can embed the env's resolved connection
// string (with credentials) or span many verbose lines, so scrub the secret (as
// the panic guards do) and bound it to a single line of reasonable length. The
// resolve is best-effort: an env that won't resolve just skips the scrub.
func userFacingError(cfg *config.Config, root, env string, err error) string {
	msg := err.Error()
	if resolved, rerr := cfg.ResolveEnv(env); rerr == nil {
		msg = scrubConnection(msg, root, resolved)
	}
	return boundLine(msg)
}

// boundLine reduces msg to its first non-empty line and caps its length,
// appending an ellipsis when truncated. Rune-aware so it never splits a
// multi-byte character; it scans at most maxUserErrorLen+1 runes rather than
// materialising the whole (possibly large) line as a rune slice.
func boundLine(msg string) string {
	msg = strings.TrimSpace(msg)
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = strings.TrimSpace(msg[:i])
	}
	runes := 0
	for i := range msg {
		if runes == maxUserErrorLen {
			return msg[:i] + "…"
		}
		runes++
	}
	return msg
}

func authRequiredError(env string) *jsonrpc2.Error {
	return &jsonrpc2.Error{
		Code:    CodeAuthRequired,
		Message: fmt.Sprintf("sign-in required for env %q", env),
	}
}

// guardedOp runs op with the same panic guard as safeFetch: a crash deep in the
// KurrentDB client (e.g. a nil-deref on an unready projection subsystem) surfaces
// as an error instead of taking down the language server. A handler panic is
// unrecovered whether it runs on the read loop or, for a blocking request, on its
// own goroutine (see offloadBlocking), so it's fatal either way without this. The
// panic value is scrubbed of the env's connection secret before logging; op is a
// parameter so the guard is testable without a live client, and label names the
// operation in the log line and the returned error.
func guardedOp[T any](cfg *config.Config, root, env, label string, op func() (T, error)) (result T, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			logScrubbedPanic(cfg, root, env, label, rec)
			err = fmt.Errorf("%s failed unexpectedly", label)
		}
	}()
	return op()
}
