// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	appconfig "github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// ShowOptions holds options for the config show command.
type ShowOptions struct {
	IOStreams  *terminal.IOStreams
	CliParams  *settings.Run
	ConfigPath string
}

// CommandShow creates the 'config show' command.
//
//nolint:dupl // Cobra command boilerplate is intentionally similar across commands
func CommandShow(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &ShowOptions{}

	cCmd := &cobra.Command{
		Use:   "show",
		Short: "Show effective configuration with sources",
		Long: strings.NewReplacer(
			settings.CliBinaryName, cliParams.BinaryName,
			settings.SafeEnvPrefix(settings.CliBinaryName), settings.SafeEnvPrefix(cliParams.BinaryName),
		).Replace(heredoc.Doc(`
			Show the effective configuration including where each value comes from.

			Displays the merged configuration from all sources:
			  - Config file (if present)
			  - Environment variables (SCAFCTL_*)
			  - Default values

			Each section shows whether it came from the config file,
			an environment variable, or is using the default value.

			Examples:
			  # Show effective configuration
			  scafctl config show

			  # Show config for a specific config file
			  scafctl config show --config ./my-config.yaml
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

			// Get config path from parent command context
			if configFlag := cCmd.Root().Flag("config"); configFlag != nil && configFlag.Value.String() != "" {
				opts.ConfigPath = configFlag.Value.String()
			}

			return opts.Run(ctx)
		},
		SilenceUsage: true,
	}

	return cCmd
}

// Run executes the config show command.
func (o *ShowOptions) Run(ctx context.Context) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return fmt.Errorf("writer not initialized in context")
	}

	mgr := appconfig.NewManager(o.ConfigPath)
	cfg, err := mgr.Load()
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.ConfigError)
	}

	// Determine config file path
	configPath := o.ConfigPath
	if configPath == "" {
		var err error
		configPath, err = paths.ConfigFile()
		if err != nil {
			err = fmt.Errorf("failed to determine config path: %w", err)
			w.Errorf("%v", err)
			return exitcode.WithCode(err, exitcode.ConfigError)
		}
	}

	// Check if config file exists
	configFileExists := false
	if _, err := os.Stat(configPath); err == nil {
		configFileExists = true
	}

	w.Plainf("# Effective Configuration\n")
	w.Plainf("# =======================\n")
	w.Plainf("#\n")

	if configFileExists {
		w.Plainf("# Config file: %s\n", configPath)
	} else {
		w.Plainf("# Config file: (not found - using defaults)\n")
	}

	// List any active environment overrides
	envOverrides := o.findEnvOverrides()
	if len(envOverrides) > 0 {
		w.Plainf("# Environment overrides:\n")
		for _, env := range envOverrides {
			w.Plainf("#   %s=%s\n", env.key, env.value)
		}
	} else {
		w.Plainf("# Environment overrides: (none)\n")
	}
	w.Plainf("#\n\n")

	// Marshal and print config
	output, err := yaml.Marshal(cfg)
	if err != nil {
		err = fmt.Errorf("failed to marshal config: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	w.Plainf("%s", string(output))

	return nil
}

// envOverride represents an environment variable override.
type envOverride struct {
	key   string
	value string
}

// sensitiveEnvKeywords is the list of substrings that mark an env var as sensitive.
// Keys containing any of these words (case-insensitive) have their value redacted.
var sensitiveEnvKeywords = []string{ //nolint:gochecknoglobals
	"secret", "password", "token", "credential", "apikey", "api_key", "private_key", "privatekey",
}

// redactEnvValue returns "***REDACTED***" if the key contains a sensitive keyword,
// otherwise it returns the original value unchanged.
func redactEnvValue(key, value string) string {
	lower := strings.ToLower(key)
	for _, kw := range sensitiveEnvKeywords {
		if strings.Contains(lower, kw) {
			return appconfig.RedactedValue
		}
	}
	return value
}

// findEnvOverrides finds environment variables with the configured binary name prefix.
func (o *ShowOptions) findEnvOverrides() []envOverride {
	var overrides []envOverride
	prefix := settings.SafeEnvPrefix(o.CliParams.BinaryName) + "_"

	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key, value := parts[0], parts[1]
		if strings.HasPrefix(key, prefix) {
			overrides = append(overrides, envOverride{key: key, value: redactEnvValue(key, value)})
		}
	}

	return overrides
}
