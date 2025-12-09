package settings

import "fmt"

const (
	CliBinaryName = "scafctl"
)

var (
	RootSolutionFolders = []string{
		CliBinaryName,
		fmt.Sprintf(".%s", CliBinaryName),
		"",
	}
	SolutionFileNames = []string{
		"solution.yaml",
		"solution.yml",
		fmt.Sprintf("%s.yaml", CliBinaryName),
		fmt.Sprintf("%s.yml", CliBinaryName),
		"solution.json",
		fmt.Sprintf("%s.json", CliBinaryName),
	}
)

var VersionInformation = VersionInfo{
	Commit:       "unknown",
	BuildVersion: "v0.0.0-nightly",
	BuildTime:    "unknown",
}

// EntryPointSettings holds configuration options for determining the entry point source.
// It specifies whether the entry point is provided via an API or CLI, and the path to the entry point.
type EntryPointSettings struct {
	FromAPI bool
	FromCli bool
	Path    string
}

// VersionInfo holds metadata about the build, including the commit hash,
// build version, and build timestamp.
type VersionInfo struct {
	Commit       string
	BuildVersion string
	BuildTime    string
}

// Run holds configuration settings for a single execution of the application.
// It includes options for logging, entry point configuration, output formatting,
// and error handling behavior.
type Run struct {
	MinLogLevel        int8
	EntryPointSettings EntryPointSettings
	IsQuiet            bool
	NoColor            bool
	ExitOnError        bool
}

// NewCliParams initializes and returns a pointer to a Run struct with default CLI parameters.
// It sets logging level to 0, configures entry point settings for CLI usage, and sets
// default flags for quiet mode, color output, and error handling.
func NewCliParams() *Run {
	return &Run{
		MinLogLevel: 0,
		EntryPointSettings: EntryPointSettings{
			FromAPI: false,
			FromCli: true,
		},
		IsQuiet:     false,
		NoColor:     false,
		ExitOnError: true,
	}
}
