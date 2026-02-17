// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/input"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/pbkdf2"
	"gopkg.in/yaml.v3"
)

const (
	exportVersion    = "scafctl-secrets-v1"
	encryptedHeader  = "SCAFCTL-ENC-V1\n"
	pbkdf2Iterations = 100000
	pbkdf2KeySize    = 32
	pbkdf2SaltSize   = 16
)

// ExportFormat represents the format for exported secrets.
type ExportFormat struct {
	Version    string           `json:"version" yaml:"version"`
	ExportedAt string           `json:"exported_at" yaml:"exported_at"`
	Secrets    []ExportedSecret `json:"secrets" yaml:"secrets"`
}

// ExportedSecret represents a single exported secret.
type ExportedSecret struct {
	Name  string `json:"name" yaml:"name"`
	Value string `json:"value" yaml:"value"`
}

// CommandExport creates the 'secrets export' command.
func CommandExport(cliParams *settings.Run, _ *terminal.IOStreams, _ string) *cobra.Command {
	var (
		outputFile  string
		formatFlag  string
		encryptFlag bool
		allFlag     bool
	)

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export secrets to a file",
		Long: `Export secrets to a file for backup or migration.

WARNING: By default, secrets are exported in PLAINTEXT!
Store the exported file securely or use --encrypt to password-protect it.

Supported formats:
  - yaml (default): YAML format
  - json:          JSON format

Use --encrypt to export with password protection (AES-256-GCM).
Use --all to include internal secrets (e.g. auth tokens).
By default, internal secrets (scafctl.*) are excluded.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			w := writer.MustFromContext(ctx)

			if outputFile == "" {
				err := fmt.Errorf("output file is required (use --output or -o)")
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			store, err := secrets.New()
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

			// Filter secrets based on --all flag
			filtered := FilterSecrets(names, allFlag)

			if len(filtered) == 0 {
				w.Warning("No secrets to export")
				return nil
			}

			// Retrieve all secret values
			exportData := ExportFormat{
				Version:    exportVersion,
				ExportedAt: time.Now().UTC().Format(time.RFC3339),
				Secrets:    make([]ExportedSecret, 0, len(filtered)),
			}

			for _, name := range filtered {
				value, err := store.Get(ctx, name)
				if err != nil {
					w.Warningf("Skipping secret '%s': %v\n", name, err)
					continue
				}
				exportData.Secrets = append(exportData.Secrets, ExportedSecret{
					Name:  name,
					Value: string(value),
				})
			}

			if len(exportData.Secrets) == 0 {
				w.Warning("No secrets retrieved for export")
				return nil
			}

			// Encode to bytes
			var exportBytes []byte
			switch formatFlag {
			case "json":
				exportBytes, err = json.Marshal(exportData)
				if err != nil {
					err := fmt.Errorf("failed to encode as JSON: %w", err)
					w.Errorf("%v", err)
					return exitcode.WithCode(err, exitcode.GeneralError)
				}
			case "yaml":
				exportBytes, err = yaml.Marshal(exportData)
				if err != nil {
					err := fmt.Errorf("failed to encode as YAML: %w", err)
					w.Errorf("%v", err)
					return exitcode.WithCode(err, exitcode.GeneralError)
				}
			default:
				err := fmt.Errorf("unsupported format: %s (use yaml or json)", formatFlag)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			// Encrypt if requested
			in := input.MustFromContext(ctx)
			if encryptFlag {
				password, err := in.ReadPassword(input.NewPasswordOptions().
					WithPrompt("Enter encryption password").
					WithConfirmation(true).
					WithMinLength(1))
				if err != nil {
					err := fmt.Errorf("failed to read password: %w", err)
					w.Errorf("%v", err)
					return exitcode.WithCode(err, exitcode.GeneralError)
				}

				exportBytes, err = encryptExport(exportBytes, password)
				if err != nil {
					err := fmt.Errorf("failed to encrypt export: %w", err)
					w.Errorf("%v", err)
					return exitcode.WithCode(err, exitcode.GeneralError)
				}
			} else {
				// Show scary warning for plaintext export
				w.Warning("⚠️  WARNING: Exporting secrets in PLAINTEXT!")
				w.Warning("   This file will contain UNENCRYPTED secrets.")
				w.Warning("   Store it securely or use --encrypt flag.")

				confirmed, err := in.Confirm(input.NewConfirmOptions().
					WithPrompt("Continue with plaintext export?").
					WithDefault(false).
					WithSkipCondition(cliParams.IsQuiet))
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

			// Write to file
			if err := os.WriteFile(outputFile, exportBytes, 0o600); err != nil {
				err := fmt.Errorf("failed to write export file: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			if encryptFlag {
				w.Successf("✓ Exported %d secrets to %s (encrypted)\n", len(exportData.Secrets), outputFile)
			} else {
				w.Successf("Exported %d secrets to %s (plaintext)\n", len(exportData.Secrets), outputFile)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&outputFile, "output", "o", "", "Output file path (required)")
	cmd.Flags().StringVarP(&formatFlag, "format", "f", "yaml", "Output format: yaml, json")
	cmd.Flags().BoolVar(&encryptFlag, "encrypt", false, "Encrypt export with password")
	cmd.Flags().BoolVarP(&allFlag, "all", "a", false, "Include internal secrets (e.g. auth tokens)")
	_ = cmd.MarkFlagRequired("output")

	return cmd
}

// encryptExport encrypts export data with a password using PBKDF2 + AES-256-GCM.
func encryptExport(data []byte, password string) ([]byte, error) {
	// Generate random salt
	salt := make([]byte, pbkdf2SaltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}

	// Derive key from password
	key := pbkdf2.Key([]byte(password), salt, pbkdf2Iterations, pbkdf2KeySize, sha256.New)

	// Create cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt
	ciphertext := gcm.Seal(nonce, nonce, data, nil)

	// Format: header + salt + iterations + ciphertext
	iterationsBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(iterationsBytes, pbkdf2Iterations)

	result := []byte(encryptedHeader)
	result = append(result, salt...)
	result = append(result, iterationsBytes...)
	result = append(result, []byte(base64.StdEncoding.EncodeToString(ciphertext))...)

	return result, nil
}
