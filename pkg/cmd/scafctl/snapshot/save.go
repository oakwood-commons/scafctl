// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package snapshot

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/execute"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// SaveOptions holds options for the save command
type SaveOptions struct {
	ConfigFile string
	OutputFile string
	Redact     bool
	Parameters []string
}

// CommandSave creates the snapshot save command
func CommandSave(_ *settings.Run, ioStreams terminal.IOStreams, binaryName string) *cobra.Command {
	opts := &SaveOptions{}

	cmd := &cobra.Command{
		Use:          "save [config-file]",
		Short:        "Execute resolvers and save snapshot",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			Execute resolvers from a configuration file and save the execution state
			to a snapshot file.
			
			The snapshot includes:
			  - All resolver values and status
			  - Execution timing and metrics
			  - Failed attempts and errors
			  - Input parameters used
			  - Phase-level information
			
			Snapshots can be used for debugging, testing, comparison, and audit trails.
		`),
		Example: heredoc.Docf(`
			# Save execution snapshot
			$ %s snapshot save config.yaml --output snapshot.json
			
			# Save with parameters
			$ %s snapshot save config.yaml -o snapshot.json -r env=prod -r region=us-west-2
			
			# Save with sensitive data redacted
			$ %s snapshot save config.yaml -o snapshot.json --redact
		`, binaryName, binaryName, binaryName),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.ConfigFile = args[0]
			return runSave(cmd.Context(), opts, ioStreams)
		},
	}

	cmd.Flags().StringVarP(&opts.OutputFile, "output", "o", "", "Output snapshot file (required)")
	cmd.Flags().BoolVar(&opts.Redact, "redact", false, "Redact sensitive values in snapshot")
	cmd.Flags().StringArrayVarP(&opts.Parameters, "resolver", "r", []string{}, "Resolver parameters (key=value)")

	_ = cmd.MarkFlagRequired("output")

	return cmd
}

func runSave(ctx context.Context, opts *SaveOptions, _ terminal.IOStreams) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	// Helper to write error
	writeErr := func(err error) {
		if w != nil {
			w.Errorf("%v", err)
		}
	}

	// Read and parse config file
	lgr.V(-1).Info("reading config file", "file", opts.ConfigFile)
	data, err := os.ReadFile(opts.ConfigFile)
	if err != nil {
		err = fmt.Errorf("failed to read config file: %w", err)
		writeErr(err)
		return exitcode.WithCode(err, exitcode.FileNotFound)
	}

	// Parse configuration
	var config struct {
		Solution  string               `yaml:"solution" json:"solution"`
		Version   string               `yaml:"version" json:"version"`
		Resolvers []*resolver.Resolver `yaml:"resolvers" json:"resolvers"`
	}

	if err := yaml.Unmarshal(data, &config); err != nil {
		err = fmt.Errorf("failed to parse config file: %w", err)
		writeErr(err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	if len(config.Resolvers) == 0 {
		err := fmt.Errorf("no resolvers found in config file")
		writeErr(err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Parse parameters
	params := make(map[string]any)
	for _, param := range opts.Parameters {
		// Simple key=value parsing (could be enhanced with flags.ParseKeyValueCSV)
		var key, value string
		if _, err := fmt.Sscanf(param, "%[^=]=%s", &key, &value); err != nil {
			err := fmt.Errorf("invalid parameter format '%s': expected key=value", param)
			writeErr(err)
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}
		params[key] = value
	}

	lgr.V(-1).Info("executing resolvers",
		"count", len(config.Resolvers),
		"parameters", len(params))

	// Execute resolvers
	providerRegistry := provider.NewRegistry()
	// Create adapter for registry
	registryAdapter := execute.NewResolverRegistryAdapter(providerRegistry)
	executor := resolver.NewExecutor(registryAdapter)
	start := time.Now()
	execCtx, err := executor.Execute(ctx, config.Resolvers, params)
	duration := time.Since(start)
	status := resolver.ExecutionStatusSuccess
	if err != nil {
		lgr.V(1).Info("resolver execution completed with errors", "error", err)
		status = resolver.ExecutionStatusFailed
		// Continue to capture snapshot even with errors
	}

	// Capture snapshot
	lgr.V(-1).Info("capturing snapshot")
	snapshot, err := resolver.CaptureSnapshot(
		execCtx,
		config.Solution,
		config.Version,
		settings.VersionInformation.BuildVersion,
		params,
		duration,
		status,
	)
	if err != nil {
		err = fmt.Errorf("failed to capture snapshot: %w", err)
		writeErr(err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	// Redact sensitive values if requested
	if opts.Redact {
		lgr.V(-1).Info("redacting sensitive values")
		// Manually redact based on resolver Sensitive flag
		sensitiveMap := make(map[string]bool)
		for _, r := range config.Resolvers {
			if r.Sensitive {
				sensitiveMap[r.Name] = true
			}
		}
		for name, sr := range snapshot.Resolvers {
			if sr == nil {
				continue
			}
			if sensitiveMap[name] {
				sr.Value = "<redacted>"
				sr.Sensitive = true
			}
		}
	}

	// Save snapshot
	lgr.V(-1).Info("saving snapshot", "output", opts.OutputFile)
	if err := resolver.SaveSnapshot(snapshot, opts.OutputFile); err != nil {
		err = fmt.Errorf("failed to save snapshot: %w", err)
		writeErr(err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	if w != nil {
		w.Successf("Snapshot saved to %s", opts.OutputFile)
		w.Plainf("  Solution: %s (v%s)\n", snapshot.Metadata.Solution, snapshot.Metadata.Version)
		w.Plainf("  Resolvers: %d\n", len(snapshot.Resolvers))
		w.Plainf("  Duration: %s\n", snapshot.Metadata.TotalDuration)
		w.Plainf("  Status: %s\n", snapshot.Metadata.Status)
	}

	return nil
}
