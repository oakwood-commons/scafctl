// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"os"
	"path/filepath"
	"testing"

	authpkg "github.com/oakwood-commons/scafctl/pkg/auth"
	appconfig "github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandSwitch_NoArgs(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)

	cmd := CommandSwitch(cliParams, ioStreams, "scafctl/auth")
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts")
}

func TestCommandSwitch_UnknownHandler(t *testing.T) {
	ctx, _ := newTestContext(t)
	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)

	cmd := CommandSwitch(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"unknown", "work"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown auth handler")
}

func TestCommandSwitch_InvalidProfileName(t *testing.T) {
	ctx, _ := newTestContext(t)

	registry := authpkg.NewRegistry()
	mock := authpkg.NewMockHandler("github")
	require.NoError(t, registry.Register(mock))
	ctx = authpkg.WithRegistry(ctx, registry)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)

	cmd := CommandSwitch(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"github", "has spaces"})

	err := cmd.Execute()
	require.Error(t, err)
}

func TestCommandSwitch_ListProfiles(t *testing.T) {
	ctx, buf := newTestContext(t)

	registry := authpkg.NewRegistry()
	mock := authpkg.NewMockHandler("github")
	require.NoError(t, registry.Register(mock))
	ctx = authpkg.WithRegistry(ctx, registry)
	ctx = appconfig.WithConfig(ctx, &appconfig.Config{
		Auth: appconfig.GlobalAuthConfig{
			GitHub: &appconfig.GitHubAuthConfig{
				ActiveProfile: "work",
				Profiles: map[string]*appconfig.GitHubProfileConfig{
					"work":     {},
					"personal": {},
				},
			},
		},
	})

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandSwitch(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"github"})

	err := cmd.Execute()
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Profiles for github")
	assert.Contains(t, output, "work")
	assert.Contains(t, output, "personal")
}

func TestCommandSwitch_SetProfile(t *testing.T) {
	// Create a temp config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("auth:\n  github:\n    clientId: test\n"), 0o600))

	ctx, buf := newTestContext(t)

	registry := authpkg.NewRegistry()
	mock := authpkg.NewMockHandler("github")
	require.NoError(t, registry.Register(mock))
	ctx = authpkg.WithRegistry(ctx, registry)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	// Use a root command with --config flag to simulate real usage
	cmd := CommandSwitch(cliParams, ioStreams, "scafctl/auth")
	root := cmd.Root()
	root.PersistentFlags().String("config", configPath, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"github", "work"})

	err := cmd.Execute()
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Switched github to profile")
	assert.Contains(t, output, "work")

	// Verify config was written with activeProfile set
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "activeProfile: work")
}

func TestCommandSwitch_ResetToDefault(t *testing.T) {
	// Create a temp config file with active profile set
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("auth:\n  github:\n    activeProfile: work\n"), 0o600))

	ctx, buf := newTestContext(t)

	registry := authpkg.NewRegistry()
	mock := authpkg.NewMockHandler("github")
	require.NoError(t, registry.Register(mock))
	ctx = authpkg.WithRegistry(ctx, registry)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandSwitch(cliParams, ioStreams, "scafctl/auth")
	root := cmd.Root()
	root.PersistentFlags().String("config", configPath, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"github", "default"})

	err := cmd.Execute()
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "built-in profile")
}
