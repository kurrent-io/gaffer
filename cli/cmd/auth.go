package cmd

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/oauth"
	"github.com/kurrent-io/gaffer/cli/internal/project"
	"github.com/kurrent-io/gaffer/cli/internal/userconfig"
)

// authTimeout bounds the whole interactive login, so a never-completed browser
// flow doesn't hang the command forever.
const authTimeout = 5 * time.Minute

func newAuthCmd() *cobra.Command {
	var envName string

	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authenticate to an environment's OAuth identity provider",
		Long: "Run an interactive browser login (OAuth authorization code + PKCE) and store the\n" +
			"resulting token for the environment's OAuth issuer. Use this for environments\n" +
			"with an [env.<name>.oauth] block. CI should instead set KURRENTDB_OAUTH_CLIENT_SECRET\n" +
			"for the non-interactive client-credentials grant.",
		Example: "gaffer auth --env staging",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAuth(cmd, envName)
		},
	}
	cmd.Flags().StringVar(&envName, "env", "", "Environment to authenticate (defaults to the env marked default)")
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

	ctx, cancel := context.WithTimeout(cmd.Context(), authTimeout)
	defer cancel()
	ctx = oauth.WithHTTPTimeout(ctx, 30*time.Second)

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
	if err := store.Save(oauth.Identity(resolved.OAuth.Issuer, resolved.OAuth.ClientID), tok); err != nil {
		return fmt.Errorf("store token: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Authenticated to env %q. Token stored.\n", resolved.Name)
	return nil
}

// browserOpener prints the authorization URL and best-effort opens it. A failed
// open is not fatal: the user can copy the printed URL.
func browserOpener(cmd *cobra.Command) func(string) error {
	return func(authURL string) error {
		fmt.Fprintf(cmd.ErrOrStderr(),
			"Opening your browser to sign in. If it doesn't open, visit:\n\n  %s\n\n", authURL)
		_ = openBrowser(authURL)
		return nil
	}
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
