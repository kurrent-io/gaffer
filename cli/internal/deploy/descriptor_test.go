package deploy

import "testing"

// TestHashGolden pins the exact hash of a fixed descriptor. It is intentionally
// brittle: any change to the payload layout (field order, labels, separators,
// canonicalisation) breaks it. That is the point - this hash is the on-the-wire
// idempotency/drift key, so an accidental format change would make already-deployed
// projections read as drifted. Regenerate deliberately if the format must change.
func TestHashGolden(t *testing.T) {
	d := Descriptor{Query: "fromAll()", EngineVersion: 2, Emit: true}
	const want = "9da1a8977948cf2044f03ebf23dc88ce9c8d650d1d989ba9ba7797e5fdd93b28"
	if got := d.Hash(); got != want {
		t.Fatalf("Hash() = %s, want %s (payload format changed - regenerate deliberately)", got, want)
	}
}

func TestHashIgnoresEditorNoise(t *testing.T) {
	base := Descriptor{Query: "fromAll()\n.when({})\n", EngineVersion: 2, Emit: true}
	variants := []struct {
		name  string
		query string
	}{
		{"CRLF", "fromAll()\r\n.when({})\r\n"},
		{"no trailing newline", "fromAll()\n.when({})"},
		{"extra trailing newlines", "fromAll()\n.when({})\n\n\n"},
		{"leading UTF-8 BOM", "\uFEFFfromAll()\n.when({})\n"},
	}
	want := base.Hash()
	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			d := base
			d.Query = v.query
			if got := d.Hash(); got != want {
				t.Errorf("%s changed the hash; editor-trivial noise should not", v.name)
			}
		})
	}
}

func TestCanonicalQueryKeepsRealDifferences(t *testing.T) {
	// A lone CR (old-Mac, or mid-line) and trailing spaces are NOT normalised, so
	// they count as drift - pin that boundary against someone widening the rule.
	for _, tc := range []struct{ a, b string }{
		{"a\rb", "a\nb"},             // lone CR vs LF
		{"fromAll()  ", "fromAll()"}, // trailing spaces vs none
	} {
		if (Descriptor{Query: tc.a}).Hash() == (Descriptor{Query: tc.b}).Hash() {
			t.Errorf("%q and %q should hash differently (only CRLF/BOM/trailing-newline are normalised)", tc.a, tc.b)
		}
	}
}

func TestHashDistinguishesEveryDimension(t *testing.T) {
	base := Descriptor{Query: "fromAll()", EngineVersion: 2, Emit: true, TrackEmittedStreams: false}
	for _, tc := range []struct {
		name string
		d    Descriptor
	}{
		{"query", Descriptor{Query: "fromCategory('x')", EngineVersion: 2, Emit: true}},
		{"engine version", Descriptor{Query: "fromAll()", EngineVersion: 1, Emit: true}},
		{"emit", Descriptor{Query: "fromAll()", EngineVersion: 2, Emit: false}},
		{"trackEmittedStreams", Descriptor{Query: "fromAll()", EngineVersion: 2, Emit: true, TrackEmittedStreams: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if base.Hash() == tc.d.Hash() {
				t.Errorf("hash should differ on %s", tc.name)
			}
		})
	}
}

func TestHashNoCrossDimensionCollision(t *testing.T) {
	// A query that mimics a field line must not collide with the field encoding;
	// the mandatory labelled prefix (always before the query) guards this. Probe
	// both labels.
	base := Descriptor{Query: "x"}
	for _, forged := range []string{
		"emit=false\nx",
		"engineVersion=0\nemit=false\ntrackEmittedStreams=false\nx",
	} {
		if base.Hash() == (Descriptor{Query: forged}).Hash() {
			t.Errorf("query %q forged the field encoding", forged)
		}
	}
}

func TestCompareDimensions(t *testing.T) {
	base := Descriptor{Query: "fromAll()", EngineVersion: 2, Emit: true}
	for _, tc := range []struct {
		name  string
		other Descriptor
		want  Comparison
	}{
		{"identical", base, Comparison{}},
		{"line-ending only", Descriptor{Query: "fromAll()\r\n", EngineVersion: 2, Emit: true}, Comparison{}},
		{"query only", Descriptor{Query: "fromCategory('x')", EngineVersion: 2, Emit: true}, Comparison{QueryDiffers: true}},
		{"engine version only", Descriptor{Query: "fromAll()", EngineVersion: 1, Emit: true}, Comparison{EngineVersionDiffers: true}},
		{"emit only", Descriptor{Query: "fromAll()", EngineVersion: 2, Emit: false}, Comparison{EmitDiffers: true}},
		{"tracking only", Descriptor{Query: "fromAll()", EngineVersion: 2, Emit: true, TrackEmittedStreams: true}, Comparison{TrackEmittedStreamsDiffers: true}},
		{"all differ", Descriptor{Query: "x", EngineVersion: 1, Emit: false, TrackEmittedStreams: true}, Comparison{QueryDiffers: true, EngineVersionDiffers: true, EmitDiffers: true, TrackEmittedStreamsDiffers: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Compare(base, tc.other)
			if got != tc.want {
				t.Fatalf("Compare = %+v, want %+v", got, tc.want)
			}
			// Hash equality must agree with InSync for every case.
			if (base.Hash() == tc.other.Hash()) != got.InSync() {
				t.Fatalf("Hash-equality (%v) disagrees with InSync (%v)", base.Hash() == tc.other.Hash(), got.InSync())
			}
		})
	}
}

func TestEmptyQuery(t *testing.T) {
	// Empty and whitespace-only-newline queries canonicalise identically; pin it.
	if (Descriptor{Query: ""}).Hash() != (Descriptor{Query: "\n\n"}).Hash() {
		t.Error("empty and newline-only queries should canonicalise the same")
	}
	if !Compare(Descriptor{}, Descriptor{}).InSync() {
		t.Error("two zero-value descriptors should be in sync")
	}
}
