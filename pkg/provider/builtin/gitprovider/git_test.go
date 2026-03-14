// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gitprovider

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGitProvider(t *testing.T) {
	p := NewGitProvider()

	require.NotNil(t, p)
	require.NotNil(t, p.Descriptor())

	desc := p.Descriptor()
	assert.Equal(t, "git", desc.Name)
	assert.Equal(t, "Git Provider", desc.DisplayName)
	assert.Equal(t, "v1", desc.APIVersion)
	assert.NotNil(t, desc.Version)
	assert.Contains(t, desc.Capabilities, provider.CapabilityAction)
	assert.Contains(t, desc.Capabilities, provider.CapabilityFrom)

	assert.NotNil(t, desc.Schema)
	assert.NotNil(t, desc.Schema.Properties)
	assert.Contains(t, desc.Schema.Required, "operation")
	assert.Equal(t, "string", desc.Schema.Properties["operation"].Type)
	assert.NotEmpty(t, desc.Schema.Properties["operation"].Enum)

	assert.NotNil(t, desc.OutputSchemas[provider.CapabilityAction])
	assert.NotNil(t, desc.OutputSchemas[provider.CapabilityAction].Properties)
	assert.Equal(t, "boolean", desc.OutputSchemas[provider.CapabilityAction].Properties["success"].Type)
	assert.Equal(t, "string", desc.OutputSchemas[provider.CapabilityAction].Properties["output"].Type)
	assert.Equal(t, "string", desc.OutputSchemas[provider.CapabilityAction].Properties["error"].Type)
}

func setupTestRepo(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	repoPath := filepath.Join(tmpDir, "test-repo")

	err := os.MkdirAll(repoPath, 0o755)
	require.NoError(t, err)

	gitDir := filepath.Join(repoPath, ".git")
	err = os.MkdirAll(gitDir, 0o755)
	require.NoError(t, err)

	return repoPath
}

func TestGitProvider_Execute_Status(t *testing.T) {
	if _, err := os.Stat("/usr/bin/git"); os.IsNotExist(err) {
		if _, err := os.Stat("/usr/local/bin/git"); os.IsNotExist(err) {
			t.Skip("git command not available")
		}
	}

	repoPath := setupTestRepo(t)

	p := NewGitProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "status",
		"path":      repoPath,
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "status", data["operation"])
	assert.Equal(t, repoPath, data["path"])
	assert.NotNil(t, data["success"])
}

func TestGitProvider_Execute_MissingOperation(t *testing.T) {
	p := NewGitProvider()
	ctx := context.Background()

	inputs := map[string]any{}

	output, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "operation is required")
}

func TestGitProvider_Execute_EmptyOperation(t *testing.T) {
	p := NewGitProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "",
	}

	output, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "operation is required")
}

func TestGitProvider_Execute_UnsupportedOperation(t *testing.T) {
	p := NewGitProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "invalid-operation",
		"path":      "/tmp/test",
	}

	output, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "unsupported operation")
}

func TestGitProvider_Execute_Clone_MissingRepository(t *testing.T) {
	p := NewGitProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "clone",
		"path":      "/tmp/test",
	}

	output, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "repository URL is required")
}

func TestGitProvider_Execute_Clone_MissingPath(t *testing.T) {
	p := NewGitProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"operation":  "clone",
		"repository": "https://github.com/user/repo.git",
	}

	output, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "path is required")
}

func TestGitProvider_Execute_Status_MissingPath(t *testing.T) {
	p := NewGitProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "status",
	}

	output, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "path is required")
}

func TestGitProvider_Execute_Add_MissingFiles(t *testing.T) {
	p := NewGitProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "add",
		"path":      "/tmp/test",
	}

	output, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "files is required")
}

func TestGitProvider_Execute_Add_InvalidFiles(t *testing.T) {
	p := NewGitProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "add",
		"path":      "/tmp/test",
		"files":     "not-an-array",
	}

	output, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "files must be an array")
}

