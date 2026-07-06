package remote

import (
	"strings"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/deploy"
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
		got, err := NormalizeHashPrefix(tc.in)
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("NormalizeHashPrefix(%q) err = %v, want %q", tc.in, err, tc.wantErr)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("NormalizeHashPrefix(%q) = (%q, %v), want %q", tc.in, got, err, tc.want)
		}
	}
}

func TestMatchAndResolveHashes(t *testing.T) {
	// Four raw versions: two occurrences of the same content (one match, not an
	// ambiguity - the hash is the identity), a tombstone (skipped), and a second
	// distinct content.
	versions := []Version{
		classifyVer(4, "original", true, classifyGafferLedger(OpDeploy)),
		rollbackTombstone(3, "original"),
		classifyVer(2, "changed", true, nil),
		classifyVer(1, "original", true, classifyGafferLedger(OpDeploy)),
	}
	originalHash := versions[0].Definition.Descriptor().Hash()

	t.Run("same content twice is one match", func(t *testing.T) {
		matches := map[string]*Definition{}
		matchHashes(versions, originalHash[:7], matches)
		if len(matches) != 1 {
			t.Fatalf("got %d matches, want 1 (same content is one identity)", len(matches))
		}
		tgt, err := resolveHashMatches(matches, originalHash[:7], "orders")
		if err != nil || tgt.Hash != originalHash || tgt.Def.Query != "original" {
			t.Errorf("resolved (%+v, %v), want the original content", tgt, err)
		}
	})

	t.Run("no match", func(t *testing.T) {
		matches := map[string]*Definition{}
		matchHashes(versions, "ffffffff", matches)
		if _, err := resolveHashMatches(matches, "ffffffff", "orders"); err == nil || !strings.Contains(err.Error(), "no version matching") {
			t.Errorf("err = %v, want no-version-matching", err)
		}
	})

	t.Run("prefix over two contents is ambiguous", func(t *testing.T) {
		// An empty prefix stands in for a real collision: it matches everything.
		matches := map[string]*Definition{}
		matchHashes(versions, "", matches)
		if len(matches) != 2 {
			t.Fatalf("got %d matches, want the 2 distinct contents", len(matches))
		}
		if _, err := resolveHashMatches(matches, "", "orders"); err == nil || !strings.Contains(err.Error(), "matches 2 different versions") {
			t.Errorf("err = %v, want the ambiguity error", err)
		}
	})
}

func TestScanSettled(t *testing.T) {
	full := strings.Repeat("ab", 32)
	one := map[string]*Definition{full: {}}
	two := map[string]*Definition{full: {}, strings.Repeat("cd", 32): {}}
	for _, tc := range []struct {
		name    string
		prefix  string
		matches map[string]*Definition
		want    bool
	}{
		{"full hash with a match is exact", full, one, true},
		{"full hash with no match keeps scanning", full, map[string]*Definition{}, false},
		{"prefix with one match keeps scanning for ambiguity", "abab", one, false},
		{"two distinct matches are already ambiguous", "abab", two, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := scanSettled(tc.prefix, tc.matches); got != tc.want {
				t.Errorf("scanSettled = %v, want %v", got, tc.want)
			}
		})
	}
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
			err := RollbackRefusal(tc.cmp, strings.Repeat("ab", 32), "orders")
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

// rollbackTombstone is a deleted version still carrying the definition it
// removed, the shape the server writes for a delete.
func rollbackTombstone(number int64, query string) Version {
	return Version{Number: number, Deleted: true, Definition: &Definition{Query: query, EngineVersion: 1, Time: classifyTime}}
}
