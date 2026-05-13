// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package build

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/builder"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/format"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseByteSize(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		wantErr  bool
	}{
		{"100B", 100, false},
		{"1KB", 1024, false},
		{"50MB", 50 * 1024 * 1024, false},
		{"1GB", 1024 * 1024 * 1024, false},
		{"100", 100, false},
		{"50mb", 50 * 1024 * 1024, false},
		{"invalid", 0, true},
		{"MB", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := builder.ParseByteSize(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestFormatByteSize(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{100, "100 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{50 * 1024 * 1024, "50.0 MB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, format.Bytes(tt.input))
		})
	}
}

func TestTagFlagParsing(t *testing.T) {
	tests := []struct {
		name        string
		tag         string
		wantName    string
		wantVersion string
		wantErr     bool
	}{
		{
			name:        "name and version",
			tag:         "hello-world@1.0.0",
			wantName:    "hello-world",
			wantVersion: "1.0.0",
		},
		{
			name:        "name and prerelease version",
			tag:         "my-solution@0.1.0-beta.1",
			wantName:    "my-solution",
			wantVersion: "0.1.0-beta.1",
		},
		{
			name:     "name only",
			tag:      "hello-world",
			wantName: "hello-world",
		},
		{
			name:    "empty version after @",
			tag:     "hello-world@",
			wantErr: true,
		},
		{
			name:    "invalid version",
			tag:     "hello-world@notaversion",
			wantErr: true,
		},
		{
			name:    "empty tag",
			tag:     "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := catalog.ParseReference(catalog.ArtifactKindSolution, tt.tag)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantName, ref.Name)
			if tt.wantVersion != "" {
				require.NotNil(t, ref.Version)
				assert.Equal(t, tt.wantVersion, ref.Version.String())
			} else {
				assert.Nil(t, ref.Version)
			}
		})
	}
}

func TestTagFlagRemoteRefParsing(t *testing.T) {
	tests := []struct {
		name        string
		tag         string
		wantName    string
		wantVersion string
		wantErr     bool
	}{
		{
			name:        "full remote ref with kind",
			tag:         "ghcr.io/myorg/solutions/my-solution@1.0.0",
			wantName:    "my-solution",
			wantVersion: "1.0.0",
		},
		{
			name:        "full remote ref without kind",
			tag:         "ghcr.io/myorg/my-solution@2.0.0",
			wantName:    "my-solution",
			wantVersion: "2.0.0",
		},
		{
			name:        "remote ref with deep repository path",
			tag:         "registry.example.com/org/team/solutions/hello-world@0.1.0",
			wantName:    "hello-world",
			wantVersion: "0.1.0",
		},
		{
			name:     "remote ref without version",
			tag:      "ghcr.io/myorg/solutions/my-solution",
			wantName: "my-solution",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			remoteRef, err := catalog.ParseRemoteReference(tt.tag)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantName, remoteRef.Name)
			if tt.wantVersion != "" {
				assert.Equal(t, tt.wantVersion, remoteRef.Tag)
			} else {
				assert.Empty(t, remoteRef.Tag)
			}
		})
	}
}

func TestCommandBuildSolution_BumpFlag(t *testing.T) {
	t.Parallel()

	cliParams := &settings.Run{NoColor: true, BinaryName: "testcli"}
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandBuildSolution(cliParams, ioStreams, "build")

	f := cmd.Flags().Lookup("bump")
	require.NotNil(t, f, "--bump flag should exist")
	assert.Equal(t, "", f.DefValue)
}

