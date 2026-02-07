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

func TestGitProvider_InjectCredentials_HTTPS(t *testing.T) {
	tests := []struct {
		name     string
		repoURL  string
		username string
		password string
		expected string
	}{
		{
			name:     "https URL",
			repoURL:  "https://github.com/user/repo.git",
			username: "testuser",
			password: "testpass",
			expected: "https://testuser:testpass@github.com/user/repo.git",
		},
		{
			name:     "http URL",
			repoURL:  "http://github.com/user/repo.git",
			username: "testuser",
			password: "testpass",
			expected: "http://testuser:testpass@github.com/user/repo.git",
		},
		{
			name:     "ssh URL unchanged",
			repoURL:  "git@github.com:user/repo.git",
			username: "testuser",
			password: "testpass",
			expected: "git@github.com:user/repo.git",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := injectCredentials(tt.repoURL, tt.username, tt.password)
			assert.Equal(t, tt.expected, result)
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
