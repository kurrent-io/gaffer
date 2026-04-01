package cmd

import (
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/projection"
)

func TestInfoSource(t *testing.T) {
	tests := []struct {
		name     string
		info     projection.Info
		expected string
	}{
		{"all streams", projection.Info{AllStreams: true}, "all"},
		{"category", projection.Info{Categories: []string{"order"}}, "category"},
		{"streams", projection.Info{Streams: []string{"order-1"}}, "streams"},
		{"unknown", projection.Info{}, "unknown"},
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
