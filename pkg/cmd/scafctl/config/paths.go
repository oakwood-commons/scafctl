// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	appconfig "github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

//go:embed paths_schema.json
var pathsSchemaJSON []byte

// Supported platforms for --platform flag.
var supportedPlatforms = paths.SupportedPlatforms

// PathsOptions holds options for the config paths command.
type PathsOptions struct {
	BinaryName     string
	IOStreams      *terminal.IOStreams
	CliParams      *settings.Run
	KvxOutputFlags flags.KvxOutputFlags
	Platform       string
}

// pathRow is the kvx-friendly row shape emitted by `config paths`.
type pathRow struct {
	Name         string `json:"name" yaml:"name"`
	Path         string `json:"path" yaml:"path"`
	Description  string `json:"description" yaml:"description"`
	XDGVariable  string `json:"xdgVariable,omitempty" yaml:"xdgVariable,omitempty"`
	Platform     string `json:"platform" yaml:"platform"`
	Illustrative bool   `json:"illustrative" yaml:"illustrative"`
}

// CommandPaths creates the 'config paths' command.
func CommandPaths(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &PathsOptions{}

	cCmd := &cobra.Command{
		Use:   "paths",
		Short: fmt.Sprintf("Show XDG-compliant paths used by %s", strings.SplitN(path, "/", 2)[0]),
		Long: strings.NewReplacer(
			settings.CliBinaryName, cliParams.BinaryName,
			settings.SafeEnvPrefix(settings.CliBinaryName), settings.SafeEnvPrefix(cliParams.BinaryName),
		).Replace(heredoc.Doc(`
			Display all paths used by scafctl.

			scafctl follows the XDG Base Directory Specification for storing
			configuration, data, cache, and state files. This command shows
			the resolved paths for the current system.

			Use --platform to see illustrative paths for other operating systems.
			This is useful for documentation or cross-platform reference.

			Environment variables can override default paths:
			  - XDG_CONFIG_HOME: Configuration files
			  - XDG_DATA_HOME: User data (secrets, catalogs)
			  - XDG_CACHE_HOME: Cache files
			  - XDG_STATE_HOME: State files (logs, history)
			  - SCAFCTL_SECRETS_DIR: Override secrets location specifically

			Standard kvx output flags (-o, -i, -e/--expression, -w/--where) are
			supported. On a real terminal, human formats also print the config-file
			merge order to stderr; machine-readable formats keep stdout and stderr
			clean.

			Examples:
			  # Show all paths for current system
			  scafctl config paths

			  # Show paths for a specific platform
			  scafctl config paths --platform linux

			  # Emit machine-readable output
			  scafctl config paths -o json
			  scafctl config paths -o yaml

			  # Browse interactively
			  scafctl config paths -i

			  # Filter rows with CEL
			  scafctl config paths --where 'name == "Config"'
		`)),
		Args: cobra.NoArgs,
		RunE: func(cCmd *cobra.Command, _ []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cCmd.Use)
			ctx := settings.IntoContext(cCmd.Context(), cliParams)

			if lgr := logger.FromContext(cCmd.Context()); lgr != nil {
				ctx = logger.WithLogger(ctx, lgr)
			}

			w := writer.FromContext(cCmd.Context())
			if w == nil {
				w = writer.New(ioStreams, cliParams)
			}
			ctx = writer.WithWriter(ctx, w)

			opts.IOStreams = ioStreams
			opts.CliParams = cliParams
			opts.BinaryName = cliParams.BinaryName
			opts.KvxOutputFlags.AppName = cliParams.BinaryName

			return opts.Run(ctx)
		},
		SilenceUsage: true,
	}

	flags.AddKvxOutputFlagsToStruct(cCmd, &opts.KvxOutputFlags)
	cCmd.Flags().StringVar(&opts.Platform, "platform", "", "Show illustrative paths for a specific platform (linux, darwin/macos, windows)")

	return cCmd
}

// Run executes the config paths command.
func (o *PathsOptions) Run(ctx context.Context) error {
	if o.BinaryName == "" {
		o.BinaryName = settings.CliBinaryName
	}

	w := writer.FromContext(ctx)
	if w == nil {
		return fmt.Errorf("writer not initialized in context")
	}

	targetPlatform, isIllustrative, err := o.resolvePlatform()
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	var pathInfos []paths.PathInfo
	if isIllustrative {
		pathInfos = paths.IllustrativePaths(targetPlatform)
	} else {
		pathInfos = paths.AllPaths()
	}

	rows := buildPathRows(pathInfos, targetPlatform, isIllustrative)

	kvxOpts := flags.ToKvxOutputOptions(&o.KvxOutputFlags,
		kvx.WithOutputContext(ctx),
		kvx.WithOutputNoColor(o.CliParams != nil && o.CliParams.NoColor),
		kvx.WithOutputAppName(o.BinaryName+" config paths"),
		kvx.WithOutputDisplaySchemaJSON(pathsSchemaJSON),
		kvx.WithIOStreams(o.IOStreams),
		kvx.WithOutputColumnOrder([]string{"name", "path", "platform"}),
		kvx.WithOutputColumnHints(map[string]tui.ColumnHint{
			"name":         {MaxWidth: 12, Priority: 10},
			"path":         {MaxWidth: 60, Priority: 9, Flex: true},
			"platform":     {MaxWidth: 10, Priority: 7},
			"description":  {Hidden: true},
			"xdgVariable":  {Hidden: true},
			"illustrative": {Hidden: true},
		}),
	)

	if err := kvxOpts.Write(rowsToGeneric(rows)); err != nil {
		return err
	}

	if !isIllustrative && isHumanFormat(kvxOpts.Format) {
		if sources := o.configSourceInfos(); len(sources) > 0 {
			renderConfigSourcesNote(w, o.BinaryName, sources)
		}
	}

	return nil
}

