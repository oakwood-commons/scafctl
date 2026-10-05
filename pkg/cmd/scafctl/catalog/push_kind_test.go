// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/adrg/xdg"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testPushRegistry   = "ghcr.io"
	testPushRepository = "org/scafctl"
	testPushCatalogURL = testPushRegistry + "/" + testPushRepository
)

// testPushDigest is a well-formed digest used to exercise digest rejection.
var testPushDigest = "sha256:" + strings.Repeat("a", 64)

// pushOpts returns kind-subcommand options with a non-default binary name so
// every rendered command proves it honors the embedder's name.
func pushOpts(kind catalog.ArtifactKind, reference, origin string) *pushKindOptions {
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	return &pushKindOptions{
		PushOptions: PushOptions{
			Reference: reference,
			Kind:      string(kind),
			CliParams: cliParams,
		},
		Origin: origin,
	}
}

// buildPush parses opts the same way the command pipeline does, minus the
// catalog location (which needs config for the default catalog).
func buildPush(t *testing.T, opts *pushKindOptions) pushReferenceBuild {
	t.Helper()
	build, err := newPushReferenceBuilder(t.Context(), opts).
		parseShortReference().
		rejectDigest().
		parseOrigin().
		parseTarget().
		Build()
	require.NoError(t, err)
	return build
}

// newPushTestCommand wires the push command the way CommandCatalog does;
// CommandPush registers the kind subcommands itself.
func newPushTestCommand(ctx context.Context) *cobra.Command {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPush(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(ctx)
	return cmd
}

// newXDGLocalCatalog points the XDG-resolved local catalog at an empty temp
// dir and returns it, so command tests can seed what the command will read.
// Callers must not run in parallel.
func newXDGLocalCatalog(t *testing.T) *catalog.LocalCatalog {
	t.Helper()
	tmp := t.TempDir()
	// xdg caches env-derived paths globally. Register the reload before Setenv
	// so it runs after the env is restored and temp paths do not leak.
	t.Cleanup(func() { xdg.Reload() })
	t.Setenv("XDG_DATA_HOME", tmp)
	xdg.Reload()

	cat, err := catalog.NewLocalCatalog(logr.Discard())
	require.NoError(t, err)
	return cat
}

func TestCommandPush_KindSubcommandsRegistered(t *testing.T) {
	t.Parallel()

	cmd := newPushTestCommand(newCatalogTestCtx(t))

	tests := []struct {
		name  string
		alias string
	}{
		{name: "solution", alias: "sol"},
		{name: "solution", alias: "solutions"},
		{name: "provider", alias: "providers"},
		{name: "auth-handler", alias: "auth-handlers"},
	}
	for _, tt := range tests {
		sub, _, err := cmd.Find([]string{tt.name})
		require.NoError(t, err)
		assert.Equal(t, tt.name, sub.Name())

		viaAlias, _, err := cmd.Find([]string{tt.alias})
		require.NoError(t, err)
		assert.Equal(t, tt.name, viaAlias.Name(), "alias %q", tt.alias)
	}
}

func TestCommandPushKind_Flags(t *testing.T) {
	t.Parallel()

	for _, pk := range pushKinds {
		// Each subtest builds its own command: cmd.Find merges persistent flags
		// into the command tree, which races when shared.
		t.Run(string(pk.Kind), func(t *testing.T) {
			t.Parallel()
			cmd := newPushTestCommand(newCatalogTestCtx(t))
			sub, _, err := cmd.Find([]string{string(pk.Kind)})
			require.NoError(t, err)

			assert.NotNil(t, sub.Flags().Lookup("catalog"))
			for _, name := range []string{"origin", "as", "force", "dry-run", "insecure", "latest"} {
				assert.NotNil(t, sub.InheritedFlags().Lookup(name), "flag --%s", name)
			}
			assert.Equal(t, "c", sub.Flags().Lookup("catalog").Shorthand)
			assert.Equal(t, "f", sub.InheritedFlags().Lookup("force").Shorthand)

			// --no-sbom only applies to solutions, which are the only kind with SBOMs.
			hasNoSBOM := sub.Flags().Lookup("no-sbom") != nil
			assert.Equal(t, pk.Kind == catalog.ArtifactKindSolution, hasNoSBOM, "--no-sbom")

			// The kind comes from the subcommand, never from a flag.
			assert.Nil(t, sub.Flags().Lookup("kind"))
		})
	}
}

func TestCommandPush_RootFlags(t *testing.T) {
	t.Parallel()

	cmd := newPushTestCommand(newCatalogTestCtx(t))

	assert.NotNil(t, cmd.PersistentFlags().Lookup("origin"))
	assert.NotNil(t, cmd.Flags().Lookup("no-sbom"))
	assert.Nil(t, cmd.Flags().Lookup("kind"), "the kind comes from the reference path")
	assert.Nil(t, cmd.Flags().Lookup("catalog"), "the destination comes from the reference")
}

func TestCommandPushKind_RequiresOneArg(t *testing.T) {
	t.Parallel()

	cmd := newPushTestCommand(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"solution"})

	require.Error(t, cmd.Execute())
}

