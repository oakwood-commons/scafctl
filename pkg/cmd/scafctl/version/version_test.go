// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package version

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionCmdOptions_PrintVersion(t *testing.T) {
	tests := []struct {
		name             string
		buildVersion     string
		commit           string
		buildTime        string
		output           string
		getLatestVersion GetLatestVersionFunc
		wantLatest       string
		wantErr          bool
	}{
		{
			name:         "latest version not implemented (error)",
			buildVersion: "1.0.0",
			commit:       "abc123",
			buildTime:    "2024-06-01T00:00:00Z",
			output:       "",
			getLatestVersion: func(ctx context.Context) (string, error) {
				return "", errors.New("not implemented")
			},
			wantLatest: "<unable to determine>",
			wantErr:    false,
		},
		{
			name:         "latest version is same as current",
			buildVersion: "1.2.3",
			commit:       "def456",
			buildTime:    "2024-06-02T00:00:00Z",
			output:       "",
			getLatestVersion: func(ctx context.Context) (string, error) {
				return "1.2.3", nil
			},
			wantLatest: "1.2.3",
			wantErr:    false,
		},
		{
			name:         "latest version is newer",
			buildVersion: "1.2.3",
			commit:       "def456",
			buildTime:    "2024-06-02T00:00:00Z",
			output:       "",
			getLatestVersion: func(ctx context.Context) (string, error) {
				return "1.3.0", nil
			},
			wantLatest: "1.3.0",
			wantErr:    false,
		},
		{
			name:         "latest version is older",
			buildVersion: "1.2.3",
			commit:       "def456",
			buildTime:    "2024-06-02T00:00:00Z",
			output:       "",
			getLatestVersion: func(ctx context.Context) (string, error) {
				return "1.2.2", nil
			},
			wantLatest: "1.2.2",
			wantErr:    false,
		},
		{
			name:         "latest version is unparsable",
			buildVersion: "1.2.3",
			commit:       "def456",
			buildTime:    "2024-06-02T00:00:00Z",
			output:       "",
			getLatestVersion: func(ctx context.Context) (string, error) {
				return "not-a-version", nil
			},
			wantLatest: "not-a-version",
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore global VersionInformation
			orig := settings.VersionInformation
			settings.VersionInformation.BuildVersion = tt.buildVersion
			settings.VersionInformation.Commit = tt.commit
			settings.VersionInformation.BuildTime = tt.buildTime

			r, w, _ := os.Pipe()
			ioStreams := &terminal.IOStreams{
				In:     os.Stdin,
				Out:    w,
				ErrOut: w,
			}
			options := &CmdOptionsVersion{
				IOStreams:        ioStreams,
				CliParams:        &settings.Run{},
				Output:           tt.output,
				GetLatestVersion: tt.getLatestVersion,
			}
			ctx := context.Background()
			w2 := writer.New(ioStreams, options.CliParams)
			ctx = writer.WithWriter(ctx, w2)

			err := options.PrintVersion(ctx)
			w.Close()
			if (err != nil) != tt.wantErr {
				t.Errorf("PrintVersion() error = %v, wantErr %v", err, tt.wantErr)
			}

			// Optionally, check output for expected latest version string
			outBytes, _ := io.ReadAll(r)
			outStr := string(outBytes)
			if tt.wantLatest != "" && !strings.Contains(outStr, tt.wantLatest) {
				t.Errorf("output does not contain expected latest version: got %q, want to contain %q", outStr, tt.wantLatest)
			}

			// Restore global VersionInformation
			settings.VersionInformation = orig
		})
	}
}

func TestVersionCmdOptions_PrintVersion_WithVersionExtra(t *testing.T) {
	orig := settings.VersionInformation
	defer func() { settings.VersionInformation = orig }()

	settings.VersionInformation = settings.VersionInfo{
		BuildVersion: "1.0.0",
		Commit:       "abc123",
		BuildTime:    "2024-06-01T00:00:00Z",
	}

	ioStreams, out, _ := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, &settings.Run{})
	ctx := writer.WithWriter(context.Background(), w)

	options := &CmdOptionsVersion{
		IOStreams:  ioStreams,
		CliParams:  &settings.Run{},
		BinaryName: "mycli",
		VersionExtra: &settings.VersionInfo{
			BuildVersion: "v2.0.0",
			Commit:       "embed789",
			BuildTime:    "2026-01-01T00:00:00Z",
		},
		GetLatestVersion: func(ctx context.Context) (string, error) {
			return "1.0.0", nil
		},
	}

	err := options.PrintVersion(ctx)
	require.NoError(t, err)

	output := out.String()
	assert.Contains(t, output, "mycli")
	assert.Contains(t, output, "v2.0.0")
	assert.Contains(t, output, "embed789")
	assert.Contains(t, output, "1.0.0")
}

func TestVersionCmdOptions_PrintVersion_JSONWithVersionExtra(t *testing.T) {
	orig := settings.VersionInformation
	defer func() { settings.VersionInformation = orig }()

	settings.VersionInformation = settings.VersionInfo{
		BuildVersion: "1.0.0",
		Commit:       "abc123",
		BuildTime:    "2024-06-01T00:00:00Z",
	}

	ioStreams, out, _ := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, &settings.Run{})
	ctx := writer.WithWriter(context.Background(), w)

	options := &CmdOptionsVersion{
		IOStreams:  ioStreams,
		CliParams:  &settings.Run{},
		Output:     "json",
		BinaryName: "mycli",
		VersionExtra: &settings.VersionInfo{
			BuildVersion: "v2.0.0",
			Commit:       "embed789",
			BuildTime:    "2026-01-01T00:00:00Z",
		},
		GetLatestVersion: func(ctx context.Context) (string, error) {
			return "1.0.0", nil
		},
	}

	err := options.PrintVersion(ctx)
	require.NoError(t, err)

	output := out.String()
	assert.Contains(t, output, "embedder")
	assert.Contains(t, output, "mycli")
}

func TestNewVersionDetails_WithVersionExtra(t *testing.T) {
	t.Parallel()
	extra := &settings.VersionInfo{
		BuildVersion: "v2.0.0",
		Commit:       "embed789",
		BuildTime:    "2026-01-01T00:00:00Z",
	}
	details := newVersionDetails("1.0.0", "mycli", extra)

	embedder, ok := details["embedder"].(map[string]any)
	require.True(t, ok, "expected 'embedder' key in details")
	assert.Equal(t, "mycli", embedder["name"])
	assert.Equal(t, "v2.0.0", embedder["version"])
}

func TestNewVersionDetails_WithoutVersionExtra(t *testing.T) {
	t.Parallel()
	details := newVersionDetails("1.0.0", "scafctl", nil)
	assert.NotContains(t, details, "embedder")
}

func TestCommandVersion_UsesPath(t *testing.T) {
	t.Parallel()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandVersion(&settings.Run{}, ioStreams, "mycli", nil)
	assert.Contains(t, cmd.Short, "mycli")
}
