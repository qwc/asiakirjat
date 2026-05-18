package validation

import (
	"strings"
	"testing"
)

func TestIsValidSlug(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"a", true},
		{"my-project", true},
		{"my-api-docs-v2", true},
		{"abc123", true},
		{"123", true},

		{"", false},
		{"My-Project", false},
		{"my_project", false},
		{"my project", false},
		{"-leading", false},
		{"trailing-", false},
		{"double--hyphen", false},
		{"a/b", false},
		{"a/../b", false},
		{".", false},
		{"..", false},
		{strings.Repeat("a", 129), false},
	}
	for _, tc := range cases {
		if got := IsValidSlug(tc.in); got != tc.want {
			t.Errorf("IsValidSlug(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestIsValidVersionTag(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"v1.0.0", true},
		{"1.0", true},
		{"latest", true},
		{"v1", true},
		{"v1.0-rc1", true},
		{"build_42", true},
		{"V1.0", true},

		{"", false},
		{".hidden", false},
		{"-leading", false},
		{".", false},
		{"..", false},
		{"../escape", false},
		{"v1/v2", false},
		{"v1\\v2", false},
		{"v1\nv2", false},
		{"v1\rv2", false},
		{"v1\"injected", false},
		{"v1 space", false},
		{strings.Repeat("v", 65), false},
	}
	for _, tc := range cases {
		if got := IsValidVersionTag(tc.in); got != tc.want {
			t.Errorf("IsValidVersionTag(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