func TestValidatePush(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		kind       string
		reference  string
		targetName string
		wantErr    string
	}{
		{name: "name only", kind: "solution", reference: "my-sol"},
		{name: "name and version", kind: "solution", reference: "my-sol@1.0.0"},
		{name: "valid rename", kind: "solution", reference: "my-sol@1.0.0", targetName: "renamed@2.0.0"},
		{name: "empty reference", kind: "solution", reference: "", wantErr: "name required"},
		{name: "host with dot", kind: "solution", reference: "ghcr.io/org/scafctl/solutions/my-sol@1.0.0", wantErr: "full remote references are not allowed"},
		{name: "oci scheme", kind: "solution", reference: "oci://ghcr.io/org/my-sol@1.0.0", wantErr: "full remote references are not allowed"},
		{name: "localhost", kind: "provider", reference: "localhost:5000/my-plugin@1.0.0", wantErr: "full remote references are not allowed"},
		{name: "unknown kind", kind: "widget", reference: "my-sol@1.0.0", wantErr: "artifact kind is required"},
		{name: "rename without version", kind: "solution", reference: "my-sol@1.0.0", targetName: "renamed", wantErr: "bare name"},
		{name: "rename to latest", kind: "solution", reference: "my-sol@1.0.0", targetName: "renamed@latest", wantErr: "bare name"},
		{name: "rename to digest", kind: "solution", reference: "my-sol@1.0.0", targetName: "renamed@" + testPushDigest, wantErr: "digest"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := pushOpts(catalog.ArtifactKind(tt.kind), tt.reference, "")
			opts.Kind = tt.kind
			opts.TargetName = tt.targetName

			err := validatePush(opts)

			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestValidatePush_RemoteReferenceIsTyped(t *testing.T) {
	t.Parallel()

	ref := "ghcr.io/org/scafctl/solutions/my-sol@1.0.0"
	err := validatePush(pushOpts(catalog.ArtifactKindSolution, ref, ""))

	var invalid *catalog.InvalidReferenceError
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, ref, invalid.Input)
}

func TestPushReferenceBuilder_ParseShortReference(t *testing.T) {
	t.Parallel()

	t.Run("name only leaves the version open", func(t *testing.T) {
		t.Parallel()

		build := buildPush(t, pushOpts(catalog.ArtifactKindProvider, "my-plugin", ""))

		assert.Equal(t, catalog.ArtifactKindProvider, build.Reference.Kind)
		assert.Equal(t, "my-plugin", build.Reference.Name)
		assert.False(t, build.Reference.HasVersion())
	})

	t.Run("name and version", func(t *testing.T) {
		t.Parallel()

		build := buildPush(t, pushOpts(catalog.ArtifactKindSolution, "my-sol@1.2.3", ""))

		assert.Equal(t, "my-sol", build.Reference.Name)
		require.True(t, build.Reference.HasVersion())
		assert.Equal(t, "1.2.3", build.Reference.Version.String())
	})

	t.Run("invalid version is rejected", func(t *testing.T) {
		t.Parallel()

		_, err := newPushReferenceBuilder(t.Context(), pushOpts(catalog.ArtifactKindSolution, "my-sol@not-a-version", "")).
			parseShortReference().
			Build()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid reference")
	})
}

func TestPushReferenceBuilder_RejectDigest(t *testing.T) {
	t.Parallel()

	_, err := newPushReferenceBuilder(t.Context(), pushOpts(catalog.ArtifactKindSolution, "my-sol@"+testPushDigest, "")).
		parseShortReference().
		rejectDigest().
		Build()

	require.Error(t, err)
	assert.Equal(t, pushDigestUnsupportedMsg, err.Error())
}

func TestPushReferenceBuilder_ParseOrigin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		origin     string
		wantBuilt  bool
		wantRemote string
		wantErr    string
	}{
		{name: "no origin leaves the reference open"},
		{name: "built", origin: "built", wantBuilt: true},
		{name: "registry", origin: testPluginsOrigin, wantRemote: testPluginsOrigin},
		{name: "oci scheme is normalized", origin: "oci://" + testPluginsOrigin + "/", wantRemote: testPluginsOrigin},
		{name: "empty registry is rejected", origin: "oci://", wantErr: "invalid --origin"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			build, err := newPushReferenceBuilder(t.Context(), pushOpts(catalog.ArtifactKindSolution, "my-sol@1.0.0", tt.origin)).
				parseShortReference().
				parseOrigin().
				Build()

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantBuilt, build.Reference.Built)
			assert.Equal(t, tt.wantRemote, build.Reference.Remote)
		})
	}
}

