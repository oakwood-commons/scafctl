// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"fmt"
	"os"

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
	// Register all default factories (CEL env/cache, Go template extensions).
	scafctl.RegisterDefaults()

	cli := scafctl.Root(nil)
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
