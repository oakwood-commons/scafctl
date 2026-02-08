// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/pbkdf2"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

// CommandImport creates the 'secrets import' command.
func CommandImport(_ *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	var (
		dryRunFlag    bool
		overwriteFlag bool
	)

	cmd := &cobra.Command{
		Use:   "import <file>",
		Short: "Import secrets from a file",
		Long: `Import secrets from a file created by 'secrets export'.

Supports both plaintext (YAML/JSON) and encrypted formats.
Format is auto-detected from file content.

Internal secrets (scafctl.*) in the import file are automatically skipped.

Use --dry-run to preview what would be imported without making changes.
Use --overwrite to replace existing secrets.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w := writer.MustFromContext(ctx)
			inputFile := args[0]

			store, err := secrets.New()
			if err != nil {
				err := fmt.Errorf("failed to initialize secrets store: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.ConfigError)
			}

			// Read file
			fileData, err := os.ReadFile(inputFile)
			if err != nil {
				err := fmt.Errorf("failed to read file '%s': %w", inputFile, err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.FileNotFound)
			}

			// Detect format and decrypt if needed
			var importData ExportFormat
			if bytes.HasPrefix(fileData, []byte(encryptedHeader)) {
				// Encrypted format
				fmt.Fprint(ioStreams.ErrOut, "Enter decryption password: ")
				passwordBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
				if err != nil {
					err := fmt.Errorf("failed to read password: %w", err)
					w.Errorf("%v", err)
					return exitcode.WithCode(err, exitcode.GeneralError)
				}
				password := string(passwordBytes)
				fmt.Fprintln(ioStreams.ErrOut)

				decrypted, err := decryptExport(fileData, password)
				if err != nil {
					err := fmt.Errorf("failed to decrypt: %w", err)
					w.Errorf("%v", err)
					return exitcode.WithCode(err, exitcode.GeneralError)
				}

				// Try JSON first, then YAML
				if err := json.Unmarshal(decrypted, &importData); err != nil {
					if err := yaml.Unmarshal(decrypted, &importData); err != nil {
						err := fmt.Errorf("failed to parse decrypted data: %w", err)
						w.Errorf("%v", err)
						return exitcode.WithCode(err, exitcode.InvalidInput)
					}
				}
			} else {
				// Plaintext format - try JSON first, then YAML
				if err := json.Unmarshal(fileData, &importData); err != nil {
					if err := yaml.Unmarshal(fileData, &importData); err != nil {
						err := fmt.Errorf("failed to parse file (unsupported format): %w", err)
						w.Errorf("%v", err)
						return exitcode.WithCode(err, exitcode.InvalidInput)
					}
				}
			}

			// Validate version
			if importData.Version != exportVersion {
				w.Warningf("Warning: Import file version '%s' does not match expected '%s'\n",
					importData.Version, exportVersion)
			}

			// Filter out internal secrets
			userSecrets := make([]ExportedSecret, 0, len(importData.Secrets))
			skippedInternal := 0
			for _, secret := range importData.Secrets {
				if err := ValidateUserSecretName(secret.Name); err != nil {
					w.Warningf("Skipping internal secret: %s\n", secret.Name)
					skippedInternal++
					continue
				}
				userSecrets = append(userSecrets, secret)
			}

			if len(userSecrets) == 0 {
				w.Warning("No user secrets found in import file")
				return nil
			}

			// Dry run - just preview
			if dryRunFlag {
				w.Info("Dry run - would import:")
				for _, secret := range userSecrets {
					w.Infof("  - %s (%d bytes)\n", secret.Name, len(secret.Value))
				}
				if skippedInternal > 0 {
					w.Warningf("Skipped %d internal secret(s)\n", skippedInternal)
				}
				return nil
			}

			// Import secrets
			imported := 0
			updated := 0
			skipped := 0

			for _, secret := range userSecrets {
				exists, err := store.Exists(ctx, secret.Name)
				if err != nil {
					w.Warningf("Failed to check if '%s' exists: %v\n", secret.Name, err)
					continue
				}

				if exists && !overwriteFlag {
					w.Warningf("Skipping existing secret: %s (use --overwrite to replace)\n", secret.Name)
					skipped++
					continue
				}

				if err := store.Set(ctx, secret.Name, []byte(secret.Value)); err != nil {
					w.Warningf("Failed to import '%s': %v\n", secret.Name, err)
					continue
				}

				if exists {
					updated++
				} else {
					imported++
				}
			}

			// Summary
			w.Successf("Import complete:\n")
			if imported > 0 {
				w.Successf("  - Created: %d\n", imported)
			}
			if updated > 0 {
				w.Successf("  - Updated: %d\n", updated)
			}
			if skipped > 0 {
				w.Warningf("  - Skipped: %d\n", skipped)
			}
			if skippedInternal > 0 {
				w.Warningf("  - Skipped internal: %d\n", skippedInternal)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRunFlag, "dry-run", false, "Preview import without making changes")
	cmd.Flags().BoolVar(&overwriteFlag, "overwrite", false, "Overwrite existing secrets")

	return cmd
}

// decryptExport decrypts an encrypted export file.
func decryptExport(data []byte, password string) ([]byte, error) {
	// Remove header
	if !bytes.HasPrefix(data, []byte(encryptedHeader)) {
		return nil, fmt.Errorf("invalid encrypted format")
	}
	data = data[len(encryptedHeader):]

	// Parse: salt + iterations + base64-ciphertext
	if len(data) < pbkdf2SaltSize+4 {
		return nil, fmt.Errorf("invalid encrypted data: too short")
	}

	salt := data[:pbkdf2SaltSize]
	iterationsBytes := data[pbkdf2SaltSize : pbkdf2SaltSize+4]
	iterations := binary.BigEndian.Uint32(iterationsBytes)
	ciphertextB64 := data[pbkdf2SaltSize+4:]

	// Decode base64
	ciphertext, err := base64.StdEncoding.DecodeString(string(ciphertextB64))
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64: %w", err)
	}

	// Derive key
	key := pbkdf2.Key([]byte(password), salt, int(iterations), pbkdf2KeySize, sha256.New)

	// Create cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Extract nonce
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce := ciphertext[:nonceSize]
	ciphertext = ciphertext[nonceSize:]

	// Decrypt
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed (wrong password?): %w", err)
	}

	return plaintext, nil
}