func TestPushReferenceBuilder_ParseTarget(t *testing.T) {
	t.Parallel()

	t.Run("no rename leaves the target empty", func(t *testing.T) {
		t.Parallel()

		build := buildPush(t, pushOpts(catalog.ArtifactKindSolution, "my-sol@1.0.0", ""))

		assert.Empty(t, build.Target.Name)
	})

	t.Run("rename carries name, version and kind", func(t *testing.T) {
		t.Parallel()

		opts := pushOpts(catalog.ArtifactKindSolution, "my-sol@1.0.0", "")
		opts.TargetName = "renamed@2.0.0"

		build := buildPush(t, opts)

		assert.Equal(t, "renamed", build.Target.Name)
		assert.Equal(t, catalog.ArtifactKindSolution, build.Target.Kind)
		require.True(t, build.Target.HasVersion())
		assert.Equal(t, "2.0.0", build.Target.Version.String())
	})

	for name, target := range map[string]string{
		"bare name":  "renamed",
		"digest pin": "renamed@" + testPushDigest,
	} {
		t.Run("rejects "+name, func(t *testing.T) {
			t.Parallel()

			opts := pushOpts(catalog.ArtifactKindSolution, "my-sol@1.0.0", "")
			opts.TargetName = target

			_, err := newPushReferenceBuilder(t.Context(), opts).
				parseShortReference().
				parseTarget().
				Build()

			require.Error(t, err)
		})
	}
}

func TestPushReferenceBuilder_ResolveCatalogLocation(t *testing.T) {
	t.Parallel()

	opts := pushOpts(catalog.ArtifactKindSolution, "my-sol@1.0.0", "")
	opts.Catalog = testPushCatalogURL

	build, err := newPushReferenceBuilder(t.Context(), opts).
		parseShortReference().
		resolveCatalogLocation().
		Build()

	require.NoError(t, err)
	assert.Equal(t, testPushRegistry, build.Registry)
	assert.Equal(t, testPushRepository, build.Repository)
}

func TestPushReferenceBuilder_FirstErrorShortCircuits(t *testing.T) {
	t.Parallel()

	opts := pushOpts(catalog.ArtifactKindSolution, "my-sol@"+testPushDigest, "oci://")
	opts.TargetName = "renamed"
	opts.Catalog = testPushCatalogURL

	_, err := newPushReferenceBuilder(t.Context(), opts).
		parseShortReference().
		rejectDigest().
		parseOrigin().
		parseTarget().
		resolveCatalogLocation().
		Build()

	require.Error(t, err)
	assert.Equal(t, pushDigestUnsupportedMsg, err.Error(), "later steps must not replace the first error")
}

func TestPushReferenceBuild_RemoteRef(t *testing.T) {
	t.Parallel()

	local := catalog.Reference{Kind: catalog.ArtifactKindSolution, Name: "my-sol", Version: semver.MustParse("1.0.0")}

	t.Run("without rename the local reference is used", func(t *testing.T) {
		t.Parallel()

		got := pushReferenceBuild{}.remoteRef(local)

		assert.Equal(t, local, got)
	})

	t.Run("rename replaces name and version", func(t *testing.T) {
		t.Parallel()

		target := catalog.Reference{Kind: catalog.ArtifactKindSolution, Name: "renamed", Version: semver.MustParse("2.0.0")}

		got := pushReferenceBuild{Target: target}.remoteRef(local)

		assert.Equal(t, "renamed", got.Name)
		assert.Equal(t, "2.0.0", got.Version.String())
	})
}

