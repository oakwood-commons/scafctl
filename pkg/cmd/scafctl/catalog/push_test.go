// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"bytes"
	"errors"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandPush(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPush(cliParams, ioStreams, "scafctl/catalog")

	require.NotNil(t, cmd)
	assert.Equal(t, "push <reference>", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandPush_Flags(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPush(cliParams, ioStreams, "scafctl/catalog")

	flagTests := []struct {
		name     string
		defValue string
	}{
		{"catalog", ""},
		{"as", ""},
		{"kind", ""},
		{"origin", ""},
		{"force", "false"},
		{"dry-run", "false"},
		{"insecure", "false"},
	}

	for _, tt := range flagTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := cmd.Flags().Lookup(tt.name)
			require.NotNil(t, f, "flag %q should exist", tt.name)
			assert.Equal(t, tt.defValue, f.DefValue, "flag %q default value", tt.name)
		})
	}
}

func TestCommandPush_CatalogFlagShorthand(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPush(cliParams, ioStreams, "scafctl/catalog")

	f := cmd.Flags().ShorthandLookup("c")
	require.NotNil(t, f, "shorthand -c should exist")
	assert.Equal(t, "catalog", f.Name)
}

func TestCommandPush_ForceFlagShorthand(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPush(cliParams, ioStreams, "scafctl/catalog")

	f := cmd.Flags().ShorthandLookup("f")
	require.NotNil(t, f, "shorthand -f should exist")
	assert.Equal(t, "force", f.Name)
}

func TestCommandPush_RequiresExactlyOneArg(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPush(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required argument: <name@version>")
}

func TestCommandPush_InvalidKind(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPush(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"my-solution@1.0.0", "--kind", "not-a-valid-kind", "--catalog", "ghcr.io/org"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid kind")
	assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
}

func TestCommandPush_InvalidSelector(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "origin with remote reference",
			args:    []string{"ghcr.io/src/solutions/my-solution@1.0.0", "--origin", catalog.OriginBuilt},
			wantErr: "--origin",
		},
		{
			name:    "invalid origin",
			args:    []string{"my-solution@1.0.0", "--origin", "https://ghcr.io/org"},
			wantErr: "invalid --origin",
		},
		{
			name:    "path that is not a remote reference",
			args:    []string{"a/b"},
			wantErr: "expected an artifact name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ioStreams, _, _ := terminal.NewTestIOStreams()
			cmd := CommandPush(settings.NewCliParams(), ioStreams, "scafctl/catalog")
			cmd.SetContext(newCatalogTestCtx(t))
			cmd.SetArgs(append(tt.args, "--catalog", "ghcr.io/dest"))

			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
		})
	}
}

func TestCommandPush_NoSBOMFlag(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPush(cliParams, ioStreams, "scafctl/catalog")

	// --sbom flag should no longer exist
	assert.Nil(t, cmd.Flags().Lookup("sbom"), "sbom flag should be removed")

	noSBOM := cmd.Flags().Lookup("no-sbom")
	require.NotNil(t, noSBOM, "no-sbom flag should exist")
	assert.Equal(t, "false", noSBOM.DefValue)
}

func TestShouldAttachSBOM(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		kind   catalog.ArtifactKind
		noSBOM bool
		want   bool
	}{
		{
			name:   "solution kind attaches SBOM",
			kind:   catalog.ArtifactKindSolution,
			noSBOM: false,
			want:   true,
		},
		{
			name:   "empty kind defaults to SBOM",
			kind:   "",
			noSBOM: false,
			want:   true,
		},
		{
			name:   "provider kind skips SBOM",
			kind:   catalog.ArtifactKindProvider,
			noSBOM: false,
			want:   false,
		},
		{
			name:   "auth-handler kind skips SBOM",
			kind:   catalog.ArtifactKindAuthHandler,
			noSBOM: false,
			want:   false,
		},
		{
			name:   "solution with --no-sbom skips SBOM",
			kind:   catalog.ArtifactKindSolution,
			noSBOM: true,
			want:   false,
		},
		{
			name:   "empty kind with --no-sbom skips SBOM",
			kind:   "",
			noSBOM: true,
			want:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := shouldAttachSBOM(tc.kind, tc.noSBOM)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestCommandPush_RequiresDestinationCatalog(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPush(cliParams, ioStreams, "mycli/catalog")
	cmd.SetContext(settings.IntoContext(newCatalogTestCtx(t), cliParams))
	// A default catalog is never used as the push destination.
	cmd.SetArgs([]string{"my-solution@1.0.0"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.ErrorIs(t, err, catalog.ErrPushDestinationRequired)
	assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
	assert.Contains(t, err.Error(), "mycli catalog push")
}

func TestPushResolveError(t *testing.T) {
	t.Parallel()

	ambiguous := &catalog.AmbiguousArtifactError{
		Kind:    catalog.ArtifactKindSolution,
		Name:    "app",
		Version: "1.0.0",
		Candidates: []catalog.PushCandidate{
			{Name: "app", Version: "1.0.0", Digest: "sha256:aaa", Tags: []string{"1.0.0"}},
			{Name: "app", Origin: "ghcr.io/org", Version: "1.0.0", Digest: "sha256:bbb", Tags: []string{"1.0.0"}},
		},
	}

	tests := []struct {
		name      string
		err       error
		wantCode  int
		wantHints []string
	}{
		{
			name:      "ambiguous artifact prints a command per copy",
			err:       ambiguous,
			wantCode:  exitcode.InvalidInput,
			wantHints: []string{"mycli catalog push app@1.0.0 --kind solution --origin built", "--origin ghcr.io/org"},
		},
		{
			name:      "ambiguous kind prints a command per kind",
			err:       &catalog.AmbiguousKindError{Name: "app", Kinds: []catalog.ArtifactKind{catalog.ArtifactKindProvider, catalog.ArtifactKindSolution}},
			wantCode:  exitcode.InvalidInput,
			wantHints: []string{"mycli catalog push app --kind provider", "mycli catalog push app --kind solution"},
		},
		{
			name:     "not found",
			err:      &catalog.ArtifactNotFoundError{Reference: catalog.Reference{Name: "app"}, Catalog: catalog.LocalCatalogName},
			wantCode: exitcode.FileNotFound,
		},
		{
			name:     "invalid reference",
			err:      &catalog.InvalidReferenceError{Input: "", Message: "name cannot be empty"},
			wantCode: exitcode.InvalidInput,
		},
		{
			name:     "other failure",
			err:      errors.New("index corrupt"),
			wantCode: exitcode.CatalogError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			w := writer.New(terminal.NewIOStreams(nil, &buf, &buf, false), settings.NewCliParams())

			err := pushResolveError(w, "mycli", tt.err)

			assert.Equal(t, tt.wantCode, exitcode.GetCode(err))
			assert.ErrorIs(t, err, tt.err)
			for _, h := range tt.wantHints {
				assert.Contains(t, buf.String(), h)
			}
			if len(tt.wantHints) == 0 {
				assert.NotContains(t, buf.String(), "Select one with")
			}
		})
	}
}

func TestPushOriginLabel(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "built locally", pushOriginLabel(""))
	assert.Equal(t, "from ghcr.io/org", pushOriginLabel("ghcr.io/org"))
}

func TestCommandPush_LatestFlag(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPush(cliParams, ioStreams, "scafctl/catalog")

	f := cmd.Flags().Lookup("latest")
	require.NotNil(t, f, "latest flag should exist")
	assert.Equal(t, "false", f.DefValue)
}

func BenchmarkCommandPush(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		CommandPush(cliParams, ioStreams, "scafctl/catalog")
	}
}
