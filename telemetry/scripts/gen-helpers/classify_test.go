package main

import (
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
)

// compileExpr compiles a snippet under `package x` and returns the named
// definition's value. Lets us test classify / flattenStringDisjunction in
// isolation without loading the real telemetry schema.
func compileExpr(t *testing.T, src, defName string) cue.Value {
	t.Helper()
	ctx := cuecontext.New()
	v := ctx.CompileString("package x\n" + src)
	if err := v.Err(); err != nil {
		t.Fatalf("compile: %v", err)
	}
	d := v.LookupPath(cue.MakePath(cue.Def(defName)))
	if err := d.Err(); err != nil {
		t.Fatalf("lookup %s: %v", defName, err)
	}
	return d
}

func TestFlattenStringDisjunction(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		def  string
		want []string
	}{
		{
			name: "single literal",
			src:  `#Lit: "only"`,
			def:  "Lit",
			want: []string{"only"},
		},
		{
			name: "two literals",
			src:  `#Two: "a" | "b"`,
			def:  "Two",
			want: []string{"a", "b"},
		},
		{
			name: "five literals",
			src:  `#Five: "alpha" | "bravo" | "charlie" | "delta" | "echo"`,
			def:  "Five",
			want: []string{"alpha", "bravo", "charlie", "delta", "echo"},
		},
		{
			name: "transitively includes referenced disjunction",
			src: `
#A: "x" | "y"
#B: "z" | #A`,
			def:  "B",
			want: []string{"x", "y", "z"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := compileExpr(t, tc.src, tc.def)
			got, err := flattenStringDisjunction(v)
			if err != nil {
				t.Fatalf("flattenStringDisjunction: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("len = %d, want %d (got %v)", len(got), len(tc.want), got)
			}
			gotSet := map[string]bool{}
			for _, g := range got {
				gotSet[g] = true
			}
			for _, w := range tc.want {
				if !gotSet[w] {
					t.Errorf("missing %q in %v", w, got)
				}
			}
		})
	}
}

func TestClassify(t *testing.T) {
	// One CUE input with several fields exercising every classify branch.
	src := `
#BucketCount: 0 | 1 | 2 | 10 | 100 | 1000
#UUID: =~"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$"
#Timestamp: string
#Outcome: "ok" | "fail"

#Sample: {
    plain_string: string
    bool_field:   bool
    raw_count:    #BucketCount
    uuid_ref:     #UUID
    ts_ref:       #Timestamp
    enum_ref:     #Outcome
    strings_arr:  [...string]
    enum_arr:     [...#Outcome]
    file_size:    0 | 1024 | 5120
}`
	v := compileExpr(t, src, "Sample")
	fields, err := readStructFields(v, nil)
	if err != nil {
		t.Fatalf("readStructFields: %v", err)
	}
	got := map[string]fieldKind{}
	for _, f := range fields {
		got[f.Wire] = f.Kind
	}
	for _, tc := range []struct {
		wire string
		kind fieldKind
	}{
		{"plain_string", kindString},
		{"bool_field", kindBool},
		{"raw_count", kindRawCount},
		{"uuid_ref", kindUUID},
		{"ts_ref", kindTimestamp},
		{"enum_ref", kindEnum},
		{"strings_arr", kindArrayOfString},
		{"enum_arr", kindArrayOfEnum},
		{"file_size", kindInt},
	} {
		if got[tc.wire] != tc.kind {
			t.Errorf("%s: kind = %d, want %d", tc.wire, got[tc.wire], tc.kind)
		}
	}
}
