package licenses

//go:generate go run ../../cmd/gen-licenses

// Dependency holds license information for a vendored module.
type Dependency struct {
	Module      string
	Version     string
	LicenseText string
}
