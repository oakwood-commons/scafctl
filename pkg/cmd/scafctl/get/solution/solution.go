// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solution

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
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
	Local     bool
}

func CommandSolution(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	options := &CmdOptionsVersion{}
	cCmd := &cobra.Command{
		Use:     "solution [name[@version]]",
		Aliases: []string{"sol", "SOL", "Solution", "solutions"},
		Short:   fmt.Sprintf("List or get %s solutions", strings.SplitN(path, "/", 2)[0]),
		Long: strings.ReplaceAll(`List solutions from the catalog or get details about a specific solution.

Without arguments, lists solutions from the configured catalog (and local catalog).
With a name argument or --file flag, shows details for a specific solution.
Use --local to auto-discover and display a solution from the current directory.

Examples:
  # List all solutions in the catalog
  scafctl get solutions

  # Get a specific solution from the catalog
  scafctl get solution my-solution

  # Get a specific version
  scafctl get solution my-solution@1.0.0

  # Auto-discover and display a local solution
  scafctl get solution --local

  # Get a solution from a file
  scafctl get solution -f ./solution.yaml

  # Output as JSON
  scafctl get solution my-solution -o json
`, settings.CliBinaryName, cliParams.BinaryName),
		Args: cobra.MaximumNArgs(1),
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

			// No args, no --file, and not --local: list solutions from catalog
			if len(args) == 0 && options.File == "" && !options.Local {
				return options.ListSolutions(ctx)
			}

			return options.GetSolution(ctx)
		},
		SilenceUsage: true,
	}
	cCmd.PersistentFlags().StringVarP(&options.Output, "output", "o", "", fmt.Sprintf("Output format. One of: (%s)", strings.Join(ValidOutputTypes, ", ")))
	cCmd.PersistentFlags().StringVarP(&options.File, "file", "f", "", "Path to the solution. This can be a local file path or a URL. If not provided and no arguments given, lists catalog solutions.")
	cCmd.PersistentFlags().BoolVar(&options.NoCache, "no-cache", false, "Bypass the artifact cache and fetch directly from the catalog")
	cCmd.PersistentFlags().BoolVar(&options.Local, "local", false, "Auto-discover and display a solution from the current directory")
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
			catalog.WithResolverRemoteCatalogs(catalog.RemoteCatalogsFromContext(ctx, *lgr)),
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

	getter := get.NewGetterFromContext(ctx, getterOpts...)

	// Emit verbose discovery information
	if w := writer.FromContext(ctx); w != nil && w.VerboseEnabled() {
		switch o.File {
		case "":
			binaryName := settings.CliBinaryName
			if o.CliParams != nil && o.CliParams.BinaryName != "" {
				binaryName = o.CliParams.BinaryName
			}
			w.Verbosef("Auto-discovering solution (binary=%s)", binaryName)
			w.Verbosef("  Search folders: %v", settings.SolutionFoldersFor(binaryName))
			w.Verbosef("  Search filenames: %v", settings.SolutionFileNamesFor(binaryName))
		default:
			w.Verbosef("Loading solution from: %s", o.File)
		}
	}

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

// ListSolutions lists solutions from the local and configured catalogs.
func (o *CmdOptionsVersion) ListSolutions(ctx context.Context) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	var artifacts []catalog.ArtifactInfo

	// List from local catalog
	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err == nil {
		localArtifacts, listErr := localCatalog.List(ctx, catalog.ArtifactKindSolution, "")
		if listErr != nil {
			lgr.V(1).Info("failed to list local solutions", "error", listErr)
		} else {
			artifacts = append(artifacts, localArtifacts...)
		}
	} else {
		lgr.V(1).Info("local catalog not available", "error", err)
	}

	// List from remote catalogs
	remoteCatalogs := catalog.RemoteCatalogsFromContext(ctx, *lgr)
	for _, rc := range remoteCatalogs {
		remoteArtifacts, listErr := rc.List(ctx, catalog.ArtifactKindSolution, "")
		if listErr != nil {
			lgr.V(1).Info("failed to list remote solutions", "catalog", rc.Name(), "error", listErr)
			continue
		}
		artifacts = append(artifacts, remoteArtifacts...)
	}

	if len(artifacts) == 0 {
		if w != nil {
			w.Infof("No solutions found. Use '%s get solution --local' to auto-discover a local solution.", o.CliParams.BinaryName)
		}
		return nil
	}

	items := deduplicateAndFormatSolutions(artifacts)

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
	return kvxOpts.Write(items)
}

// deduplicateAndFormatSolutions deduplicates artifacts by name (keeping the
// highest version) and returns sorted display items.
func deduplicateAndFormatSolutions(artifacts []catalog.ArtifactInfo) []solutionListItem {
	latestByName := make(map[string]catalog.ArtifactInfo)
	for _, a := range artifacts {
		existing, ok := latestByName[a.Reference.Name]
		if !ok {
			latestByName[a.Reference.Name] = a
			continue
		}
		if a.Reference.Version != nil && (existing.Reference.Version == nil || a.Reference.Version.GreaterThan(existing.Reference.Version)) {
			latestByName[a.Reference.Name] = a
		}
	}

	items := make([]solutionListItem, 0, len(latestByName))
	for _, a := range latestByName {
		version := ""
		if a.Reference.Version != nil {
			version = a.Reference.Version.String()
		}
		items = append(items, solutionListItem{
			Name:    a.Reference.Name,
			Version: version,
			Kind:    string(a.Reference.Kind),
			Catalog: a.Catalog,
		})
	}

	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items
}

// solutionListItem is a display-friendly representation for solution listing.
type solutionListItem struct {
	Name    string `json:"name" yaml:"name"`
	Version string `json:"version" yaml:"version"`
	Kind    string `json:"kind" yaml:"kind"`
	Catalog string `json:"catalog" yaml:"catalog"`
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
