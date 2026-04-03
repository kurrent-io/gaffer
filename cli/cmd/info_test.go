package cmd

import (
	"testing"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
)

func TestInfoSource(t *testing.T) {
	tests := []struct {
		name     string
		info     gafferruntime.QuerySources
		expected string
	}{
		{"all streams", gafferruntime.QuerySources{AllStreams: true}, "all"},
		{"category", gafferruntime.QuerySources{Categories: []string{"order"}}, "category"},
		{"streams", gafferruntime.QuerySources{Streams: []string{"order-1"}}, "streams"},
		{"unknown", gafferruntime.QuerySources{}, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := infoSource(tt.info)
			if got != tt.expected {
				t.Errorf("infoSource() = %q, want %q", got, tt.expected)
			}
		})
	}
}
