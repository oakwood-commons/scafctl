package secrets

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/adrg/xdg"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetSecretsDir(t *testing.T) {
	t.Run("uses environment variable override", func(t *testing.T) {
		customDir := "/custom/secrets/dir"
		t.Setenv(secretsDirEnvVar, customDir)

		dir, err := getSecretsDir()
		require.NoError(t, err)
		assert.Equal(t, customDir, dir)
	})

	t.Run("returns XDG-compliant path when no env var", func(t *testing.T) {
		// Ensure env var is not set
		t.Setenv(secretsDirEnvVar, "")

		dir, err := getSecretsDir()
		require.NoError(t, err)

		// Check that it contains expected path components
		assert.Contains(t, dir, "scafctl")
		assert.Contains(t, dir, paths.SecretsDirName)

		// Platform-specific checks
		switch runtime.GOOS {
		case "darwin":
			assert.Contains(t, dir, "Library/Application Support")
		case "linux":
			home, _ := os.UserHomeDir()
			xdgData := os.Getenv("XDG_DATA_HOME")
			if xdgData != "" {
				assert.True(t, strings.HasPrefix(dir, xdgData))
			} else {
				assert.True(t, strings.HasPrefix(dir, filepath.Join(home, ".local", "share")))
			}
		case "windows":
			assert.Contains(t, dir, "scafctl")
		}
	})

	t.Run("respects XDG_DATA_HOME on Linux", func(t *testing.T) {
		if runtime.GOOS != "linux" {
			t.Skip("XDG_DATA_HOME test only relevant on Linux")
		}

		customData := t.TempDir()
		t.Setenv("XDG_DATA_HOME", customData)
		t.Setenv(secretsDirEnvVar, "") // Ensure override is not set
		xdg.Reload()
		defer xdg.Reload()

		dir, err := getSecretsDir()
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(customData, "scafctl", paths.SecretsDirName), dir)
	})
}

func TestEnsureSecretsDir(t *testing.T) {
	t.Run("creates directory if not exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		secretsDir := filepath.Join(tmpDir, "secrets")

		err := ensureSecretsDir(secretsDir)
		require.NoError(t, err)

		// Verify directory exists
		info, err := os.Stat(secretsDir)
		require.NoError(t, err)
		assert.True(t, info.IsDir())

		// Check permissions on Unix
		if runtime.GOOS != "windows" {
			assert.Equal(t, os.FileMode(dirPermissions), info.Mode().Perm())
		}
	})

	t.Run("creates nested directories", func(t *testing.T) {
		tmpDir := t.TempDir()
		secretsDir := filepath.Join(tmpDir, "deep", "nested", "secrets")

		err := ensureSecretsDir(secretsDir)
		require.NoError(t, err)

		info, err := os.Stat(secretsDir)
		require.NoError(t, err)
		assert.True(t, info.IsDir())
	})

	t.Run("succeeds if directory already exists with correct permissions", func(t *testing.T) {
		tmpDir := t.TempDir()
		secretsDir := filepath.Join(tmpDir, "secrets")

		// Create directory first
		err := os.MkdirAll(secretsDir, dirPermissions)
		require.NoError(t, err)

		// Should not error
		err = ensureSecretsDir(secretsDir)
		require.NoError(t, err)
	})

	t.Run("fixes incorrect permissions on Unix", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Permission test not relevant on Windows")
		}

		tmpDir := t.TempDir()
		secretsDir := filepath.Join(tmpDir, "secrets")

		// Create with wrong permissions
		err := os.MkdirAll(secretsDir, 0o755)
		require.NoError(t, err)

		// Should fix permissions
		err = ensureSecretsDir(secretsDir)
		require.NoError(t, err)

		info, err := os.Stat(secretsDir)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(dirPermissions), info.Mode().Perm())
	})

	t.Run("errors if path exists but is a file", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "secrets")

		// Create a file instead of directory
		err := os.WriteFile(filePath, []byte("not a directory"), 0o600)
		require.NoError(t, err)

		err = ensureSecretsDir(filePath)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not a directory")
	})
}

func TestSecretFilePath(t *testing.T) {
	t.Run("returns correct path with extension", func(t *testing.T) {
		dir := "/path/to/secrets"
		name := "my-secret"

		path := secretFilePath(dir, name)
		assert.Equal(t, "/path/to/secrets/my-secret.enc", path)
	})

	t.Run("handles special characters in name", func(t *testing.T) {
		dir := "/path/to/secrets"
		name := "my.secret_v2"

		path := secretFilePath(dir, name)
		assert.Equal(t, "/path/to/secrets/my.secret_v2.enc", path)
	})
}

