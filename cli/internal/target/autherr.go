package target

import (
	"errors"
	"fmt"

	"github.com/kurrent-io/gaffer/cli/internal/oauth"
)

// AuthRequiredError is returned when an OAuth env can't authenticate without an
// interactive sign-in: no stored token, or a passphrase-locked keyring that
// can't be unlocked non-interactively. It's distinct from a connection failure
// so callers (the --json stream, telemetry) can surface a "sign in" action
// rather than a generic error. Env is the environment to authenticate.
//
// It lives beside target rather than in engine so every Target consumer - the
// connection, the config-drift check, any future one - classifies a missing or
// locked credential the same way, through AsAuthRequired, instead of each
// re-deriving the sentinel check.
type AuthRequiredError struct {
	Env string
}

func (e *AuthRequiredError) Error() string {
	return fmt.Sprintf("env %q requires sign-in: run `gaffer auth --env %s`", e.Env, e.Env)
}

// AsAuthRequired maps the token-store sentinels that mean "no usable stored
// credential" (oauth.ErrNoToken / oauth.ErrKeyringLocked) to an
// *AuthRequiredError for env. Any other error - including nil - passes through
// unchanged. This is the single place a Target consumer turns a missing or
// locked token into the typed sign-in signal.
func AsAuthRequired(env string, err error) error {
	if errors.Is(err, oauth.ErrNoToken) || errors.Is(err, oauth.ErrKeyringLocked) {
		return &AuthRequiredError{Env: env}
	}
	return err
}
