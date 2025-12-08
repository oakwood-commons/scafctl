package main

import (
	"fmt"
	"os"

	"github.com/kcloutie/scafctl/pkg/cmd/scafctl"
	"github.com/kcloutie/scafctl/pkg/profiler"
	"github.com/kcloutie/scafctl/pkg/settings"
)

var (
	Commit       string
	BuildVersion string
	BuildTime    string
)

func main() {
	verInfo := settings.VersionInfo{
		Commit:       Commit,
		BuildVersion: BuildVersion,
		BuildTime:    BuildTime,
	}
	settings.VersionInformation = verInfo

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cli := scafctl.Root()
	defer func() {
		// Profiler shutdown errors are logged but not treated as fatal,
		// as they do not affect the main application flow.
		if err := profiler.StopProfiler(); err != nil {
			fmt.Fprintf(os.Stderr, "profiler stop error: %v\n", err)
		}
	}()

	if cli == nil {
		return fmt.Errorf("failed to initialize CLI: scafctl.Root() returned nil")
	}
	return cli.Execute()
}
