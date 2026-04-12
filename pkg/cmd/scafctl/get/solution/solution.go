// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solution

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/cache"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/get"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/output"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

var ValidOutputTypes = []string{"json", "yaml", "table"}

type GetLatestVersionFunc func(ctx context.Context) (string, error)

type CmdOptionsVersion struct {
	IOStreams *terminal.IOStreams
	CliParams *settings.Run
	Output    string
	File      string
	NoCache   bool
}

func CommandSolution(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	options := &CmdOptionsVersion{}
	cCmd := &cobra.Command{
		Use:     "solution [name[@version]]",
		Aliases: []string{"sol", "SOL", "Solution", "solutions"},
		Short:   fmt.Sprintf("Gets %s solutions", strings.SplitN(path, "/", 2)[0]),
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cCmd *cobra.Command, args []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cCmd.Name())
			ctx := settings.IntoContext(cCmd.Context(), cliParams)

			lgr := logger.FromContext(ctx)
			lgr.V(3).Info("Solution command invoked")

			options.IOStreams = ioStreams
			options.CliParams = cliParams

			// Handle positional catalog name argument
			if len(args) > 0 {
				if err := get.ValidatePositionalRef(args[0], options.File, cliParams.BinaryName+" get solution"); err != nil {
					output.NewWriteMessageOptions(
						options.IOStreams,
						output.MessageTypeError,
						options.CliParams.NoColor,
						options.CliParams.ExitOnError,
					).WriteMessage(err.Error())
					return exitcode.WithCode(err, exitcode.InvalidInput)
				}
				options.File = args[0]
			}

			err := output.ValidateOutputType(options.Output, ValidOutputTypes)
			if err != nil {
				output.NewWriteMessageOptions(
					options.IOStreams,
					output.MessageTypeError,
					options.CliParams.NoColor,
					options.CliParams.ExitOnError,
				).WriteMessage(err.Error())

				return exitcode.WithCode(err, exitcode.InvalidInput)
			}
			return options.GetSolution(ctx)
		},
		SilenceUsage: true,
	}
	cCmd.PersistentFlags().StringVarP(&options.Output, "output", "o", "", fmt.Sprintf("Output format. One of: (%s)", strings.Join(ValidOutputTypes, ", ")))
	cCmd.PersistentFlags().StringVarP(&options.File, "file", "f", "", "Path to the solution. This can be a local file path or a URL. If not provided, the command will attempt to locate a solution file in default locations.")
	cCmd.PersistentFlags().BoolVar(&options.NoCache, "no-cache", false, "Bypass the artifact cache and fetch directly from the catalog")
	return cCmd
}

func (o *CmdOptionsVersion) GetSolution(ctx context.Context) error {
	lgr := logger.FromContext(ctx)

	// Set up getter with catalog resolver for bare name resolution
	var getterOpts []get.Option
	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err == nil {
		resolverOpts := []catalog.SolutionResolverOption{
			catalog.WithResolverNoCache(o.NoCache),
		}
		if !o.NoCache {
			artifactCache := cache.NewArtifactCache(paths.ArtifactCacheDir(), settings.DefaultArtifactCacheTTL)
			resolverOpts = append(resolverOpts, catalog.WithResolverArtifactCache(artifactCache))
		}
		resolver := catalog.NewSolutionResolver(localCatalog, *lgr, resolverOpts...)
		getterOpts = append(getterOpts, get.WithCatalogResolver(resolver))
	} else {
		lgr.V(1).Info("catalog not available for solution resolution", "error", err)
	}

	getter := get.NewGetter(getterOpts...)
	return o.GetSolutionWithGetter(ctx, getter)
}

// GetSolutionWithGetter retrieves a solution using the provided getter implementation.
// This method allows for dependency injection, making it easier to test with mock implementations.
// The getter parameter should implement the get.Interface.
func (o *CmdOptionsVersion) GetSolutionWithGetter(ctx context.Context, getter get.Interface) error {
	w := writer.FromContext(ctx)

	sol, err := getter.Get(ctx, o.File)
	if err != nil {
		if w != nil {
			w.Errorf("%v", err)
		}
		return exitcode.WithCode(err, exitcode.FileNotFound)
	}

	// For json/yaml, use the direct output writer. For table or default,
	// use kvx which provides table rendering.
	switch o.Output {
	case "json", "yaml":
		err = output.WriteOutput(o.IOStreams, o.Output, sol, nil)
	default:
		// Default / table: use kvx for structured table output
		format := o.Output
		if format == "" {
			format = "auto"
		}
		kvxOpts := flags.NewKvxOutputOptionsFromFlags(
			format,
			false,
			"",
			kvx.WithOutputContext(ctx),
			kvx.WithOutputNoColor(o.CliParams.NoColor),
			kvx.WithOutputAppName(o.CliParams.BinaryName+" get solution"),
		)
		kvxOpts.IOStreams = o.IOStreams
		err = kvxOpts.Write(newSolutionSummary(sol))
	}

	if err != nil {
		if w != nil {
			w.Errorf("%v", err)
		}
		return exitcode.WithCode(err, exitcode.GeneralError)
	}
	return nil
}

// solutionSummary is a display-friendly representation of a solution.
type solutionSummary struct {
	Name        string `json:"name" yaml:"name"`
	Version     string `json:"version" yaml:"version"`
	DisplayName string `json:"displayName,omitempty" yaml:"displayName,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Resolvers   int    `json:"resolvers" yaml:"resolvers"`
	Actions     int    `json:"actions" yaml:"actions"`
}

func newSolutionSummary(sol *solution.Solution) *solutionSummary {
	version := ""
	if sol.Metadata.Version != nil {
		version = sol.Metadata.Version.String()
	}
	resolverCount := 0
	if sol.Spec.Resolvers != nil {
		resolverCount = len(sol.Spec.Resolvers)
	}
	actionCount := 0
	if sol.Spec.Workflow != nil && sol.Spec.Workflow.Actions != nil {
		actionCount = len(sol.Spec.Workflow.Actions)
	}
	return &solutionSummary{
		Name:        sol.Metadata.Name,
		Version:     version,
		DisplayName: sol.Metadata.DisplayName,
		Description: sol.Metadata.Description,
		Resolvers:   resolverCount,
		Actions:     actionCount,
	}
}