func TestResolvePushSource(t *testing.T) {
	t.Parallel()

	t.Run("single built artifact with open version resolves to the latest", func(t *testing.T) {
		t.Parallel()

		cat := newDeleteLocalCatalog(t)
		seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.0.0", "")
		seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.2.0", "")
		ctx, _ := newCapturingCtx(t)
		opts := pushOpts(catalog.ArtifactKindSolution, "my-sol", "")

		info, err := resolvePushSource(ctx, opts, cat, buildPush(t, opts))

		require.NoError(t, err)
		assert.Equal(t, "1.2.0", info.Reference.Version.String())
		assert.True(t, info.Reference.Built)
	})

	t.Run("single pulled artifact resolves without --origin", func(t *testing.T) {
		t.Parallel()

		cat := newDeleteLocalCatalog(t)
		seedLocal(t, cat, catalog.ArtifactKindProvider, "my-plugin", "1.0.0", testPluginsOrigin)
		ctx, _ := newCapturingCtx(t)
		opts := pushOpts(catalog.ArtifactKindProvider, "my-plugin@1.0.0", "")

		info, err := resolvePushSource(ctx, opts, cat, buildPush(t, opts))

		require.NoError(t, err)
		assert.Equal(t, testPluginsOrigin, info.Reference.Remote)
		assert.False(t, info.Reference.Built)
	})

	t.Run("--origin selects between built and pulled copies", func(t *testing.T) {
		t.Parallel()

		cat := newDeleteLocalCatalog(t)
		seedLocal(t, cat, catalog.ArtifactKindProvider, "my-plugin", "1.0.0", "")
		seedLocal(t, cat, catalog.ArtifactKindProvider, "my-plugin", "1.0.0", testPluginsOrigin)

		tests := []struct {
			name       string
			origin     string
			wantBuilt  bool
			wantRemote string
		}{
			{name: "built", origin: "built", wantBuilt: true},
			{name: "pulled", origin: testPluginsOrigin, wantRemote: testPluginsOrigin},
		}
		for _, tt := range tests {
			ctx, _ := newCapturingCtx(t)
			opts := pushOpts(catalog.ArtifactKindProvider, "my-plugin@1.0.0", tt.origin)

			info, err := resolvePushSource(ctx, opts, cat, buildPush(t, opts))

			require.NoError(t, err, tt.name)
			assert.Equal(t, tt.wantBuilt, info.Reference.Built, tt.name)
			assert.Equal(t, tt.wantRemote, info.Reference.Remote, tt.name)
		}
	})

	t.Run("several origins without --origin is ambiguous and hints each origin", func(t *testing.T) {
		t.Parallel()

		cat := newDeleteLocalCatalog(t)
		seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.0.0", "")
		seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.0.0", testPluginsOrigin)
		ctx, out := newCapturingCtx(t)
		opts := pushOpts(catalog.ArtifactKindSolution, "my-sol@1.0.0", "")

		_, err := resolvePushSource(ctx, opts, cat, buildPush(t, opts))

		require.Error(t, err)
		assert.ErrorIs(t, err, catalog.ErrAmbiguousArtifact)
		assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
		assert.Contains(t, out.String(), "re-run the same command with one of:")
		assert.Contains(t, out.String(), "--origin built")
		assert.Contains(t, out.String(), "--origin "+testPluginsOrigin)
	})

	t.Run("hint goes to stderr and survives --quiet", func(t *testing.T) {
		t.Parallel()

		cat := newDeleteLocalCatalog(t)
		seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.0.0", "")
		seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.0.0", testPluginsOrigin)
		var stdout, stderr bytes.Buffer
		params := settings.NewCliParams()
		params.IsQuiet = true
		w := writer.New(terminal.NewIOStreams(nil, &stdout, &stderr, false), params)
		ctx := writer.WithWriter(context.Background(), w)
		opts := pushOpts(catalog.ArtifactKindSolution, "my-sol@1.0.0", "")

		_, err := resolvePushSource(ctx, opts, cat, buildPush(t, opts))

		require.Error(t, err)
		assert.Empty(t, stdout.String())
		assert.Contains(t, stderr.String(), "--origin built")
		assert.Contains(t, stderr.String(), "--origin "+testPluginsOrigin)
	})

	t.Run("missing artifact is not found", func(t *testing.T) {
		t.Parallel()

		cat := newDeleteLocalCatalog(t)
		ctx, out := newCapturingCtx(t)
		opts := pushOpts(catalog.ArtifactKindSolution, "ghost@1.0.0", "")

		_, err := resolvePushSource(ctx, opts, cat, buildPush(t, opts))

		require.Error(t, err)
		assert.Equal(t, exitcode.FileNotFound, exitcode.GetCode(err))
		assert.Contains(t, out.String(), "not found in local catalog")
	})

	t.Run("full identity with the wrong origin is not found", func(t *testing.T) {
		t.Parallel()

		cat := newDeleteLocalCatalog(t)
		seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.0.0", "")
		ctx, _ := newCapturingCtx(t)
		opts := pushOpts(catalog.ArtifactKindSolution, "my-sol@1.0.0", testPluginsOrigin)

		_, err := resolvePushSource(ctx, opts, cat, buildPush(t, opts))

		require.Error(t, err)
		assert.Equal(t, exitcode.FileNotFound, exitcode.GetCode(err))
	})

	t.Run("kinds do not cross", func(t *testing.T) {
		t.Parallel()

		cat := newDeleteLocalCatalog(t)
		seedLocal(t, cat, catalog.ArtifactKindSolution, "shared", "1.0.0", "")
		ctx, _ := newCapturingCtx(t)
		opts := pushOpts(catalog.ArtifactKindProvider, "shared@1.0.0", "")

		_, err := resolvePushSource(ctx, opts, cat, buildPush(t, opts))

		require.Error(t, err)
		assert.Equal(t, exitcode.FileNotFound, exitcode.GetCode(err))
	})
}

