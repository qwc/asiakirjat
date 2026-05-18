package main

import (
	"strings"
	"testing"
)

func TestIsInsecureInitialAdminPassword(t *testing.T) {
	cases := []struct {
		pw         string
		insecure   bool
	}{
		// Banned defaults — regardless of length.
		{"changeme", true},
		{"admin", true},
		{"password", true},

		// Too short.
		{"", true},
		{"short", true},
		{strings.Repeat("a", minInitialAdminPasswordLen-1), true},

		// Long enough and not a banned default.
		{strings.Repeat("a", minInitialAdminPasswordLen), false},
		{"correct-horse-battery-staple", false},
		{"a-Reasonable-Pass-123", false},
	}

	for _, tc := range cases {
		t.Run(tc.pw, func(t *testing.T) {
			if got := isInsecureInitialAdminPassword(tc.pw); got != tc.insecure {
				t.Errorf("isInsecureInitialAdminPassword(%q) = %v, want %v", tc.pw, got, tc.insecure)
			}
		})
	}
}
