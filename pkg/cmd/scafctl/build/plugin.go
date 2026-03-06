// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package build

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// PluginOptions holds options for the build plugin command.
type PluginOptions struct {
	Name      string
	Kind      string // "provider" or "auth-handler"
	Version   string
	Platforms []string // e.g. ["linux/amd64=./bin/linux-amd64/my-plugin", "darwin/arm64=./bin/darwin-arm64/my-plugin"]
	Force     bool
	CliParams *settings.Run
	IOStreams *terminal.IOStreams
}

// CommandBuildPlugin creates the build plugin command.
func CommandBuildPlugin(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	options := &PluginOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:          "plugin",
		Aliases:      []string{"plug", "p"},
		Short:        "Build a multi-platform plugin into the local catalog",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			Build one or more platform-specific plugin binaries into the local catalog
			as an OCI image index (multi-platform artifact).

			Each --platform flag maps a target platform to the local path of the pre-built
			binary for that platform. The format is:

			  --platform <os/arch>=<path>

			Supported platforms: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64

			The resulting artifact is stored as an OCI image index (fat manifest) with one
			manifest per platform. At runtime, scafctl automatically selects the correct
			binary for the current OS and architecture.

			If only a single --platform is specified, the artifact is still stored as an
			image index for forward compatibility.

			Examples:
			  # Build a provider plugin for two platforms
			  scafctl build plugin --name aws-provider --kind provider --version 1.0.0 \
			    --platform linux/amd64=./dist/aws-provider-linux-amd64 \
			    --platform darwin/arm64=./dist/aws-provider-darwin-arm64

			  # Build an auth handler for all supported platforms
			  scafctl build plugin --name github-auth --kind auth-handler --version 2.1.0 \
			    --platform linux/amd64=./dist/github-auth-linux-amd64 \
			    --platform linux/arm64=./dist/github-auth-linux-arm64 \
			    --platform darwin/amd64=./dist/github-auth-darwin-amd64 \
			    --platform darwin/arm64=./dist/github-auth-darwin-arm64 \
			    --platform windows/amd64=./dist/github-auth-windows-amd64.exe

			  # Overwrite existing version
			  scafctl build plugin --name aws-provider --kind provider --version 1.0.0 \
			    --platform linux/amd64=./dist/aws-provider --force
		`),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runBuildPlugin(cmd.Context(), options)
		},
	}

	cmd.Flags().StringVar(&options.Name, "name", "", "Plugin artifact name (required)")
	cmd.Flags().StringVar(&options.Kind, "kind", "provider", "Plugin kind: 'provider' or 'auth-handler'")
	cmd.Flags().StringVar(&options.Version, "version", "", "Semantic version (required)")
	cmd.Flags().StringArrayVar(&options.Platforms, "platform", nil,
		"Platform-to-binary mapping in os/arch=path format (can be specified multiple times)")
	cmd.Flags().BoolVar(&options.Force, "force", false, "Overwrite existing version")

	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("version")
	_ = cmd.MarkFlagRequired("platform")

	return cmd
}

func runBuildPlugin(ctx context.Context, opts *PluginOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	// Validate name
	if !catalog.IsValidName(opts.Name) {
		w.Errorf("invalid name %q: must be lowercase alphanumeric with hyphens", opts.Name)
		return exitcode.Errorf("invalid name")
	}

	// Validate kind
	kind, ok := catalog.ParseArtifactKind(opts.Kind)
	if !ok || (kind != catalog.ArtifactKindProvider && kind != catalog.ArtifactKindAuthHandler) {
		w.Errorf("invalid kind %q: must be 'provider' or 'auth-handler'", opts.Kind)
		return exitcode.Errorf("invalid kind")
	}

	// Parse version
	version, err := semver.NewVersion(opts.Version)
	if err != nil {
		w.Errorf("invalid version %q: %v", opts.Version, err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Parse platform mappings
	binaries, err := parsePlatformFlags(opts.Platforms)
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Read binary files
	platformBinaries := make([]catalog.PlatformBinary, 0, len(binaries))
	for platform, path := range binaries {
		data, err := os.ReadFile(path)
		if err != nil {
			w.Errorf("failed to read binary for %s at %s: %v", platform, path, err)
			return exitcode.WithCode(err, exitcode.FileNotFound)
		}

		if len(data) == 0 {
			w.Errorf("binary for %s at %s is empty", platform, path)
			return exitcode.Errorf("empty binary")
		}

		platformBinaries = append(platformBinaries, catalog.PlatformBinary{
			Platform: platform,
			Data:     data,
		})

		w.Infof("  %s → %s (%s)", platform, path, formatBytes(int64(len(data))))
	}

	// Build reference
	ref := catalog.Reference{
		Kind:    kind,
		Name:    opts.Name,
		Version: version,
	}

	// Open local catalog
	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err != nil {
		w.Errorf("failed to open catalog: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Build annotations
	annotations := catalog.NewAnnotationBuilder().Build()

	// Store as multi-platform image index
	info, err := localCatalog.StoreMultiPlatform(ctx, ref, platformBinaries, annotations, opts.Force)
	if err != nil {
		if catalog.IsExists(err) {
			w.Errorf("%v\nUse --force to overwrite", err)
			return exitcode.WithCode(err, exitcode.CatalogError)
		}
		w.Errorf("failed to store plugin: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	lgr.V(1).Info("built multi-platform plugin",
		"name", info.Reference.Name,
		"version", info.Reference.Version.String(),
		"platforms", len(platformBinaries),
		"digest", info.Digest)

	w.Successf("Built %s@%s (%d platform(s))", info.Reference.Name, info.Reference.Version.String(), len(platformBinaries))
	w.Infof("  Digest: %s", info.Digest)
	w.Infof("  Catalog: %s", localCatalog.Path())
	for _, pb := range platformBinaries {
		w.Infof("  Platform: %s", pb.Platform)
	}

	return nil
}

// parsePlatformFlags parses --platform flags of the form "os/arch=path" into a
// map[platform]path. Validates that each platform is supported and the path exists.
func parsePlatformFlags(flags []string) (map[string]string, error) {
	result := make(map[string]string, len(flags))

	for _, flag := range flags {
		parts := strings.SplitN(flag, "=", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("invalid --platform format %q: expected os/arch=path (e.g., linux/amd64=./bin/my-plugin)", flag)
		}

		platform := parts[0]
		path := parts[1]

		if !catalog.IsSupportedPlatform(platform) {
			return nil, fmt.Errorf("unsupported platform %q: supported platforms are %v", platform, catalog.SupportedPluginPlatforms)
		}

		if _, exists := result[platform]; exists {
			return nil, fmt.Errorf("duplicate platform %q", platform)
		}

		// Resolve and validate path
		absPath, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("invalid path %q for platform %s: %w", path, platform, err)
		}

		info, err := os.Stat(absPath)
		if err != nil {
			return nil, fmt.Errorf("binary not found for platform %s at %q: %w", platform, path, err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("path for platform %s is a directory, expected a file: %s", platform, path)
		}

		result[platform] = absPath
	}

	return result, nil
}

// formatBytes formats bytes as a human-readable string (local copy to avoid
// pulling in the catalog cmd package).
func formatBytes(b int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case b >= gb:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
