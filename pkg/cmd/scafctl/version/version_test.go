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
	if err != nil {
		t.Fatalf("PrintVersion() error = %v", err)
	}

	output := out.String()
	// Should contain embedder info
	if !strings.Contains(output, "mycli") {
		t.Errorf("output should contain embedder binary name 'mycli', got: %s", output)
	}
	if !strings.Contains(output, "v2.0.0") {
		t.Errorf("output should contain embedder version 'v2.0.0', got: %s", output)
	}
	if !strings.Contains(output, "embed789") {
		t.Errorf("output should contain embedder commit 'embed789', got: %s", output)
	}
	// Should still contain scafctl version info
	if !strings.Contains(output, "1.0.0") {
		t.Errorf("output should contain scafctl version '1.0.0', got: %s", output)
	}
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
	if err != nil {
		t.Fatalf("PrintVersion() error = %v", err)
	}

	output := out.String()
	// JSON output should contain embedder block
	if !strings.Contains(output, "embedder") {
		t.Errorf("JSON output should contain 'embedder' key, got: %s", output)
	}
	if !strings.Contains(output, "mycli") {
		t.Errorf("JSON output should contain embedder name 'mycli', got: %s", output)
	}
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
	if !ok {
		t.Fatal("expected 'embedder' key in details")
	}
	if embedder["name"] != "mycli" {
		t.Errorf("embedder name = %v, want 'mycli'", embedder["name"])
	}
	if embedder["version"] != "v2.0.0" {
		t.Errorf("embedder version = %v, want 'v2.0.0'", embedder["version"])
	}
}

func TestNewVersionDetails_WithoutVersionExtra(t *testing.T) {
	t.Parallel()
	details := newVersionDetails("1.0.0", "scafctl", nil)

	if _, ok := details["embedder"]; ok {
		t.Error("expected no 'embedder' key when VersionExtra is nil")
	}
}

func TestCommandVersion_UsesPath(t *testing.T) {
	t.Parallel()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandVersion(&settings.Run{}, ioStreams, "mycli", nil)
	if !strings.Contains(cmd.Short, "mycli") {
		t.Errorf("Short should contain path 'mycli', got: %s", cmd.Short)
	}
}
