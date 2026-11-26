// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package newcmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/scaffold"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// SolutionOptions holds options for the new solution command.
type SolutionOptions struct {
	IOStreams      *terminal.IOStreams
	CliParams      *settings.Run
	Name           string
	PositionalName string
	Description    string
	Version        string
	Features       string
	Providers      string
	OutputFile     string
}

// CommandSolution creates the 'new solution' command.
func CommandSolution(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &SolutionOptions{}

	binaryName := settings.CliBinaryName
	if cliParams != nil && cliParams.BinaryName != "" {
		binaryName = cliParams.BinaryName
	}

	cCmd := &cobra.Command{
		Use:     "solution [name]",
		Aliases: []string{"sol", "s", "solutions"},
		Short:   "Create a new solution scaffold",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Generate a new solution YAML scaffold with best practices.

			Creates a well-structured solution file with the specified features
			and provider examples. Output defaults to stdout so you can pipe
			it to a file or review before saving.

			Available features:
			  parameters   - Input parameters with validation
			  resolvers    - Data resolution from providers
			  actions      - Workflow actions
			  transforms   - Data transformation
			  validation   - Input validation rules
			  tests        - Test suite scaffold
			  composition  - Solution composition

			Examples:
			  # Create a basic solution using a positional name
			  scafctl new solution my-deploy -d "Deploy to Kubernetes"

			  # Create a basic solution using --name
			  scafctl new solution -n my-deploy -d "Deploy to Kubernetes"

			  # Include specific features
			  scafctl new solution my-deploy -d "Deploy to K8s" \
			    --features parameters,resolvers,actions

			  # Include provider-specific examples
			  scafctl new solution my-deploy -d "Deploy" \
			    --providers exec,http

			  # Write to a file
			  scafctl new solution my-deploy -d "Deploy" --output-file my-deploy.yaml

			  # Pipe to file
			  scafctl new solution my-deploy -d "Deploy" > my-deploy.yaml
		`), settings.CliBinaryName, binaryName),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cmd.Name())
			ctx := settings.IntoContext(cmd.Context(), cliParams)

			if lgr := logger.FromContext(cmd.Context()); lgr != nil {
				ctx = logger.WithLogger(ctx, lgr)
			}

			w := writer.FromContext(cmd.Context())
			if w == nil {
				w = writer.New(ioStreams, cliParams)
			}
			ctx = writer.WithWriter(ctx, w)

			opts.IOStreams = ioStreams
			opts.CliParams = cliParams
			opts.PositionalName = ""
			if len(args) > 0 {
				opts.PositionalName = args[0]
			}

			return opts.Run(ctx)
		},
		SilenceUsage: true,
	}

	cCmd.Flags().StringVarP(&opts.Name, "name", "n", "", "Solution name (lowercase, hyphens, 3-60 chars)")
	cCmd.Flags().StringVarP(&opts.Description, "description", "d", "", "Brief description of the solution (required)")
	cCmd.Flags().StringVar(&opts.Version, "version", "", "Semantic version (optional, for catalog publishing)")
	cCmd.Flags().StringVar(&opts.Features, "features", "", "Comma-separated features: parameters,resolvers,actions,transforms,validation,tests,composition")
	cCmd.Flags().StringVar(&opts.Providers, "providers", "", "Comma-separated provider examples to include")
	cCmd.Flags().StringVar(&opts.OutputFile, "output-file", "", "Output file path (default: stdout)")

	_ = cCmd.MarkFlagRequired("description")

	return cCmd
}

// Run executes the new solution command.
func (o *SolutionOptions) Run(ctx context.Context) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return fmt.Errorf("writer not initialized in context")
	}

	name := strings.TrimSpace(o.Name)
	posName := strings.TrimSpace(o.PositionalName)
	switch {
	case name == "" && posName == "":
		err := fmt.Errorf("solution name is required: pass <name> positionally or via --name (-n)")
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	case name != "" && posName != "" && name != posName:
		err := fmt.Errorf("conflicting solution names: positional argument %q does not match --name %q", posName, name)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	case name == "":
		name = posName
	}

	// Build features map
	features := make(map[string]bool)
	if o.Features != "" {
		for _, f := range strings.Split(o.Features, ",") {
			f = strings.TrimSpace(f)
			if f != "" {
				// Validate feature name
				valid := false
				for _, vf := range scaffold.ValidFeatures {
					if f == vf {
						valid = true
						break
					}
				}
				if !valid {
					err := fmt.Errorf("unknown feature %q; valid features: %s", f, strings.Join(scaffold.ValidFeatures, ", "))
					w.Errorf("%v", err)
					return exitcode.WithCode(err, exitcode.InvalidInput)
				}
				features[f] = true
			}
		}
	}

	// Parse providers
	var providers []string
	if o.Providers != "" {
		for _, p := range strings.Split(o.Providers, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				providers = append(providers, p)
			}
		}
	}

	// Build scaffold
	result := scaffold.Solution(scaffold.Options{
		Name:        name,
		Description: o.Description,
		Version:     o.Version,
		Features:    features,
		Providers:   providers,
	})

	// Write output
	if o.OutputFile != "" {
		if err := os.WriteFile(o.OutputFile, []byte(result.YAML), 0o600); err != nil {
			w.Errorf("failed to write file: %v", err)
			return exitcode.WithCode(err, exitcode.GeneralError)
		}
		w.Successf("Solution written to %s\n", o.OutputFile)
		if len(result.NextSteps) > 0 {
			w.Plain("")
			w.Infof("Next steps:")
			for _, step := range result.NextSteps {
				w.Plainf("  • %s\n", step)
			}
		}
		return nil
	}

	// Write to stdout
	w.Plain(result.YAML)

	return nil
}
