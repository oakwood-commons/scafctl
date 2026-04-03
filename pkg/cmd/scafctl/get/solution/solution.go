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
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/get"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/output"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

var ValidOutputTypes = []string{"json", "yaml"}

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
		Short:   fmt.Sprintf("Gets %s solutions", settings.CliBinaryName),
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
				if err := get.ValidatePositionalRef(args[0], options.File, "scafctl get solution"); err != nil {
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

	err = output.WriteOutput(o.IOStreams, o.Output, sol, nil)
	if err != nil {
		if w != nil {
			w.Errorf("%v", err)
		}
		return exitcode.WithCode(err, exitcode.GeneralError)
	}
	return nil
}