func TestWriteSecret(t *testing.T) {
	t.Run("writes data to file", func(t *testing.T) {
		tmpDir := t.TempDir()
		data := []byte("encrypted secret data")

		err := writeSecret(tmpDir, "test-secret", data)
		require.NoError(t, err)

		// Verify file exists and contains data
		path := secretFilePath(tmpDir, "test-secret")
		readData, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, data, readData)
	})

	t.Run("sets correct file permissions on Unix", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Permission test not relevant on Windows")
		}

		tmpDir := t.TempDir()
		data := []byte("encrypted secret data")

		err := writeSecret(tmpDir, "test-secret", data)
		require.NoError(t, err)

		path := secretFilePath(tmpDir, "test-secret")
		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(filePermissions), info.Mode().Perm())
	})

	t.Run("overwrites existing file", func(t *testing.T) {
		tmpDir := t.TempDir()
		originalData := []byte("original data")
		newData := []byte("new data")

		// Write original
		err := writeSecret(tmpDir, "test-secret", originalData)
		require.NoError(t, err)

		// Overwrite
		err = writeSecret(tmpDir, "test-secret", newData)
		require.NoError(t, err)

		// Verify new data
		path := secretFilePath(tmpDir, "test-secret")
		readData, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, newData, readData)
	})

	t.Run("handles large data", func(t *testing.T) {
		tmpDir := t.TempDir()
		// Create 1MB of data
		data := make([]byte, 1024*1024)
		for i := range data {
			data[i] = byte(i % 256)
		}

		err := writeSecret(tmpDir, "large-secret", data)
		require.NoError(t, err)

		// Verify data
		path := secretFilePath(tmpDir, "large-secret")
		readData, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, data, readData)
	})

	t.Run("cleans up temp file on write error", func(t *testing.T) {
		// Use a non-existent directory to cause error
		nonExistentDir := "/non/existent/path"

		err := writeSecret(nonExistentDir, "test-secret", []byte("data"))
		assert.Error(t, err)

		// No temp files should be left behind (we can't really verify this
		// since the dir doesn't exist, but the error path is tested)
	})
}

func TestReadSecret(t *testing.T) {
	t.Run("reads existing secret", func(t *testing.T) {
		tmpDir := t.TempDir()
		expectedData := []byte("secret data")

		// Write directly
		path := secretFilePath(tmpDir, "test-secret")
		err := os.WriteFile(path, expectedData, 0o600)
		require.NoError(t, err)

		data, err := readSecret(tmpDir, "test-secret")
		require.NoError(t, err)
		assert.Equal(t, expectedData, data)
	})

	t.Run("returns ErrNotFound for missing secret", func(t *testing.T) {
		tmpDir := t.TempDir()

		_, err := readSecret(tmpDir, "non-existent")
		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("returns ErrNotFound for missing directory", func(t *testing.T) {
		_, err := readSecret("/non/existent/path", "test-secret")
		assert.ErrorIs(t, err, ErrNotFound)
	})
}

func TestDeleteSecret(t *testing.T) {
	t.Run("deletes existing secret", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create secret
		path := secretFilePath(tmpDir, "test-secret")
		err := os.WriteFile(path, []byte("data"), 0o600)
		require.NoError(t, err)

		// Delete
		err = deleteSecret(tmpDir, "test-secret")
		require.NoError(t, err)

		// Verify deleted
		_, err = os.Stat(path)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("succeeds for non-existent secret", func(t *testing.T) {
		tmpDir := t.TempDir()

		err := deleteSecret(tmpDir, "non-existent")
		assert.NoError(t, err)
	})

	t.Run("succeeds for non-existent directory", func(t *testing.T) {
		err := deleteSecret("/non/existent/path", "test-secret")
		assert.NoError(t, err)
	})
}

func TestListSecrets(t *testing.T) {
	t.Run("lists all secrets", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create some secrets
		secrets := []string{"secret1", "secret2", "my-api-key"}
		for _, name := range secrets {
			path := secretFilePath(tmpDir, name)
			err := os.WriteFile(path, []byte("data"), 0o600)
			require.NoError(t, err)
		}

		// List
		names, err := listSecrets(tmpDir)
		require.NoError(t, err)

		sort.Strings(names)
		sort.Strings(secrets)
		assert.Equal(t, secrets, names)
	})

	t.Run("returns empty list for empty directory", func(t *testing.T) {
		tmpDir := t.TempDir()

		names, err := listSecrets(tmpDir)
		require.NoError(t, err)
		assert.Empty(t, names)
	})

	t.Run("returns empty list for non-existent directory", func(t *testing.T) {
		names, err := listSecrets("/non/existent/path")
		require.NoError(t, err)
		assert.Empty(t, names)
	})

	t.Run("ignores non-.enc files", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create a .enc file
		err := os.WriteFile(filepath.Join(tmpDir, "valid.enc"), []byte("data"), 0o600)
		require.NoError(t, err)

		// Create non-.enc files
		err = os.WriteFile(filepath.Join(tmpDir, "invalid.txt"), []byte("data"), 0o600)
		require.NoError(t, err)
		err = os.WriteFile(filepath.Join(tmpDir, "readme"), []byte("data"), 0o600)
		require.NoError(t, err)

		names, err := listSecrets(tmpDir)
		require.NoError(t, err)
		assert.Equal(t, []string{"valid"}, names)
	})

	t.Run("ignores directories", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create a subdirectory with .enc extension (unusual but possible)
		err := os.MkdirAll(filepath.Join(tmpDir, "subdir.enc"), 0o700)
		require.NoError(t, err)

		// Create actual secret
		err = os.WriteFile(filepath.Join(tmpDir, "real-secret.enc"), []byte("data"), 0o600)
		require.NoError(t, err)

		names, err := listSecrets(tmpDir)
		require.NoError(t, err)
		assert.Equal(t, []string{"real-secret"}, names)
	})

	t.Run("ignores temp files", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create real secret
		err := os.WriteFile(filepath.Join(tmpDir, "real-secret.enc"), []byte("data"), 0o600)
		require.NoError(t, err)

		// Create temp file (shouldn't happen normally, but could if crash during write)
		err = os.WriteFile(filepath.Join(tmpDir, ".secret-abc123.enc"), []byte("data"), 0o600)
		require.NoError(t, err)

		names, err := listSecrets(tmpDir)
		require.NoError(t, err)
		assert.Equal(t, []string{"real-secret"}, names)
	})
}