func TestPushSourceLabel(t *testing.T) {
	t.Parallel()

	assert.Equal(t, catalog.SourceBuiltKeyword, pushSourceLabel(catalog.Reference{Built: true}))
	assert.Equal(t, testPluginsOrigin, pushSourceLabel(catalog.Reference{Remote: testPluginsOrigin}))
}

func TestWritePushDryRun(t *testing.T) {
	t.Parallel()

	ref := catalog.Reference{Kind: catalog.ArtifactKindSolution, Name: "my-sol", Version: semver.MustParse("1.0.0"), Built: true}
	build := pushReferenceBuild{Registry: testPushRegistry, Repository: testPushRepository}

	dryRun := func(t *testing.T, opts *pushKindOptions, ref catalog.Reference, build pushReferenceBuild) string {
		t.Helper()
		ctx, out := newCapturingCtx(t)
		writePushDryRun(writer.FromContext(ctx), opts, ref, build)
		return out.String()
	}

	t.Run("plain push names the destination", func(t *testing.T) {
		t.Parallel()

		got := dryRun(t, pushOpts(catalog.ArtifactKindSolution, "my-sol@1.0.0", ""), ref, build)

		assert.Contains(t, got, "Would push my-sol@1.0.0 (origin: built) to ghcr.io/org/scafctl/solutions/my-sol")
		assert.NotContains(t, got, "--as")
		assert.NotContains(t, got, "--force")
		assert.NotContains(t, got, "--latest")
	})

	t.Run("pulled artifact reports its origin", func(t *testing.T) {
		t.Parallel()

		pulled := catalog.Reference{Kind: catalog.ArtifactKindProvider, Name: "my-plugin", Version: semver.MustParse("1.0.0"), Remote: testPluginsOrigin}

		got := dryRun(t, pushOpts(catalog.ArtifactKindProvider, "my-plugin@1.0.0", ""), pulled, build)

		assert.Contains(t, got, "Would push my-plugin@1.0.0 (origin: "+testPluginsOrigin+") to ghcr.io/org/scafctl/providers/my-plugin")
	})

	t.Run("rename shows the new identity and destination", func(t *testing.T) {
		t.Parallel()

		opts := pushOpts(catalog.ArtifactKindSolution, "my-sol@1.0.0", "")
		opts.TargetName = "renamed@2.0.0"
		renamed := build
		renamed.Target = catalog.Reference{Kind: catalog.ArtifactKindSolution, Name: "renamed", Version: semver.MustParse("2.0.0")}

		got := dryRun(t, opts, ref, renamed)

		assert.Contains(t, got, "ghcr.io/org/scafctl/solutions/renamed")
		assert.Contains(t, got, "--as: would publish as renamed@2.0.0")
	})

	t.Run("force and latest are reported", func(t *testing.T) {
		t.Parallel()

		opts := pushOpts(catalog.ArtifactKindSolution, "my-sol@1.0.0", "")
		opts.Force = true
		opts.Latest = true

		got := dryRun(t, opts, ref, build)

		assert.Contains(t, got, "--force: would overwrite if exists")
		assert.Contains(t, got, `--latest: would tag as "latest"`)
	})
}

