package version

import "runtime/debug"

var defaultVersion = "0.0.0-dev"

// Version returns the embeded build version. When the binary is built with
// -ldflags "-X example.com/scafctl-sample/internal/version.buildVersion=..."
// this variable is overridden.
var buildVersion = defaultVersion

// BuildInfo gathers information from runtime/debug to enrich version output.
type BuildInfo struct {
	Version    string
	GitCommit  string
	DirtyTree  bool
}

// Version returns the current build version string.
func Version() string {
	if buildVersion != "" {
		return buildVersion
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		return info.Main.Version
	}
	return defaultVersion
}
