package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/celexp/env"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/profiler"
	"github.com/oakwood-commons/scafctl/pkg/settings"
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
		code := exitcode.GetCode(err)
		// If GetCode returns GeneralError (1), it means no ExitError was wrapped.
		// This typically happens for Cobra errors (unknown command, missing flags, etc.)
		// that bypassed our command handlers. Print these to stderr.
		if code == exitcode.GeneralError {
			var exitErr *exitcode.ExitError
			if !errors.As(err, &exitErr) {
				// Not an ExitError, so it's an unhandled Cobra error - print it
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			}
		}
		os.Exit(code)
	}
}

func run() error {
	// Register env.New as the environment factory for celexp package.
	// This allows celexp to use environments with all extensions without circular dependency.
	celexp.SetEnvFactory(env.New)

	// Register env.GlobalCache as the cache factory for celexp package.
	// This allows celexp to automatically use the global cache when no cache is specified.
	celexp.SetCacheFactory(env.GlobalCache)

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