// The command tests below go through the real XDG-resolved catalog, so they
// set XDG_DATA_HOME and must not run in parallel.

func TestCommandPushKind_LocalNotFound(t *testing.T) {
	for _, pk := range pushKinds {
		t.Run(string(pk.Kind), func(t *testing.T) {
			newXDGLocalCatalog(t)
			cmd := newPushTestCommand(newCatalogTestCtx(t))
			cmd.SetArgs([]string{string(pk.Kind), "my-artifact@1.0.0", "--catalog", testPushCatalogURL})

			err := cmd.Execute()

			require.Error(t, err)
			assert.Equal(t, exitcode.FileNotFound, exitcode.GetCode(err))
		})
	}
}

func TestCommandPushKind_DryRun(t *testing.T) {
	t.Run("built artifact", func(t *testing.T) {
		cat := newXDGLocalCatalog(t)
		seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.0.0", "")
		ctx, out := newCapturingCtx(t)
		cmd := newPushTestCommand(ctx)
		cmd.SetArgs([]string{"solution", "my-sol@1.0.0", "--catalog", testPushCatalogURL, "--dry-run"})

		require.NoError(t, cmd.Execute())

		assert.Contains(t, out.String(), "Would push my-sol@1.0.0 (origin: built) to ghcr.io/org/scafctl/solutions/my-sol")
	})

	t.Run("open version resolves to the latest local version", func(t *testing.T) {
		cat := newXDGLocalCatalog(t)
		seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.0.0", "")
		seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.1.0", "")
		ctx, out := newCapturingCtx(t)
		cmd := newPushTestCommand(ctx)
		cmd.SetArgs([]string{"solution", "my-sol", "--catalog", testPushCatalogURL, "--dry-run"})

		require.NoError(t, cmd.Execute())

		assert.Contains(t, out.String(), "Would push my-sol@1.1.0")
	})

	t.Run("rename with force", func(t *testing.T) {
		cat := newXDGLocalCatalog(t)
		seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.0.0", "")
		ctx, out := newCapturingCtx(t)
		cmd := newPushTestCommand(ctx)
		cmd.SetArgs([]string{"solution", "my-sol@1.0.0", "--catalog", testPushCatalogURL, "--as", "renamed@2.0.0", "--force", "--dry-run"})

		require.NoError(t, cmd.Execute())

		assert.Contains(t, out.String(), "ghcr.io/org/scafctl/solutions/renamed")
		assert.Contains(t, out.String(), "--as: would publish as renamed@2.0.0")
		assert.Contains(t, out.String(), "--force: would overwrite if exists")
	})

	t.Run("--origin picks the pulled copy", func(t *testing.T) {
		cat := newXDGLocalCatalog(t)
		seedLocal(t, cat, catalog.ArtifactKindProvider, "my-plugin", "1.0.0", "")
		seedLocal(t, cat, catalog.ArtifactKindProvider, "my-plugin", "1.0.0", testPluginsOrigin)
		ctx, out := newCapturingCtx(t)
		cmd := newPushTestCommand(ctx)
		cmd.SetArgs([]string{"provider", "my-plugin@1.0.0", "--origin", testPluginsOrigin, "--catalog", testPushCatalogURL, "--dry-run"})

		require.NoError(t, cmd.Execute())

		assert.Contains(t, out.String(), "(origin: "+testPluginsOrigin+")")
		assert.Contains(t, out.String(), "ghcr.io/org/scafctl/providers/my-plugin")
	})
}

