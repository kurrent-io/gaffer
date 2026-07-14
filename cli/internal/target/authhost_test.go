package target

import (
	"strings"
	"testing"
)

func TestAuthHost(t *testing.T) {
	tests := []struct {
		name, conn, want string
	}{
		{"single node", "esdb://localhost:2113?tls=false", "localhost:2113"},
		{"default port applied", "esdb://localhost", "localhost:2113"},
		{"host lowercased", "esdb://KurrentDB.Example:2113", "kurrentdb.example:2113"},
		{"multi-node sorted", "esdb://b.example:2113,a.example:2113", "a.example:2113,b.example:2113"},
		{"duplicates collapse", "esdb://a.example:2113,a.example", "a.example:2113"},
		{"scheme excluded", "esdb+discover://cluster.example:2113", "cluster.example:2113"},
		{"kurrentdb scheme agrees", "kurrentdb://cluster.example", "cluster.example:2113"},
		{"userinfo excluded", "esdb://admin:secret@host.example:2113", "host.example:2113"},
		{"query excluded", "esdb://host.example:2113?tls=false&nodePreference=leader", "host.example:2113"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := authHost(tt.conn)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("authHost(%q) = %q, want %q", tt.conn, got, tt.want)
			}
		})
	}
}

func TestAuthHost_ParseErrorRedactsPassword(t *testing.T) {
	_, err := authHost("esdb://admin:hunter2@host:notaport")
	if err == nil {
		t.Fatal("want error for invalid port")
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Errorf("err %q leaks the password", err)
	}
	if !strings.Contains(err.Error(), "admin:***@") {
		t.Errorf("err %q should carry the redacted connection string", err)
	}
}

func TestAuthHost_PathEchoRedactsPassword(t *testing.T) {
	// A one-slash typo makes url.Parse put everything in the URL path, which
	// the kurrentdb parser echoes as a fragment ("unsupported URL path: ...")
	// that no full-connection-string replacement matches.
	_, err := authHost("esdb:/admin:hunter2@host:2113")
	if err == nil {
		t.Fatal("want error for one-slash connection string")
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Errorf("err %q leaks the password via the path echo", err)
	}
}

func TestAuthHost_QuotedEchoRedactsPassword(t *testing.T) {
	// A control character makes url.Parse fail with a %q-escaped echo of the
	// whole input, so a password containing a quote is spelled differently in
	// the message than in the raw connection string.
	_, err := authHost("esdb://admin:hu\"nter2@host\x01:2113")
	if err == nil {
		t.Fatal("want error for control character in connection string")
	}
	if strings.Contains(err.Error(), "nter2") {
		t.Errorf("err %q leaks the password via the escaped echo", err)
	}
}