// resolvePlatform normalizes --platform and reports whether the result diverges
// from the runtime OS.
func (o *PathsOptions) resolvePlatform() (string, bool, error) {
	if o.Platform == "" {
		return runtime.GOOS, false, nil
	}

	target := strings.ToLower(o.Platform)
	if target == "macos" {
		target = "darwin"
	}
	if !slices.Contains(supportedPlatforms, target) {
		return "", false, fmt.Errorf("unsupported platform %q; supported platforms: linux, darwin (or macos), windows", o.Platform)
	}
	return target, target != runtime.GOOS, nil
}

// buildPathRows converts PathInfo entries into kvx-friendly rows, tagging every
// row with the platform they describe.
func buildPathRows(infos []paths.PathInfo, platform string, illustrative bool) []pathRow {
	rows := make([]pathRow, 0, len(infos))
	for _, p := range infos {
		rows = append(rows, pathRow{
			Name:         p.Name,
			Path:         p.Path,
			Description:  p.Description,
			XDGVariable:  p.XDGVariable,
			Platform:     platform,
			Illustrative: illustrative,
		})
	}
	return rows
}

// rowsToGeneric converts a typed row slice to kvx's generic list form
// ([]any of map[string]any). Structured formats (json/yaml/csv/toml) apply the
// per-item Where filter before serialization, and kvx's CEL evaluator only
// treats generic map-based lists as filterable list data.
func rowsToGeneric(rows []pathRow) []any {
	out := make([]any, len(rows))
	for i, r := range rows {
		m := map[string]any{
			"name":         r.Name,
			"path":         r.Path,
			"description":  r.Description,
			"platform":     r.Platform,
			"illustrative": r.Illustrative,
		}
		if r.XDGVariable != "" {
			m["xdgVariable"] = r.XDGVariable
		}
		out[i] = m
	}
	return out
}

// isHumanFormat reports whether the format renders human/interactive output on
// stdout. Structured formats and quiet suppress the merge-order note so piped
// runs stay clean.
func isHumanFormat(format kvx.OutputFormat) bool {
	if kvx.IsStructuredFormat(format) {
		return false
	}
	if format == kvx.OutputFormatQuiet {
		return false
	}
	return true
}

// renderConfigSourcesNote prints the config-file merge order to stderr. Callers
// gate on format so machine-readable output never sees this text.
func renderConfigSourcesNote(w *writer.Writer, binaryName string, sources []configSource) {
	w.PlainStderr("")
	w.PlainStderr("Config sources (merge order)")
	w.PlainStderr("")
	idx := 1
	w.PlainStderrf("  %d. built-in defaults", idx)
	idx++
	for _, s := range sources {
		line := s.Info.Path
		if !s.Exists {
			line += "  (not present)"
		}
		w.PlainStderrf("  %d. %s", idx, line)
		idx++
	}
	w.PlainStderrf("  %d. %s_* environment variables", idx, settings.SafeEnvPrefix(binaryName))
	w.PlainStderr("")
	w.PlainStderr("Later sources override earlier ones.")
}

// configSource is a single on-disk config file layer feeding the merged
// configuration, paired with whether the file currently exists on disk.
type configSource struct {
	Info   paths.PathInfo
	Exists bool
}

// configSourceInfos returns the on-disk config file sources that feed the merged
// configuration, in merge order: each config.d fragment (lexical), followed by
// the user config file. Non-file layers (built-in defaults, any embedder base
// config, and SCAFCTL_* environment overrides) are surfaced separately by the
// stderr note. Returns nil when the config path cannot be resolved.
func (o *PathsOptions) configSourceInfos() []configSource {
	configPath, err := paths.ConfigFile()
	if err != nil {
		return nil
	}

	fragments, err := appconfig.DirFragments(filepath.Dir(configPath))
	if err != nil {
		return nil
	}

	sources := make([]configSource, 0, len(fragments)+1)
	for _, frag := range fragments {
		// Fragments come from a directory glob, so they exist by construction.
		sources = append(sources, configSource{
			Info: paths.PathInfo{
				Name:        "config.d",
				Path:        frag,
				Description: "Drop-in fragment (loaded in merge order)",
			},
			Exists: true,
		})
	}

	_, statErr := os.Stat(configPath)
	exists := statErr == nil
	desc := "User config file"
	if !exists {
		desc = "User config file (not present)"
	}
	sources = append(sources, configSource{
		Info: paths.PathInfo{
			Name:        "config.yaml",
			Path:        configPath,
			Description: desc,
		},
		Exists: exists,
	})

	return sources
}