func TestGitProvider_Execute_Commit_MissingMessage(t *testing.T) {
	p := NewGitProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "commit",
		"path":      "/tmp/test",
	}

	output, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "message is required")
}

func TestGitProvider_Execute_Checkout_MissingBranch(t *testing.T) {
	p := NewGitProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "checkout",
		"path":      "/tmp/test",
	}

	output, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "branch is required")
}

func TestGitProvider_Execute_InvalidDirectory(t *testing.T) {
	p := NewGitProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "status",
		"path":      "/nonexistent/path/that/does/not/exist",
	}

	output, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "directory does not exist")
}

func TestGitProvider_Execute_DryRun(t *testing.T) {
	p := NewGitProvider()
	ctx := provider.WithDryRun(context.Background(), true)

	inputs := map[string]any{
		"operation":  "clone",
		"repository": "https://github.com/user/repo.git",
		"path":       "/tmp/test-repo",
		"branch":     "main",
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, data["success"])
	assert.Equal(t, "clone", data["operation"])
	assert.Equal(t, true, data["_dryRun"])
	assert.Contains(t, data["_message"], "Would execute git clone")
	assert.Contains(t, data["_message"], "https://github.com/user/repo.git")
}

func TestGitProvider_Execute_DryRun_Status(t *testing.T) {
	p := NewGitProvider()
	ctx := provider.WithDryRun(context.Background(), true)

	inputs := map[string]any{
		"operation": "status",
		"path":      "/tmp/test-repo",
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, data["success"])
	assert.Equal(t, "status", data["operation"])
	assert.Equal(t, true, data["_dryRun"])
}

func TestGitProvider_CreateNetrcCredentials(t *testing.T) {
	tests := []struct {
		name       string
		repoURL    string
		username   string
		password   string
		wantNilEnv bool // true for non-HTTP(S) URLs
	}{
		{
			name:     "https URL creates netrc",
			repoURL:  "https://github.com/user/repo.git",
			username: "testuser",
			password: "testpass",
		},
		{
			name:     "http URL creates netrc",
			repoURL:  "http://internal.example.com/repo.git",
			username: "testuser",
			password: "testpass",
		},
		{
			name:       "ssh URL returns nil env",
			repoURL:    "git@github.com:user/repo.git",
			username:   "testuser",
			password:   "testpass",
			wantNilEnv: true,
		},
		{
			name:     "special characters in credentials written as-is",
			repoURL:  "https://github.com/org/repo",
			username: "user@corp",
			password: "p@ss:w/rd%100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, cleanup, err := createNetrcCredentials(tt.repoURL, tt.username, tt.password)
			require.NoError(t, err)
			require.NotNil(t, cleanup)
			defer cleanup()

			if tt.wantNilEnv {
				assert.Nil(t, env)
				return
			}

			// Verify env is non-nil and HOME is overridden to the temp dir.
			require.NotNil(t, env)
			var homeDir string
			for _, e := range env {
				if len(e) > 5 && e[:5] == "HOME=" {
					homeDir = e[5:]
					break
				}
			}
			require.NotEmpty(t, homeDir, "HOME must be set in credential env")

			// Verify .netrc exists inside the temp HOME dir with correct permissions.
			netrcPath := filepath.Join(homeDir, ".netrc")
			info, err := os.Stat(netrcPath)
			require.NoError(t, err, ".netrc file should exist")
			assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), ".netrc should be mode 0600")

			// Verify .netrc content contains machine, login, and password lines.
			content, err := os.ReadFile(netrcPath)
			require.NoError(t, err)
			contentStr := string(content)
			assert.Contains(t, contentStr, "login "+tt.username)
			assert.Contains(t, contentStr, "password "+tt.password)
		})
	}
}

func TestGitProvider_CreateNetrcCredentials_EmptyHostname(t *testing.T) {
	_, _, err := createNetrcCredentials("https:///path", "user", "pass")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no hostname")
}