func TestCommandBuildSolution_BumpConflictsWithVersion(t *testing.T) {
	t.Parallel()

	cliParams := &settings.Run{NoColor: true, BinaryName: "testcli"}
	ioStreams, _, errBuf := terminal.NewTestIOStreams()
	cmd := CommandBuildSolution(cliParams, ioStreams, "build")
	cmd.SetArgs([]string{"--bump", "patch", "--version", "1.0.0", "-f", "test.yaml"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error()+errBuf.String(), "--bump cannot be used together with --version or a versioned --tag")
}

func TestCommandBuildSolution_BumpInvalidLevel(t *testing.T) {
	t.Parallel()

	cliParams := &settings.Run{NoColor: true, BinaryName: "testcli"}
	ioStreams, _, errBuf := terminal.NewTestIOStreams()
	cmd := CommandBuildSolution(cliParams, ioStreams, "build")
	cmd.SetArgs([]string{"--bump", "invalid", "-f", "test.yaml"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error()+errBuf.String(), "invalid bump level")
}

func TestCommandBuildSolution_BumpConflictsWithVersionedTag(t *testing.T) {
	t.Parallel()

	cliParams := &settings.Run{NoColor: true, BinaryName: "testcli"}
	ioStreams, _, errBuf := terminal.NewTestIOStreams()
	cmd := CommandBuildSolution(cliParams, ioStreams, "build")
	cmd.SetArgs([]string{"--bump", "patch", "-t", "my-solution@1.0.0", "-f", "test.yaml"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error()+errBuf.String(), "--bump cannot be used together with --version or a versioned --tag")
}

func TestCommandBuildSolution_TagLatestRejected(t *testing.T) {
	t.Parallel()

	cliParams := &settings.Run{NoColor: true, BinaryName: "testcli"}
	ioStreams, _, _ := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, cliParams)
	cmd := CommandBuildSolution(cliParams, ioStreams, "build")
	cmd.SetContext(writer.WithWriter(context.Background(), w))
	cmd.SetArgs([]string{"-t", "my-solution@latest", "-f", "test.yaml"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "'latest' is not a valid build version")
}

func TestCommandBuildSolution_TagRemoteRefWrongKind(t *testing.T) {
	t.Parallel()

	cliParams := &settings.Run{NoColor: true, BinaryName: "testcli"}
	ioStreams, _, _ := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, cliParams)
	cmd := CommandBuildSolution(cliParams, ioStreams, "build")
	cmd.SetContext(writer.WithWriter(context.Background(), w))
	cmd.SetArgs([]string{"-t", "ghcr.io/myorg/providers/my-provider@1.0.0", "-f", "test.yaml"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "references kind")
}

func TestCommandBuildSolution_TagRemoteRefSetsNameAndVersion(t *testing.T) {
	t.Parallel()

	// A valid remote --tag should parse without tag errors.
	// The command will fail later (no solution file), but tag parsing succeeds.
	cliParams := &settings.Run{NoColor: true, BinaryName: "testcli"}
	ioStreams, _, _ := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, cliParams)
	cmd := CommandBuildSolution(cliParams, ioStreams, "build")
	cmd.SetContext(writer.WithWriter(context.Background(), w))
	cmd.SetArgs([]string{"-t", "ghcr.io/myorg/solutions/my-solution@2.0.0", "-f", "/nonexistent/solution.yaml"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "invalid tag")
	assert.NotContains(t, err.Error(), "references kind")
}

func TestCommandBuildSolution_BumpWithUnversionedTag(t *testing.T) {
	t.Parallel()

	// --bump with a name-only tag (no version) should NOT conflict.
	cliParams := &settings.Run{NoColor: true, BinaryName: "testcli"}
	ioStreams, _, _ := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, cliParams)
	cmd := CommandBuildSolution(cliParams, ioStreams, "build")
	cmd.SetContext(writer.WithWriter(context.Background(), w))
	cmd.SetArgs([]string{"--bump", "patch", "-t", "my-solution", "-f", "/nonexistent/solution.yaml"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "--bump cannot be used together with --version")
}

func TestRunBuildSolution_NoGitMetadata(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Write solution file with pre-existing git annotations.
	solFile := filepath.Join(tmpDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solFile, []byte(`apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: git-test
  version: 1.0.0
  annotations:
    io.scafctl.build.commit: "abc123"
    io.scafctl.build.dirty: "true"
spec: {}
`), 0o644))

	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, &settings.Run{NoColor: true, BinaryName: "testcli"})
	ctx := writer.WithWriter(context.Background(), w)
	nlgr := logr.Discard()
	ctx = logger.WithLogger(ctx, &nlgr)

	opts := &SolutionOptions{
		File:          solFile,
		Version:       "1.0.0",
		DryRun:        true,
		NoBundle:      true,
		SkipLint:      true,
		SkipTests:     true,
		NoGitMetadata: true,
		CliParams:     &settings.Run{NoColor: true, BinaryName: "testcli"},
		IOStreams:     ioStreams,
	}
	err := runBuildSolution(ctx, opts)
	require.NoError(t, err)
	assert.Contains(t, outBuf.String(), "Dry run: would build git-test@1.0.0")
}
