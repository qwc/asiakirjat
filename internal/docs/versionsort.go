package docs

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// semver regex: optional 'v' prefix, major.minor.patch, optional prerelease
var semverRe = regexp.MustCompile(`^v?(\d+)(?:\.(\d+))?(?:\.(\d+))?(.*)$`)

type semverParts struct {
	Major      int
	Minor      int
	Patch      int
	Prerelease string
	Original   string
}

func parseSemver(tag string) semverParts {
	m := semverRe.FindStringSubmatch(tag)
	if m == nil {
		return semverParts{Original: tag}
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	patch, _ := strconv.Atoi(m[3])
	return semverParts{
		Major:      major,
		Minor:      minor,
		Patch:      patch,
		Prerelease: strings.TrimPrefix(m[4], "-"),
		Original:   tag,
	}
}

// IsSemver returns true if the given tag matches the semver pattern.
func IsSemver(tag string) bool {
	return semverRe.MatchString(tag)
}

// SortVersionTags sorts version tags in descending semver order.
// Tags that match semver come first; non-semver tags are sorted lexicographically at the end.
func SortVersionTags(tags []string) {
	sort.Slice(tags, func(i, j int) bool {
		aIsSemver := semverRe.MatchString(tags[i])
		bIsSemver := semverRe.MatchString(tags[j])

		// Semver tags come before non-semver tags
		if aIsSemver && !bIsSemver {
			return true
		}
		if !aIsSemver && bIsSemver {
			return false
		}
		// Both non-semver: sort lexicographically descending
		if !aIsSemver && !bIsSemver {
			return tags[i] > tags[j]
		}

		a := parseSemver(tags[i])
		b := parseSemver(tags[j])

		if a.Major != b.Major {
			return a.Major > b.Major
		}
		if a.Minor != b.Minor {
			return a.Minor > b.Minor
		}
		if a.Patch != b.Patch {
			return a.Patch > b.Patch
		}
		// No prerelease > has prerelease (release is "greater")
		if a.Prerelease == "" && b.Prerelease != "" {
			return true
		}
		if a.Prerelease != "" && b.Prerelease == "" {
			return false
		}
		return a.Prerelease > b.Prerelease
	})
}