func TestGitProvider_CreateNetrcCredentials_WhitespaceRejection(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
		wantErr  string
	}{
		{"space in username", "user name", "pass", "username contains whitespace"},
		{"tab in password", "user", "pass\tword", "password contains whitespace"},
		{"newline in password", "user", "pass\nword", "password contains whitespace"},
		{"newline in username", "user\nname", "pass", "username contains whitespace"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := createNetrcCredentials("https://github.com/org/repo.git", tt.username, tt.password)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestGitProvider_Execute_Pull_DefaultRemote(t *testing.T) {
	p := NewGitProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "pull",
		"path":      "/nonexistent",
	}

	output, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "directory does not exist")
}

func TestGitProvider_Execute_Branch_List(t *testing.T) {
	p := NewGitProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "branch",
		"path":      "/nonexistent",
	}

	output, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, output)
}

func TestGitProvider_Execute_Tag_List(t *testing.T) {
	p := NewGitProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "tag",
		"path":      "/nonexistent",
	}

	output, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, output)
}

func TestApplyEnvOverrides(t *testing.T) {
	t.Run("basic override", func(t *testing.T) {
		base := []string{"HOME=/original", "PATH=/usr/bin"}
		overrides := map[string]string{"HOME": "/tmp/fake"}
		result := applyEnvOverrides(base, overrides)
		assert.Contains(t, result, "HOME=/tmp/fake")
		assert.Contains(t, result, "PATH=/usr/bin")
		assert.NotContains(t, result, "HOME=/original")
	})

	t.Run("case-insensitive key matching", func(t *testing.T) {
		// Simulates Windows where env may have mixed-case keys.
		base := []string{"UserProfile=C:\\Users\\me", "Path=C:\\Windows"}
		overrides := map[string]string{"USERPROFILE": "C:\\tmp"}
		result := applyEnvOverrides(base, overrides)
		assert.Contains(t, result, "USERPROFILE=C:\\tmp")
		assert.NotContains(t, result, "UserProfile=C:\\Users\\me")
		assert.Contains(t, result, "Path=C:\\Windows")
	})

	t.Run("no override leaves base intact", func(t *testing.T) {
		base := []string{"A=1", "B=2"}
		result := applyEnvOverrides(base, map[string]string{})
		assert.ElementsMatch(t, base, result)
	})

	t.Run("adds new keys not in base", func(t *testing.T) {
		base := []string{"A=1"}
		overrides := map[string]string{"NEW_VAR": "val"}
		result := applyEnvOverrides(base, overrides)
		assert.Contains(t, result, "A=1")
		assert.Contains(t, result, "NEW_VAR=val")
	})
}

func BenchmarkApplyEnvOverrides(b *testing.B) {
	base := []string{"HOME=/home/user", "PATH=/usr/bin", "UserProfile=C:\\Users\\user", "SHELL=/bin/zsh"}
	overrides := map[string]string{"HOME": "/tmp/fake", "USERPROFILE": "C:\\tmp"}
	b.ResetTimer()
	for b.Loop() {
		_ = applyEnvOverrides(base, overrides)
	}
}

func BenchmarkCreateNetrcCredentials(b *testing.B) {
	benchmarks := []struct {
		name     string
		repoURL  string
		username string
		password string
	}{
		{"basic", "https://github.com/org/repo.git", "user", "pass"},
		{"special_chars", "https://github.com/org/repo.git", "user@corp", "p@ss:w/rd%100"},
		{"ssh_noop", "git@github.com:user/repo.git", "user", "pass"},
	}

	for _, bb := range benchmarks {
		b.Run(bb.name, func(b *testing.B) {
			for b.Loop() {
				_, cleanup, _ := createNetrcCredentials(bb.repoURL, bb.username, bb.password)
				if cleanup != nil {
					cleanup()
				}
			}
		})
	}
}
