package engine

import (
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/config"
)

func TestPrincipalOAuthIsClientID(t *testing.T) {
	// gaffer's OAuth is the client-credentials grant, so the principal is the
	// client_id (the service identity) - no token decode.
	got := Principal("", "", "", &config.OAuthConfig{ClientID: "svc-deployer"})
	if got != "svc-deployer" {
		t.Errorf("Principal = %q, want svc-deployer", got)
	}
}

func TestPrincipalBasicFromConnectionString(t *testing.T) {
	t.Setenv("KURRENTDB_USERNAME", "") // isolate from any ambient overlay creds
	got := Principal("kurrentdb://admin:changeit@localhost:2113", t.TempDir(), "", nil)
	if got != "admin" {
		t.Errorf("Principal = %q, want admin (from connection-string userinfo)", got)
	}
}

func TestPrincipalKurrentUsernameEnvWins(t *testing.T) {
	// KURRENTDB_USERNAME is the override Connect honours over inline userinfo.
	t.Setenv("KURRENTDB_USERNAME", "ops")
	t.Setenv("KURRENTDB_PASSWORD", "secret")
	got := Principal("kurrentdb://admin:changeit@localhost:2113", t.TempDir(), "", nil)
	if got != "ops" {
		t.Errorf("Principal = %q, want ops (KURRENTDB_USERNAME overrides userinfo)", got)
	}
}

func TestPrincipalAnonymousIsEmpty(t *testing.T) {
	t.Setenv("KURRENTDB_USERNAME", "")
	got := Principal("kurrentdb://localhost:2113", t.TempDir(), "", nil)
	if got != "" {
		t.Errorf("Principal = %q, want empty for an anonymous connection", got)
	}
}
