package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/envvar"
	"github.com/kurrent-io/gaffer/cli/internal/oauth"
	"github.com/kurrent-io/gaffer/cli/internal/project"
	"github.com/kurrent-io/gaffer/cli/internal/target"
	"github.com/kurrent-io/gaffer/cli/internal/userconfig"
)

// authTimeout bounds the whole interactive login, so a never-completed browser
// flow doesn't hang the command forever.
const authTimeout = 5 * time.Minute

func newAuthCmd() *cobra.Command {
	var envName string
	var clear bool

	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authenticate to an environment's OAuth identity provider",
		Long: "Signs in to the environment's OAuth identity provider with an interactive browser\n" +
			"login (authorization code + PKCE) and stores the resulting token, which gaffer\n" +
			"refreshes automatically. It applies to environments configured for OAuth in\n" +
			"gaffer.toml. For CI, set KURRENTDB_OAUTH_CLIENT_SECRET instead to use the\n" +
			"non-interactive client-credentials grant.\n\n" +
			"The token is bound to the host the environment's connection names and is only\n" +
			"ever sent there. Environments pointing at the same host share one sign-in;\n" +
			"a different host needs its own. The connection string must resolve to name\n" +
			"that host, so an unset ${VAR} or an unparseable connection fails the sign-in.\n\n" +
			"--clear removes every stored token, signing out of all environments. Use it to\n" +
			"reset a keyring whose passphrase has been forgotten; it needs neither the\n" +
			"passphrase nor a gaffer project.\n\n" +
			"GAFFER_NO_OPEN prints the authorization URL instead of opening a browser.\n" +
			"GAFFER_KEYRING_PASSWORD supplies the keyring passphrase on a host without an OS keyring.",
		Example: "gaffer auth --env staging",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if clear {
				return runAuthClear(cmd)
			}
			return runAuth(cmd, envName)
		},
	}
	cmd.Flags().StringVar(&envName, "env", "", "Environment to authenticate (defaults to the env marked default)")
	cmd.Flags().BoolVar(&clear, "clear", false, "Remove every stored token, signing out of all environments")
	cmd.MarkFlagsMutuallyExclusive("env", "clear")
	return cmd
}

// resolveOAuthEnv resolves the named env and requires it to have OAuth
// configured. Split out so the resolution and error paths are unit-testable
// without a project on disk, a keyring, or a browser.
func resolveOAuthEnv(cfg *config.Config, envName string) (config.ResolvedEnv, error) {
	resolved, err := cfg.ResolveEnv(envName)
	if err != nil {
		return config.ResolvedEnv{}, err
	}
	if resolved.OAuth == nil {
		return config.ResolvedEnv{}, fmt.Errorf("env %q has no [env.%s.oauth] configuration", resolved.Name, resolved.Name)
	}
	return resolved, nil
}

func runAuth(cmd *cobra.Command, envName string) error {
	root := project.FindRoot()
	if root == "" {
		return project.ErrNotInProject
	}
	cfg, err := config.Load(project.ConfigPath(root))
	if err != nil {
		return err
	}
	resolved, err := resolveOAuthEnv(cfg, envName)
	if err != nil {
		return err
	}

	// The token is stored bound to the host the env's connection names
	// (oauth.Identity), so resolve the target before the browser round-trip:
	// an env whose connection can't be expanded or parsed has no host to
	// bind a token to, and should fail here rather than after a sign-in.
	if err := envvar.Load(root); err != nil {
		return err
	}
	tgt, err := target.Resolve(root, resolved)
	if err != nil {
		return fmt.Errorf("cannot determine the host to bind the token to: %w", err)
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), authTimeout)
	defer cancel()
	ctx, err = oauth.WithHTTPClient(ctx, 30*time.Second, tgt.OAuthCAFile)
	if err != nil {
		return err
	}

	tok, err := oauth.Login(ctx, oauth.Config{
		Issuer:   resolved.OAuth.Issuer,
		ClientID: resolved.OAuth.ClientID,
		Scopes:   resolved.OAuth.Scopes,
		Audience: resolved.OAuth.Audience,
	}, browserOpener(cmd))
	if err != nil {
		return err
	}

	dir, err := userconfig.DefaultDir()
	if err != nil {
		return err
	}
	store, err := oauth.OpenTokenStore(dir)
	if err != nil {
		return err
	}
	if err := store.Save(tgt.OAuthIdentity(), tok); err != nil {
		return fmt.Errorf("store token: %w", err)
	}

	// Name the host the token is bound to: sign-ins are per host, and this
	// makes that visible when a project has several OAuth envs.
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Authenticated to env %q (%s). Token stored.\n", resolved.Name, tgt.AuthHost)
	return nil
}

// runAuthClear removes every stored token. It deliberately resolves nothing but
// the user-config directory: clearing must work from outside a project and
// without the keyring passphrase, so a forgotten passphrase is recoverable.
func runAuthClear(cmd *cobra.Command) error {
	dir, err := userconfig.DefaultDir()
	if err != nil {
		return err
	}
	store, err := oauth.OpenTokenStore(dir)
	if err != nil {
		return err
	}
	n, err := store.Clear()
	if err != nil {
		return fmt.Errorf("clear tokens: %w", err)
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Cleared %d stored token(s).\n", n)
	return nil
}

// browserOpener prints the authorization URL and best-effort opens it. A failed
// open is not fatal: the user can copy the printed URL. Setting GAFFER_NO_OPEN
// skips the open entirely (for headless, CI, or scripted logins).
func browserOpener(cmd *cobra.Command) func(string) error {
	return func(authURL string) error {
		if os.Getenv("GAFFER_NO_OPEN") != "" {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Visit this URL to sign in:\n\n  %s\n\n", authURL)
			return nil
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
			"Opening your browser to sign in. If it doesn't open, visit:\n\n  %s\n\n", authURL)
		_ = openBrowser(authURL)
		return nil
	}
}

// openBrowser is a var so tests can confirm GAFFER_NO_OPEN suppresses it.
var openBrowser = func(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
