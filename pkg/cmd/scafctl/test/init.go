// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// InitOptions holds configuration for the test init command.
type InitOptions struct {
	IOStreams *terminal.IOStreams
	CliParams *settings.Run
	File      string
}

// CommandInit creates the 'test init' subcommand that generates a starter test suite.
func CommandInit(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &InitOptions{}

	cCmd := &cobra.Command{
		Use:   "init",
		Short: "Generate a starter test suite from a solution",
		Long: `Generate skeleton test cases by analyzing a solution's structure.

This command parses the solution YAML and generates starter test definitions
based on the resolvers, validation rules, and workflow actions it finds.
No commands are executed — this is structural analysis only.

The generated YAML is written to stdout and can be pasted into the solution's
spec.tests section or saved to a separate test file.

Examples:
  # Generate tests from a solution file
  scafctl test init -f solution.yaml

  # Save generated tests to a file
  scafctl test init -f solution.yaml > tests.yaml

  # Pipe into a compose file
  scafctl test init -f solution.yaml >> solution-tests.yaml`,
		SilenceUsage: true,
		RunE: func(cCmd *cobra.Command, _ []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cCmd.Use)
			ctx := settings.IntoContext(context.Background(), cliParams)

			opts.IOStreams = ioStreams
			opts.CliParams = cliParams

			return runInit(ctx, opts)
		},
	}

	cCmd.Flags().StringVarP(&opts.File, "file", "f", "", "Path to the solution file (required)")
	_ = cCmd.MarkFlagRequired("file")

	return cCmd
}

// runInit implements the test init command logic.
func runInit(ctx context.Context, opts *InitOptions) error {
	w := writer.FromContext(ctx)
	if w == nil {
		w = writer.New(opts.IOStreams, opts.CliParams)
	}

	// Read and parse the solution
	data, err := os.ReadFile(opts.File)
	if err != nil {
		w.Errorf("reading solution file: %s", err)
		return exitcode.WithCode(fmt.Errorf("reading solution file: %w", err), exitcode.FileNotFound)
	}

	var sol solution.Solution
	if err := sol.UnmarshalFromBytes(data); err != nil {
		w.Errorf("parsing solution: %s", err)
		return exitcode.WithCode(fmt.Errorf("parsing solution: %w", err), exitcode.InvalidInput)
	}

	// Build scaffold input from the parsed solution
	input := &soltesting.ScaffoldInput{
		Resolvers: sol.Spec.Resolvers,
		Workflow:  sol.Spec.Workflow,
	}

	// Generate scaffold
	result := soltesting.Scaffold(input)

	// Marshal to YAML and write to stdout
	out, err := soltesting.ScaffoldToYAML(result)
	if err != nil {
		w.Errorf("marshalling test YAML: %s", err)
		return fmt.Errorf("marshalling test YAML: %w", err)
	}

	// Write YAML header comment
	fmt.Fprintf(opts.IOStreams.Out, "# Generated test scaffold for %s\n", opts.File)
	fmt.Fprintf(opts.IOStreams.Out, "# Paste this into your solution's spec section or a compose test file.\n")
	fmt.Fprintf(opts.IOStreams.Out, "# Customize assertions and parameters to match your expected behavior.\n\n")
	fmt.Fprint(opts.IOStreams.Out, string(out))

	return nil
}
