// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"os"
	"path/filepath"
	"testing"

	authpkg "github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandProfileDelete_NoArgs(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)

	cmd := CommandProfileDelete(cliParams, ioStreams, "scafctl/auth/profile")
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts 2 arg(s)")
}

func TestCommandProfileDelete_UnknownHandler(t *testing.T) {
	ctx, _ := newTestContext(t)
	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)

	cmd := CommandProfileDelete(cliParams, ioStreams, "scafctl/auth/profile")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"unknown", "work"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown auth handler")
}

func TestCommandProfileDelete_CannotDeleteDefault(t *testing.T) {
	ctx, _ := newTestContext(t)

	registry := authpkg.NewRegistry()
	mock := authpkg.NewMockHandler("github")
	require.NoError(t, registry.Register(mock))
	ctx = authpkg.WithRegistry(ctx, registry)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)

	cmd := CommandProfileDelete(cliParams, ioStreams, "scafctl/auth/profile")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"github", "default"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot delete the unnamed built-in profile")
}

func TestCommandProfileDelete_ProfileNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("auth:\n  github:\n    profiles:\n      work: {}\n"), 0o600))

	ctx, buf := newTestContext(t)

	registry := authpkg.NewRegistry()
	mock := authpkg.NewMockHandler("github")
	require.NoError(t, registry.Register(mock))
	ctx = authpkg.WithRegistry(ctx, registry)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandProfileDelete(cliParams, ioStreams, "scafctl/auth/profile")
	root := cmd.Root()
	root.PersistentFlags().String("config", configPath, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"github", "nonexistent"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestCommandProfileDelete_Success(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("auth:\n  github:\n    profiles:\n      work: {}\n      personal: {}\n"), 0o600))

	ctx, buf := newTestContext(t)

	registry := authpkg.NewRegistry()
	mock := authpkg.NewMockHandler("github")
	require.NoError(t, registry.Register(mock))
	ctx = authpkg.WithRegistry(ctx, registry)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandProfileDelete(cliParams, ioStreams, "scafctl/auth/profile")
	root := cmd.Root()
	root.PersistentFlags().String("config", configPath, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"github", "work"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Deleted profile")
	assert.Contains(t, output, "work")

	// Verify config was written without the deleted profile
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "work")
	assert.Contains(t, string(data), "personal")
}

func TestCommandProfileDelete_ResetsActiveProfile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("auth:\n  github:\n    activeProfile: staging\n    profiles:\n      staging: {}\n      work: {}\n"), 0o600))

	ctx, buf := newTestContext(t)

	registry := authpkg.NewRegistry()
	mock := authpkg.NewMockHandler("github")
	require.NoError(t, registry.Register(mock))
	ctx = authpkg.WithRegistry(ctx, registry)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandProfileDelete(cliParams, ioStreams, "scafctl/auth/profile")
	root := cmd.Root()
	root.PersistentFlags().String("config", configPath, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"github", "staging"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Verify activeProfile was reset
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "activeProfile: staging")
	assert.NotContains(t, string(data), "staging")
	assert.Contains(t, string(data), "work")
}

func TestCommandProfileDelete_EmbedderBinaryName(t *testing.T) {
	cliParams := &settings.Run{BinaryName: "mycli"}
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)

	cmd := CommandProfileDelete(cliParams, ioStreams, "mycli/auth/profile")
	assert.Equal(t, "delete <handler> <profile>", cmd.Use)
	assert.NotContains(t, cmd.Long, "scafctl")
}

func TestCommandProfile_SubcommandRegistered(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)

	cmd := CommandProfile(cliParams, ioStreams, "scafctl/auth")
	assert.Equal(t, "profile", cmd.Use)

	// Verify delete subcommand is registered
	deleteCmd, _, err := cmd.Find([]string{"delete"})
	require.NoError(t, err)
	assert.Equal(t, "delete <handler> <profile>", deleteCmd.Use)
}
