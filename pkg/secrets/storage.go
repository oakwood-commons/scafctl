// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/paths"
)

const (
	// secretFileExtension is the extension for encrypted secret files.
	secretFileExtension = ".enc"

	// dirPermissions is the permission mode for the secrets directory (rwx------).
	dirPermissions = 0o700

	// filePermissions is the permission mode for secret files (rw-------).
	filePermissions = 0o600

	// secretsDirEnvVar is the environment variable to override the secrets directory.
	secretsDirEnvVar = "SCAFCTL_SECRETS_DIR" //nolint:gosec // This is not a credential
)

// getSecretsDir returns the secrets directory path.
// The directory is determined in the following order:
//  1. SCAFCTL_SECRETS_DIR environment variable (if set)
//  2. XDG-compliant path via paths.SecretsDir():
//     - Linux: ~/.local/share/scafctl/secrets/
//     - macOS: ~/.local/share/scafctl/secrets/
//     - Windows: %LOCALAPPDATA%\scafctl\secrets\
func getSecretsDir() (string, error) {
	// Check environment variable override first
	if envDir := os.Getenv(secretsDirEnvVar); envDir != "" {
		return envDir, nil
	}

	// Use XDG-compliant path
	return paths.SecretsDir()
}

// ensureSecretsDir creates the secrets directory if it doesn't exist
// and validates/fixes permissions if it does.
func ensureSecretsDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Create directory with proper permissions
			if err := os.MkdirAll(dir, dirPermissions); err != nil {
				return fmt.Errorf("creating secrets directory: %w", err)
			}
			return nil
		}
		return fmt.Errorf("checking secrets directory: %w", err)
	}

	// Directory exists, check if it's actually a directory
	if !info.IsDir() {
		return fmt.Errorf("secrets path exists but is not a directory: %s", dir)
	}

	// Check and fix permissions on Unix-like systems
	if runtime.GOOS != "windows" {
		mode := info.Mode().Perm()
		if mode != dirPermissions {
			if err := os.Chmod(dir, dirPermissions); err != nil {
				return fmt.Errorf("fixing secrets directory permissions: %w", err)
			}
		}
	}

	return nil
}

// secretFilePath returns the full path for a secret file.
func secretFilePath(dir, name string) string {
	return filepath.Join(dir, name+secretFileExtension)
}

// writeSecret writes encrypted data to a secret file using atomic write.
// It writes to a temp file first, then renames to ensure atomicity.
func writeSecret(dir, name string, data []byte) error {
	targetPath := secretFilePath(dir, name)

	// Create temp file in the same directory (for atomic rename)
	tempFile, err := os.CreateTemp(dir, ".secret-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tempPath := tempFile.Name()

	// Ensure cleanup on error
	success := false
	defer func() {
		if !success {
			_ = os.Remove(tempPath)
		}
	}()

	// Write data
	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("writing to temp file: %w", err)
	}

	// Close before chmod/rename
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}

	// Set proper permissions
	if err := os.Chmod(tempPath, filePermissions); err != nil {
		return fmt.Errorf("setting file permissions: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempPath, targetPath); err != nil {
		return fmt.Errorf("renaming temp file: %w", err)
	}

	success = true
	return nil
}

// readSecret reads encrypted data from a secret file.
func readSecret(dir, name string) ([]byte, error) {
	path := secretFilePath(dir, name)

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("reading secret file: %w", err)
	}

	return data, nil
}

// deleteSecret removes a secret file.
// It returns nil if the file doesn't exist.
func deleteSecret(dir, name string) error {
	path := secretFilePath(dir, name)

	err := os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("deleting secret file: %w", err)
	}

	return nil
}

// listSecrets returns the names of all secrets in the directory.
func listSecrets(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("reading secrets directory: %w", err)
	}

	var names []string
	for _, entry := range entries {
		// Skip directories and non-.enc files
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, secretFileExtension) {
			continue
		}

		// Remove extension to get secret name
		secretName := strings.TrimSuffix(name, secretFileExtension)

		// Skip temp files (they start with .secret-)
		if strings.HasPrefix(secretName, ".secret-") {
			continue
		}

		names = append(names, secretName)
	}

	return names, nil
}

// secretExists checks if a secret file exists.
func secretExists(dir, name string) (bool, error) {
	path := secretFilePath(dir, name)

	_, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("checking secret file: %w", err)
	}

	return true, nil
}

// deleteAllSecrets removes all secret files in the directory.
// This is used when the master key is lost and secrets can no longer be decrypted.
func deleteAllSecrets(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("reading secrets directory: %w", err)
	}

	var errs []error
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, secretFileExtension) {
			continue
		}

		path := filepath.Join(dir, name)
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("deleting %s: %w", name, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to delete some secrets: %v", errs)
	}

	return nil
}

// generateTempFileName creates a random temporary file name.
// This is exported for testing purposes.
func generateTempFileName() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generating random bytes: %w", err)
	}
	return ".secret-" + hex.EncodeToString(bytes), nil
}
