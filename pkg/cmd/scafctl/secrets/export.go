// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/secrets/secretcrypto"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/input"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

type exportRunOptions struct {
	outputFile       string
	format           string
	encrypt          bool
	all              bool
	confirmPlaintext func(ctx context.Context, isQuiet bool) (bool, error)
}

// CommandExport creates the 'secrets export' command.
func CommandExport(cliParams *settings.Run, _ *terminal.IOStreams, _ string) *cobra.Command {
	var (
		outputFile  string
		formatFlag  string
		encryptFlag bool
		allFlag     bool
	)

	binaryName := settings.CliBinaryName
	if cliParams != nil && cliParams.BinaryName != "" {
		binaryName = cliParams.BinaryName
	}

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export secrets to a file",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Export secrets to a file for backup or migration.

			WARNING: By default, secrets are exported in PLAINTEXT!
			Store the exported file securely or use --encrypt to password-protect it.

			Supported formats:
			  - yaml (default): YAML format
			  - json:           JSON format

			Use --encrypt to export with password protection (AES-256-GCM).
			Use --all to include internal secrets (e.g. auth tokens).
			By default, internal secrets (scafctl.*) are excluded.

			Examples:
			  # Export secrets as YAML
			  scafctl secrets export --output-file secrets-backup.yaml

			  # Export secrets as JSON with encryption
			  scafctl secrets export --output-file secrets-backup.enc -o json --encrypt
		`), settings.CliBinaryName, binaryName),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runExport(cmd.Context(), cliParams, exportRunOptions{
				outputFile: outputFile,
				format:     formatFlag,
				encrypt:    encryptFlag,
				all:        allFlag,
			})
		},
	}

	cmd.Flags().StringVar(&outputFile, "output-file", "", "Output file path (required)")
	cmd.Flags().StringVarP(&formatFlag, "output", "o", "yaml", "Output format: yaml, json")
	cmd.Flags().BoolVar(&encryptFlag, "encrypt", false, "Encrypt export with password")
	cmd.Flags().BoolVarP(&allFlag, "all", "a", false, "Include internal secrets (e.g. auth tokens)")
	_ = cmd.MarkFlagRequired("output-file")

	return cmd
}

func runExport(ctx context.Context, cliParams *settings.Run, opts exportRunOptions) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return fmt.Errorf("writer not initialized in context")
	}

	if opts.outputFile == "" {
		err := fmt.Errorf("output file is required (use --output-file)")
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	if opts.format != "yaml" && opts.format != "json" {
		err := fmt.Errorf("unsupported output format %q (use yaml or json)", opts.format)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	store, err := newStoreFromContext(ctx)
	if err != nil {
		err := fmt.Errorf("failed to initialize secrets store: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.ConfigError)
	}

	// List all secrets
	names, err := store.List(ctx)
	if err != nil {
		err := fmt.Errorf("failed to list secrets: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	// Filter secrets based on --all flag across both the configured binary
	// namespace and the default internal namespace.
	var binaryName string
	if cliParams != nil {
		binaryName = cliParams.BinaryName
	}
	prefix := secrets.InternalSecretPrefixFor(binaryName)
	filtered := secrets.FilterSecretsFor(names, opts.all, prefix)
	if prefix != secrets.InternalSecretPrefix {
		filtered = secrets.FilterSecrets(filtered, opts.all)
	}

	if len(filtered) == 0 {
		w.Warning("No secrets to export")
		return nil
	}

	// Retrieve all secret values
	exportData := secretcrypto.ExportFormat{
		Version:    secretcrypto.ExportVersion,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Secrets:    make([]secretcrypto.ExportedSecret, 0, len(filtered)),
	}

	for _, name := range filtered {
		value, err := store.Get(ctx, name)
		if err != nil {
			w.Warningf("Skipping secret '%s': %v\n", name, err)
			continue
		}
		exportData.Secrets = append(exportData.Secrets, secretcrypto.ExportedSecret{
			Name:  name,
			Value: string(value),
		})
	}

	if len(exportData.Secrets) == 0 {
		w.Warning("No secrets retrieved for export")
		return nil
	}

	var password string
	if opts.encrypt {
		in := input.FromContext(ctx)
		if in == nil {
			return fmt.Errorf("input not initialized in context")
		}
		password, err = in.ReadPassword(input.NewPasswordOptions().
			WithPrompt("Enter encryption password").
			WithConfirmation(true).
			WithMinLength(1))
		if err != nil {
			err := fmt.Errorf("failed to read password: %w", err)
			w.Errorf("%v", err)
			return exitcode.WithCode(err, exitcode.GeneralError)
		}
	} else {
		// Show scary warning for plaintext export
		w.Warning("⚠️  WARNING: Exporting secrets in PLAINTEXT!")
		w.Warning("   This file will contain UNENCRYPTED secrets.")
		w.Warning("   Store it securely or use --encrypt flag.")

		isQuiet := cliParams != nil && cliParams.IsQuiet
		var confirmed bool
		if opts.confirmPlaintext != nil {
			confirmed, err = opts.confirmPlaintext(ctx, isQuiet)
		} else {
			in := input.FromContext(ctx)
			if in == nil {
				return fmt.Errorf("input not initialized in context")
			}
			confirmed, err = in.Confirm(input.NewConfirmOptions().
				WithPrompt("Continue with plaintext export?").
				WithDefault(false).
				WithSkipCondition(isQuiet))
		}
		if err != nil {
			err := fmt.Errorf("failed to read confirmation: %w", err)
			w.Errorf("%v", err)
			return exitcode.WithCode(err, exitcode.GeneralError)
		}
		if !confirmed {
			w.Info("Export cancelled")
			return nil
		}
	}

	// Encode and optionally encrypt
	exportBytes, err := secrets.Export(&exportData, secrets.ExportOptions{
		Format:   opts.format,
		Encrypt:  opts.encrypt,
		Password: password,
	})
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	// Write to file
	if err := os.WriteFile(opts.outputFile, exportBytes, 0o600); err != nil {
		err := fmt.Errorf("failed to write export file: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	if opts.encrypt {
		w.Successf("✓ Exported %d secrets to %s (encrypted)\n", len(exportData.Secrets), opts.outputFile)
	} else {
		w.Successf("Exported %d secrets to %s (plaintext)\n", len(exportData.Secrets), opts.outputFile)
	}

	return nil
}
