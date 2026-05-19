package updatecheck

import "testing"

func TestIsNewer(t *testing.T) {
	cases := []struct {
		name            string
		latest, current string
		want            bool
	}{
		{"strict greater bare", "0.2.0", "0.1.3", true},
		{"strict greater v-prefixed", "v0.2.0", "v0.1.3", true},
		{"mixed prefix forms", "0.2.0", "v0.1.3", true},
		{"equal bare", "0.1.3", "0.1.3", false},
		{"equal v-prefixed", "v0.1.3", "v0.1.3", false},
		{"older latest", "0.1.0", "0.1.3", false},
		{"patch bump", "0.1.4", "0.1.3", true},
		{"minor bump", "0.2.0", "0.1.99", true},
		{"major bump", "1.0.0", "0.99.99", true},
		{"pre-release lower than release", "0.2.0-rc.1", "0.2.0", false},
		{"pre-release on both", "0.2.0-rc.2", "0.2.0-rc.1", true},
		{"empty latest", "", "0.1.3", false},
		{"empty current", "0.2.0", "", false},
		{"both empty", "", "", false},
		{"malformed latest", "not-a-version", "0.1.3", false},
		{"malformed current", "0.2.0", "not-a-version", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsNewer(tc.latest, tc.current); got != tc.want {
				t.Errorf("IsNewer(%q, %q) = %v, want %v", tc.latest, tc.current, got, tc.want)
			}
		})
	}
}
