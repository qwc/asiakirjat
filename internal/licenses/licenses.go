package licenses

//go:generate go run ../../cmd/gen-licenses

// Dependency holds license information for a vendored module.
type Dependency struct {
	Module      string
	Version     string
	LicenseType string // e.g. "MIT", "Apache-2.0", "BSD-3-Clause"
	LicenseText string
}