func TestCommandPushKind_AmbiguousOrigin(t *testing.T) {
	cat := newXDGLocalCatalog(t)
	seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.0.0", "")
	seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.0.0", testPluginsOrigin)
	ctx, out := newCapturingCtx(t)
	cmd := newPushTestCommand(ctx)
	cmd.SetArgs([]string{"solution", "my-sol@1.0.0", "--catalog", testPushCatalogURL, "--dry-run"})

	err := cmd.Execute()

	require.Error(t, err)
	assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
	assert.Contains(t, out.String(), "--origin built")
	assert.Contains(t, out.String(), "--origin "+testPluginsOrigin)
}

func TestCommandPushKind_InvalidInput(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantErr  string
		wantCode int
	}{
		{
			name:     "empty origin registry",
			args:     []string{"solution", "my-sol@1.0.0", "--origin", "oci://", "--catalog", testPushCatalogURL},
			wantErr:  "invalid --origin",
			wantCode: exitcode.InvalidInput,
		},
		{
			name:     "digest reference",
			args:     []string{"solution", "my-sol@" + testPushDigest, "--catalog", testPushCatalogURL},
			wantErr:  pushDigestUnsupportedMsg,
			wantCode: exitcode.InvalidInput,
		},
		{
			name:    "rename without version",
			args:    []string{"solution", "my-sol@1.0.0", "--as", "renamed", "--catalog", testPushCatalogURL},
			wantErr: "bare name",
		},
		{
			name:    "full remote reference",
			args:    []string{"solution", "ghcr.io/org/scafctl/solutions/my-sol@1.0.0"},
			wantErr: "full remote references are not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newXDGLocalCatalog(t)
			cmd := newPushTestCommand(newCatalogTestCtx(t))
			cmd.SetArgs(tt.args)

			err := cmd.Execute()

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, exitcode.GetCode(err))
			}
		})
	}
}

func TestCommandPushKind_InheritedSharedFlags(t *testing.T) {
	t.Parallel()

	// Each subtest builds its own command: cmd.Find merges the parent's
	// persistent flags into the command tree, which races when shared.
	for _, pk := range pushKinds {
		t.Run(string(pk.Kind), func(t *testing.T) {
			t.Parallel()
			cmd := newPushTestCommand(newCatalogTestCtx(t))
			sub, _, err := cmd.Find([]string{string(pk.Kind)})
			require.NoError(t, err)

			inherited := sub.InheritedFlags()
			for _, name := range []string{"origin", "as", "force", "dry-run", "insecure", "latest"} {
				assert.NotNil(t, inherited.Lookup(name), "flag --%s", name)
			}
			if pk.Kind == catalog.ArtifactKindSolution {
				assert.NotNil(t, sub.Flags().Lookup("no-sbom"), "flag --no-sbom")
			} else {
				assert.Nil(t, sub.Flags().Lookup("no-sbom"), "flag --no-sbom")
			}
		})
	}
}

// TestCommandPushKind_SharesRootOptions guards against the kind subcommands
// holding a separate options copy: a persistent flag parsed from the root
// command must be visible to the options the kind subcommand runs with.
func TestCommandPushKind_SharesRootOptions(t *testing.T) {
	t.Parallel()

	options := &pushKindOptions{}
	root := &cobra.Command{Use: "push"}
	root.PersistentFlags().BoolVar(&options.DryRun, "dry-run", false, "")
	root.PersistentFlags().BoolVar(&options.Insecure, "insecure", false, "")
	addPushKindCommands(root, settings.NewCliParams(), nil, options)

	sub, _, err := root.Find([]string{"solution"})
	require.NoError(t, err)
	require.NoError(t, sub.ParseFlags([]string{"--dry-run", "--insecure"}))

	assert.True(t, options.DryRun, "--dry-run given after the kind must reach the shared options")
	assert.True(t, options.Insecure, "--insecure given after the kind must reach the shared options")
}

func BenchmarkAddPushKindCommands(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	b.ReportAllocs()
	for b.Loop() {
		parent := &cobra.Command{Use: "push"}
		addPushKindCommands(parent, cliParams, ioStreams, &pushKindOptions{})
	}
}
