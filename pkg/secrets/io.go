// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/secrets/secretcrypto"
	"gopkg.in/yaml.v3"
)

// IsEncrypted returns true if the data starts with the encrypted header marker.
func IsEncrypted(data []byte) bool {
	return bytes.HasPrefix(data, []byte(secretcrypto.EncryptedHeader))
}

// ImportOptions configures secret import behavior.
type ImportOptions struct {
	// Password for decrypting encrypted imports. Required when data is encrypted.
	Password string `json:"-"` // nosec G117 -- not serialized, used only as function parameter
}

// ImportResult holds the result of parsing an import file.
type ImportResult struct {
	// Secrets contains the user secrets from the import (internal secrets filtered out).
	Secrets []secretcrypto.ExportedSecret
	// SkippedInternal is the number of internal secrets that were filtered out.
	SkippedInternal int
	// Version is the version string from the import file.
	Version string
	// VersionMismatch is true if the import version differs from the current export version.
	VersionMismatch bool
}

// Import reads secrets from raw bytes, auto-detecting format (encrypted vs plaintext,
// JSON vs YAML), filtering out internal secrets, and returning a structured result.
func Import(data []byte, opts ImportOptions) (*ImportResult, error) {
	var importData secretcrypto.ExportFormat

	if bytes.HasPrefix(data, []byte(secretcrypto.EncryptedHeader)) {
		// Encrypted format
		if opts.Password == "" {
			return nil, fmt.Errorf("data is encrypted but no password provided")
		}
		decrypted, err := secretcrypto.Decrypt(data, opts.Password)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt: %w", err)
		}

		if err := parseSecretData(decrypted, &importData); err != nil {
			return nil, fmt.Errorf("failed to parse decrypted data: %w", err)
		}
	} else {
		if err := parseSecretData(data, &importData); err != nil {
			return nil, fmt.Errorf("failed to parse file (unsupported format): %w", err)
		}
	}

	// Filter out internal secrets
	userSecrets := make([]secretcrypto.ExportedSecret, 0, len(importData.Secrets))
	skippedInternal := 0
	for _, secret := range importData.Secrets {
		if err := ValidateUserSecretName(secret.Name); err != nil {
			skippedInternal++
			continue
		}
		userSecrets = append(userSecrets, secret)
	}

	return &ImportResult{
		Secrets:         userSecrets,
		SkippedInternal: skippedInternal,
		Version:         importData.Version,
		VersionMismatch: importData.Version != secretcrypto.ExportVersion,
	}, nil
}

// ExportOptions configures secret export behavior.
type ExportOptions struct {
	// Format is the output format: "json" or "yaml" (default: "yaml").
	Format string
	// Encrypt enables password-based encryption of the output.
	Encrypt bool
	// Password for encryption. Required when Encrypt is true.
	Password string `json:"-"` // nosec G117 -- not serialized, used only as function parameter
}

// Export serializes a secret collection to bytes in the specified format,
// optionally encrypting the output.
func Export(exportData *secretcrypto.ExportFormat, opts ExportOptions) ([]byte, error) {
	format := opts.Format
	if format == "" {
		format = "yaml"
	}

	var exportBytes []byte
	var err error

	switch format {
	case "json":
		exportBytes, err = json.Marshal(exportData)
		if err != nil {
			return nil, fmt.Errorf("failed to encode as JSON: %w", err)
		}
	case "yaml":
		exportBytes, err = yaml.Marshal(exportData)
		if err != nil {
			return nil, fmt.Errorf("failed to encode as YAML: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported format: %s (use yaml or json)", format)
	}

	if opts.Encrypt {
		if opts.Password == "" {
			return nil, fmt.Errorf("encryption requested but no password provided")
		}
		exportBytes, err = secretcrypto.Encrypt(exportBytes, opts.Password)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt export: %w", err)
		}
	}

	return exportBytes, nil
}

// parseSecretData tries JSON first, then YAML to parse secret export data.
func parseSecretData(data []byte, out *secretcrypto.ExportFormat) error {
	if err := json.Unmarshal(data, out); err != nil {
		if err := yaml.Unmarshal(data, out); err != nil {
			return err
		}
	}
	return nil
}
