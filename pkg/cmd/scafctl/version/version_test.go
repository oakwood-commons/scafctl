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