func TestSecretExists(t *testing.T) {
	t.Run("returns true for existing secret", func(t *testing.T) {
		tmpDir := t.TempDir()

		path := secretFilePath(tmpDir, "test-secret")
		err := os.WriteFile(path, []byte("data"), 0o600)
		require.NoError(t, err)

		exists, err := secretExists(tmpDir, "test-secret")
		require.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run("returns false for non-existent secret", func(t *testing.T) {
		tmpDir := t.TempDir()

		exists, err := secretExists(tmpDir, "non-existent")
		require.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("returns false for non-existent directory", func(t *testing.T) {
		exists, err := secretExists("/non/existent/path", "test-secret")
		require.NoError(t, err)
		assert.False(t, exists)
	})
}

func TestDeleteAllSecrets(t *testing.T) {
	t.Run("deletes all secrets", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create some secrets
		secrets := []string{"secret1", "secret2", "secret3"}
		for _, name := range secrets {
			path := secretFilePath(tmpDir, name)
			err := os.WriteFile(path, []byte("data"), 0o600)
			require.NoError(t, err)
		}

		// Delete all
		err := deleteAllSecrets(tmpDir)
		require.NoError(t, err)

		// Verify all deleted
		names, err := listSecrets(tmpDir)
		require.NoError(t, err)
		assert.Empty(t, names)
	})

	t.Run("succeeds for empty directory", func(t *testing.T) {
		tmpDir := t.TempDir()

		err := deleteAllSecrets(tmpDir)
		assert.NoError(t, err)
	})

	t.Run("succeeds for non-existent directory", func(t *testing.T) {
		err := deleteAllSecrets("/non/existent/path")
		assert.NoError(t, err)
	})

	t.Run("only deletes .enc files", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create .enc file
		err := os.WriteFile(filepath.Join(tmpDir, "secret.enc"), []byte("data"), 0o600)
		require.NoError(t, err)

		// Create non-.enc file
		otherFile := filepath.Join(tmpDir, "other.txt")
		err = os.WriteFile(otherFile, []byte("data"), 0o600)
		require.NoError(t, err)

		// Delete all secrets
		err = deleteAllSecrets(tmpDir)
		require.NoError(t, err)

		// Other file should still exist
		_, err = os.Stat(otherFile)
		assert.NoError(t, err)
	})
}

func TestGenerateTempFileName(t *testing.T) {
	t.Run("generates unique names", func(t *testing.T) {
		names := make(map[string]bool)

		for i := 0; i < 100; i++ {
			name, err := generateTempFileName()
			require.NoError(t, err)
			assert.False(t, names[name], "duplicate name generated")
			names[name] = true
		}
	})

	t.Run("generates names with correct prefix", func(t *testing.T) {
		name, err := generateTempFileName()
		require.NoError(t, err)
		assert.True(t, len(name) > len(".secret-"))
		assert.Equal(t, ".secret-", name[:8])
	})
}

func TestWriteReadRoundtrip(t *testing.T) {
	t.Run("write then read returns same data", func(t *testing.T) {
		tmpDir := t.TempDir()
		originalData := []byte("this is my secret data with special chars: 日本語 🎉")

		err := writeSecret(tmpDir, "roundtrip-secret", originalData)
		require.NoError(t, err)

		readData, err := readSecret(tmpDir, "roundtrip-secret")
		require.NoError(t, err)
		assert.Equal(t, originalData, readData)
	})
}
