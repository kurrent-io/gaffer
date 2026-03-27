package cmd

import (
	"testing"
)

func TestInfoSource(t *testing.T) {
	tests := []struct {
		name     string
		info     projectionInfo
		expected string
	}{
		{"all streams", projectionInfo{AllStreams: true}, "all"},
		{"category", projectionInfo{Categories: []string{"order"}}, "category"},
		{"streams", projectionInfo{Streams: []string{"order-1"}}, "streams"},
		{"unknown", projectionInfo{}, "unknown"},
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
