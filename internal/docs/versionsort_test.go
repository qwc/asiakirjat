package docs

import (
	"reflect"
	"testing"
)

func TestSortVersionTags(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "basic semver",
			input:    []string{"v1.0.0", "v2.0.0", "v1.5.0"},
			expected: []string{"v2.0.0", "v1.5.0", "v1.0.0"},
		},
		{
			name:     "patch versions",
			input:    []string{"v1.0.1", "v1.0.0", "v1.0.10", "v1.0.2"},
			expected: []string{"v1.0.10", "v1.0.2", "v1.0.1", "v1.0.0"},
		},
		{
			name:     "without v prefix",
			input:    []string{"1.0.0", "2.0.0", "1.5.0"},
			expected: []string{"2.0.0", "1.5.0", "1.0.0"},
		},
		{
			name:     "prerelease sorted after release",
			input:    []string{"v1.0.0-beta", "v1.0.0", "v1.0.0-alpha"},
			expected: []string{"v1.0.0", "v1.0.0-beta", "v1.0.0-alpha"},
		},
		{
			name:     "major.minor only",
			input:    []string{"v1.0", "v2.0", "v1.10"},
			expected: []string{"v2.0", "v1.10", "v1.0"},
		},
		{
			name:     "single element",
			input:    []string{"v1.0.0"},
			expected: []string{"v1.0.0"},
		},
		{
			name:     "empty slice",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "mixed formats",
			input:    []string{"v1.0.0", "latest", "v2.0.0", "nightly"},
			expected: []string{"v2.0.0", "v1.0.0", "nightly", "latest"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := make([]string, len(tt.input))
			copy(input, tt.input)
			SortVersionTags(input)
			if len(input) > 0 && !reflect.DeepEqual(input, tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, input)
			}
		})
	}
}
