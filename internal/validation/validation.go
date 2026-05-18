// Package validation centralizes input validators for values that flow into
// filesystem paths, response headers, or other security-sensitive sinks.
//
// All validators are pure functions and safe to call from any goroutine.
package validation

import "regexp"

const (
	maxSlugLen    = 128
	maxVersionLen = 64
)

var (
	slugPattern       = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	versionTagPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

// IsValidSlug reports whether s is a valid project slug:
// lowercase alphanumeric segments separated by single hyphens, 1-128 chars.
func IsValidSlug(s string) bool {
	if len(s) < 1 || len(s) > maxSlugLen {
		return false
	}
	return slugPattern.MatchString(s)
}

// IsValidVersionTag reports whether t is a safe version tag for filesystem
// and header use: starts with an alphanumeric, then alphanumeric / dot /
// underscore / hyphen, 1-64 chars. Rejects ".", "..", leading dot, and any
// path separator.
func IsValidVersionTag(t string) bool {
	if len(t) < 1 || len(t) > maxVersionLen {
		return false
	}
	return versionTagPattern.MatchString(t)
}
