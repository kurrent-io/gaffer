package gafferruntime

import (
	"strings"
	"testing"
)

func TestKnownBugs_ReturnsRegistry(t *testing.T) {
	bugs, err := KnownBugs()
	if err != nil {
		t.Fatalf("KnownBugs failed: %v", err)
	}
	if len(bugs) == 0 {
		t.Fatal("expected at least one known bug")
	}

	// Every entry has a non-empty code in the compat.* namespace and a
	// non-empty description.
	for _, b := range bugs {
		if b.Code == "" {
			t.Errorf("entry has empty code: %+v", b)
		}
		if !strings.HasPrefix(b.Code, "compat.") {
			t.Errorf("expected compat.* prefix, got %q", b.Code)
		}
		if b.Description == "" {
			t.Errorf("entry has empty description: %+v", b)
		}
	}
}

func TestKnownBugs_IncludesAllExpectedCodes(t *testing.T) {
	bugs, err := KnownBugs()
	if err != nil {
		t.Fatalf("KnownBugs failed: %v", err)
	}

	// Codes that are tracked by the runtime today. Update when the registry
	// changes.
	expected := []string{
		"compat.linkStreamTo.outOfBoundsParameters",
		"compat.log.multiParam",
		"compat.event.bodyCast",
		"compat.biState.stringSlot",
		"compat.serialize.nonFinite",
	}
	codes := make(map[string]bool, len(bugs))
	for _, b := range bugs {
		codes[b.Code] = true
	}
	for _, e := range expected {
		if !codes[e] {
			t.Errorf("expected registry to include %q; got %v", e, codes)
		}
	}
}
