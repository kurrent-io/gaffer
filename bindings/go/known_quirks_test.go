package gafferruntime

import (
	"strings"
	"testing"
)

func TestKnownQuirks_ReturnsRegistry(t *testing.T) {
	quirks, err := KnownQuirks()
	if err != nil {
		t.Fatalf("KnownQuirks failed: %v", err)
	}
	if len(quirks) == 0 {
		t.Fatal("expected at least one known quirk")
	}

	// Every entry has a non-empty code in the compat.* namespace and a
	// non-empty description.
	for _, b := range quirks {
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

func TestKnownQuirks_IncludesAllExpectedCodes(t *testing.T) {
	quirks, err := KnownQuirks()
	if err != nil {
		t.Fatalf("KnownQuirks failed: %v", err)
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
	codes := make(map[string]bool, len(quirks))
	for _, b := range quirks {
		codes[b.Code] = true
	}
	for _, e := range expected {
		if !codes[e] {
			t.Errorf("expected registry to include %q; got %v", e, codes)
		}
	}
}
