package versiondiff

import (
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/testutil"
)

func TestLocalDescriptorInvalidLocal(t *testing.T) {
	// A local projection that doesn't compile still yields its source for a
	// version diff, but with the compile error surfaced (not swallowed) and no
	// trustworthy hash.
	const broken = `fromAll().when({ $any: function (s, e) { return `
	p := testutil.NewProject(t).AddProjection("bad", broken).Save()

	d, compileErr, err := localDescriptor(p.Cfg, p.Dir, "bad")
	if err != nil {
		t.Fatalf("localDescriptor returned a fatal error for a mere compile failure: %v", err)
	}
	if compileErr == nil {
		t.Fatal("a non-compiling local must return its compile error, not swallow it")
	}
	if d.Query == "" {
		t.Error("the source should still be available to diff")
	}
}

func TestParseRef(t *testing.T) {
	if r, err := ParseRef("deployed"); err != nil || r.Kind != RefDeployed {
		t.Errorf("deployed: got %+v err %v", r, err)
	}
	if r, err := ParseRef("local"); err != nil || r.Kind != RefLocal {
		t.Errorf("local: got %+v err %v", r, err)
	}
	if r, err := ParseRef("9f2a1c"); err != nil || r.Kind != RefHash || r.Hash != "9f2a1c" {
		t.Errorf("hash: got %+v err %v", r, err)
	}
	if _, err := ParseRef("zzz"); err == nil {
		t.Error("a non-hex ref should be rejected")
	}
}

func TestShortRef(t *testing.T) {
	if got := ShortRef("1234567890abcdef"); got != "1234567" {
		t.Errorf("ShortRef long: got %q", got)
	}
	if got := ShortRef("abc"); got != "abc" {
		t.Errorf("ShortRef short: got %q", got)
	}
}
