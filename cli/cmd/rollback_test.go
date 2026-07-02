package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

func TestNormalizeHashPrefix(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    string
		wantErr string
	}{
		{"23e1fa6", "23e1fa6", ""},
		{"23E1FA6", "23e1fa6", ""}, // case-insensitive, like git
		{"abcd", "abcd", ""},       // minimum length
		{"abc", "", "too short"},
		{strings.Repeat("a", 64), strings.Repeat("a", 64), ""},
		{strings.Repeat("a", 65), "", "longer than a full content hash"},
		{"23e1fg6", "", "not hexadecimal"},
		{"origin/x", "", "not hexadecimal"},
	} {
		got, err := normalizeHashPrefix(tc.in)
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("normalizeHashPrefix(%q) err = %v, want %q", tc.in, err, tc.wantErr)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("normalizeHashPrefix(%q) = (%q, %v), want %q", tc.in, got, err, tc.want)
		}
	}
}

func TestMatchAndResolveHashes(t *testing.T) {
	// Three raw versions: two occurrences of the same content (one match, not an
	// ambiguity - the hash is the identity), a tombstone (skipped), and a second
	// distinct content.
	versions := []remote.Version{
		ver(4, "original", true, gafferLedger(remote.OpDeploy)),
		tombstone(3, "original"),
		ver(2, "changed", true, nil),
		ver(1, "original", true, gafferLedger(remote.OpDeploy)),
	}
	originalHash := versions[0].Definition.Descriptor().Hash()

	t.Run("same content twice is one match", func(t *testing.T) {
		matches := map[string]*remote.Definition{}
		matchHashes(versions, originalHash[:7], matches)
		if len(matches) != 1 {
			t.Fatalf("got %d matches, want 1 (same content is one identity)", len(matches))
		}
		tgt, err := resolveHashMatches(matches, originalHash[:7], "orders")
		if err != nil || tgt.hash != originalHash || tgt.def.Query != "original" {
			t.Errorf("resolved (%+v, %v), want the original content", tgt, err)
		}
	})

	t.Run("no match", func(t *testing.T) {
		matches := map[string]*remote.Definition{}
		matchHashes(versions, "ffffffff", matches)
		if _, err := resolveHashMatches(matches, "ffffffff", "orders"); err == nil || !strings.Contains(err.Error(), "no version matching") {
			t.Errorf("err = %v, want no-version-matching", err)
		}
	})

	t.Run("prefix over two contents is ambiguous", func(t *testing.T) {
		// An empty prefix stands in for a real collision: it matches everything.
		matches := map[string]*remote.Definition{}
		matchHashes(versions, "", matches)
		if len(matches) != 2 {
			t.Fatalf("got %d matches, want the 2 distinct contents", len(matches))
		}
		if _, err := resolveHashMatches(matches, "", "orders"); err == nil || !strings.Contains(err.Error(), "matches 2 different versions") {
			t.Errorf("err = %v, want the ambiguity error", err)
		}
	})
}

func TestRollbackRefusal(t *testing.T) {
	for _, tc := range []struct {
		name string
		cmp  deploy.Comparison
		want string // "" = applyable
	}{
		{"in-place applyable", deploy.Comparison{QueryDiffers: true, EmitDiffers: true}, ""},
		{"engine version", deploy.Comparison{EngineVersionDiffers: true}, "engine version"},
		{"tracking", deploy.Comparison{TrackEmittedStreamsDiffers: true}, "emitted-stream tracking"},
		{"both names engine version", deploy.Comparison{EngineVersionDiffers: true, TrackEmittedStreamsDiffers: true}, "engine version"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := rollbackRefusal(tc.cmp, strings.Repeat("ab", 32), "orders")
			if tc.want == "" {
				if err != nil {
					t.Fatalf("want applyable, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "gaffer recreate orders") {
				t.Errorf("err = %v, want it to name %q and point at recreate", err, tc.want)
			}
		})
	}
}

func TestRenderRollback(t *testing.T) {
	full := strings.Repeat("ab", 32)
	for _, tc := range []struct {
		jsonOut               bool
		outcome, hash, target string
		want                  string
	}{
		{true, "rolled-back", full, "staging", `{"name":"orders","outcome":"rolled-back","hash":"` + full + `"}` + "\n"},
		{false, "rolled-back", full, "staging", "Rolled back orders to abababa on staging.\n"},
		{false, "rolled-back", full, "", "Rolled back orders to abababa.\n"},
		{false, "unchanged", full, "staging", "orders is already at abababa.\n"},
	} {
		var b bytes.Buffer
		if err := renderRollback(&b, tc.jsonOut, "orders", tc.outcome, tc.hash, tc.target); err != nil {
			t.Fatalf("renderRollback: %v", err)
		}
		if b.String() != tc.want {
			t.Errorf("renderRollback(json=%v, %q) = %q, want %q", tc.jsonOut, tc.outcome, b.String(), tc.want)
		}
	}
}

func TestWriteRollbackPreview(t *testing.T) {
	current := ver(2, "fromAll().when({});\n", true, nil).Definition.Descriptor()
	target := ver(1, "fromStream('a').when({});\n", true, nil).Definition.Descriptor()
	target.Emit = true
	cmp := deploy.Compare(target, current)

	var b bytes.Buffer
	writeRollbackPreview(&b, "orders", current, target, target.Hash(), cmp)
	out := b.String()
	for _, want := range []string{
		shortHash(current.Hash()), shortHash(target.Hash()), // the hash movement
		"emit disabled → enabled",   // the scalar flip
		"fromAll", "fromStream",     // both sides of the query diff
		"state built by the newer",  // state caution
		"local files are untouched", // drift caution
	} {
		if !strings.Contains(out, want) {
			t.Errorf("preview missing %q\n%s", want, out)
		}
	}
}