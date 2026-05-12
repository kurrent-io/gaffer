package telemetry

import (
	"encoding/json"
	"math"
	"testing"
)

// These boundary tables pin the wire-format bucket lookups for
// RawCount and RawDuration. The bucket-label literals (the right-hand
// side of each row) become baked into dashboards and historical
// data once the schema ships, so silent drift in the marshal switch
// would invalidate cross-version comparisons. The tests touch every
// boundary edge plus a max-int extreme so a transposed case in the
// switch fails loudly.

func TestRawCount_MarshalJSONBoundaries(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{math.MinInt32, "0"},
		{-1, "0"},
		{0, "0"},
		{1, "1"},
		{2, "2"},
		{9, "2"},
		{10, "10"},
		{99, "10"},
		{100, "100"},
		{999, "100"},
		{1000, "1000"},
		{math.MaxInt32, "1000"},
	}
	for _, tc := range cases {
		got, err := json.Marshal(RawCount(tc.in))
		if err != nil {
			t.Fatalf("Marshal(%d): %v", tc.in, err)
		}
		if string(got) != tc.want {
			t.Errorf("RawCount(%d) -> %s, want %s", tc.in, got, tc.want)
		}
	}
}

func TestRawDuration_MarshalJSONBoundaries(t *testing.T) {
	cases := []struct {
		ms   int
		want string
	}{
		{math.MinInt32, "0"},
		{-1, "0"},
		{0, "0"},
		{9, "0"},
		{10, "10"},
		{99, "10"},
		{100, "100"},
		{999, "100"},
		{1000, "1000"},
		{9999, "1000"},
		{10000, "10000"},
		{59999, "10000"},
		{60000, "60000"},
		{599999, "60000"},
		{600000, "600000"},
		{math.MaxInt32, "600000"},
	}
	for _, tc := range cases {
		got, err := json.Marshal(RawDuration(tc.ms))
		if err != nil {
			t.Fatalf("Marshal(%d): %v", tc.ms, err)
		}
		if string(got) != tc.want {
			t.Errorf("RawDuration(%d) -> %s, want %s", tc.ms, got, tc.want)
		}
	}
}
