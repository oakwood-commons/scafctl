// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"testing"

	"github.com/Masterminds/semver/v3"
	catalogpkg "github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandPull(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPull(cliParams, ioStreams, "scafctl/catalog")

	require.NotNil(t, cmd)
	assert.Equal(t, "pull <reference>", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandPull_Flags(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPull(cliParams, ioStreams, "scafctl/catalog")

	flagTests := []struct {
		name     string
		defValue string
	}{
		{"as", ""},
		{"force", "false"},
		{"dry-run", "false"},
		{"insecure", "false"},
		{"no-cache", "false"},
	}

	for _, tt := range flagTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := cmd.Flag(tt.name)
			require.NotNil(t, f, "flag %q should exist", tt.name)
			assert.Equal(t, tt.defValue, f.DefValue, "flag %q default value", tt.name)
		})
	}
}

func TestCommandPull_ForceFlagShorthand(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPull(cliParams, ioStreams, "scafctl/catalog")

	f := cmd.PersistentFlags().ShorthandLookup("f")
	require.NotNil(t, f, "shorthand -f should exist")
	assert.Equal(t, "force", f.Name)
}

func TestCommandPull_RequiresExactlyOneArg(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPull(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts 1 arg(s)")
}

func TestCommandPull_InvalidReference(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPull(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"not-a-valid-remote-reference"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRemoteReferenceRequired)
}

func TestCommandPull_NoCacheFlag(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPull(cliParams, ioStreams, "scafctl/catalog")

	f := cmd.Flag("no-cache")
	require.NotNil(t, f, "no-cache flag should exist")
	assert.Equal(t, "false", f.DefValue)
}

func TestCommandPull_AsFlag(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPull(cliParams, ioStreams, "scafctl/catalog")

	f := cmd.Flag("as")
	require.NotNil(t, f, "as flag should exist")
	assert.Equal(t, "", f.DefValue)
}

func TestCommandPull_ShortNameNoCatalog(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPull(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	// Short name without --catalog and no config
	cmd.SetArgs([]string{"my-solution@1.0.0"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRemoteReferenceRequired)
}

func TestRunPull_ValidatesVersionConstraintBeforeRemoteShape(t *testing.T) {
	t.Parallel()

	ctx := newCatalogTestCtx(t)
	err := runPull(ctx, &PullOptions{
		Reference:         "demo@1.0.0",
		VersionConstraint: "^2.0.0",
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrVersionConstraintConflict)
}

func TestRunPull_IgnoresKindForRemoteOnlyGate(t *testing.T) {
	t.Parallel()

	ctx := newCatalogTestCtx(t)
	err := runPull(ctx, &PullOptions{
		Reference: "demo",
		Kind:      "not-a-kind",
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRemoteReferenceRequired)
	assert.NotErrorIs(t, err, catalogpkg.ErrInvalidReference)
}

func TestRunPull_ValidatesTargetNameFromBuiltKind(t *testing.T) {
	t.Parallel()

	ctx := newCatalogTestCtx(t)
	err := runPull(ctx, &PullOptions{
		Reference:  "ghcr.io/myorg/scafctl/solutions/demo@1.0.0",
		TargetName: "renamed",
	})

	require.Error(t, err)
	var refErr *catalogpkg.InvalidReferenceError
	require.ErrorAs(t, err, &refErr)
	assert.Equal(t, "renamed", refErr.Input)
}

func TestValidate_TargetName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		opts    *PullOptions
		wantErr error
	}{
		{
			name: "versioned target name is allowed",
			opts: &PullOptions{
				Reference:  "demo@1.0.0",
				Kind:       "solution",
				TargetName: "renamed@2.0.0",
			},
		},
		{
			name: "bare target name is rejected",
			opts: &PullOptions{
				Reference:  "demo@1.0.0",
				Kind:       "solution",
				TargetName: "renamed",
			},
			wantErr: catalogpkg.ErrInvalidReference,
		},
		{
			name: "digest target name is rejected",
			opts: &PullOptions{
				Reference:  "demo@1.0.0",
				Kind:       "solution",
				TargetName: "renamed@sha256:abc",
			},
			wantErr: catalogpkg.ErrInvalidReference,
		},
		{
			name: "missing kind is rejected when validating as",
			opts: &PullOptions{
				Reference:  "demo@1.0.0",
				TargetName: "renamed@2.0.0",
			},
			wantErr: catalogpkg.ErrInvalidReference,
		},
		{
			name: "remote reference is rejected for kind subcommands",
			opts: &PullOptions{
				Reference: "ghcr.io/myorg/scafctl/solutions/demo@1.0.0",
				Kind:      "solution",
			},
			wantErr: catalogpkg.ErrInvalidReference,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := Validate(tt.opts)
			if tt.wantErr == nil {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			assert.ErrorIs(t, err, tt.wantErr)

			var refErr *catalogpkg.InvalidReferenceError
			assert.ErrorAs(t, err, &refErr)
		})
	}
}

func TestWritePullDryRun(t *testing.T) {
	t.Parallel()

	ioStreams, outBuf, errBuf := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, settings.NewCliParams())
	_ = writer.WithWriter(context.Background(), w)
	ref := catalogpkg.Reference{
		Kind:    catalogpkg.ArtifactKindSolution,
		Name:    "demo",
		Version: semver.MustParse("1.2.3"),
	}

	writePullDryRun(w, &PullOptions{TargetName: "renamed@2.0.0"}, ref, "ghcr.io", "myorg/scafctl")

	output := outBuf.String() + errBuf.String()
	assert.Contains(t, output, "Would pull demo@1.2.3 from ghcr.io/myorg/scafctl/solutions/demo to local catalog as renamed@2.0.0")
	assert.NotContains(t, output, "[skeleton]")
}

func TestHandlePullCopyError_Exists(t *testing.T) {
	t.Parallel()

	ioStreams, _, errBuf := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, settings.NewCliParams())
	err := handlePullCopyError(w, &catalogpkg.ArtifactExistsError{
		Reference: catalogpkg.Reference{Name: "demo"},
		Catalog:   catalogpkg.LocalCatalogName,
	})

	require.Error(t, err)
	assert.Contains(t, errBuf.String(), "artifact already exists locally")
	assert.True(t, catalogpkg.IsExists(err))
}

func TestHandlePullCopyError_Generic(t *testing.T) {
	t.Parallel()

	ioStreams, _, errBuf := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, settings.NewCliParams())
	err := handlePullCopyError(w, assert.AnError)

	require.Error(t, err)
	assert.Contains(t, errBuf.String(), "failed to pull artifact")
	assert.False(t, catalogpkg.IsExists(err))
}

func BenchmarkCommandPull(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		CommandPull(cliParams, ioStreams, "scafctl/catalog")
	}
}
