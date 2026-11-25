// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	appconfig "github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandInspect(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandInspect(cliParams, ioStreams, "scafctl/catalog")

	require.NotNil(t, cmd)
	assert.Equal(t, "inspect <name[@version]>", cmd.Use)
	assert.Contains(t, cmd.Aliases, "info")
	assert.Contains(t, cmd.Aliases, "show")
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandInspect_Flags(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandInspect(cliParams, ioStreams, "scafctl/catalog")

	flagTests := []struct {
		name     string
		defValue string
	}{
		{"kind", ""},
		{"catalog", ""},
		{"version", ""},
		{"pre-release", "false"},
		{"referrers", "false"},
		{"artifact-type", ""},
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

func TestCommandInspect_CatalogFlagShorthand(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandInspect(cliParams, ioStreams, "scafctl/catalog")

	f := cmd.Flags().ShorthandLookup("c")
	require.NotNil(t, f, "shorthand -c should exist")
	assert.Equal(t, "catalog", f.Name)
}

func TestCommandInspect_RequiresExactlyOneArg(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandInspect(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts 1 arg(s)")
}

func TestCommandInspect_InvalidReference(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandInspect(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"@@@invalid"})

	err := cmd.Execute()
	require.Error(t, err)
}

func TestCommandInspect_ReferrersRequiresCatalog(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandInspect(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"my-solution@1.0.0", "--referrers"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "catalog required")
}

func TestCommandInspect_ReferrersInvalidCatalog(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandInspect(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	// Provide --catalog but the URL resolution will fail (no config)
	cmd.SetArgs([]string{"my-solution@1.0.0", "--referrers", "--catalog", "nonexistent-catalog"})

	err := cmd.Execute()
	require.Error(t, err)
}

func TestCommandInspect_MultipleArgs(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandInspect(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetArgs([]string{"sol1", "sol2"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts 1 arg(s)")
}

func TestArtifactDetail_Fields(t *testing.T) {
	t.Parallel()

	detail := ArtifactDetail{
		Name:      "my-solution",
		Version:   "1.0.0",
		Kind:      "solution",
		Digest:    "sha256:abc123",
		Size:      1234,
		CreatedAt: "2025-01-01 00:00:00",
		Catalog:   "local",
		Annotations: map[string]string{
			"org.opencontainers.image.title": "my-solution",
		},
	}

	assert.Equal(t, "my-solution", detail.Name)
	assert.Equal(t, "1.0.0", detail.Version)
	assert.Equal(t, "solution", detail.Kind)
	assert.Equal(t, "sha256:abc123", detail.Digest)
	assert.Equal(t, int64(1234), detail.Size)
	assert.Equal(t, "local", detail.Catalog)
	assert.Contains(t, detail.Annotations, "org.opencontainers.image.title")
}

func BenchmarkCommandInspect(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		CommandInspect(cliParams, ioStreams, "scafctl/catalog")
	}
}

func TestCommandInspect_FullOCIRefConflictsWithCatalog(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandInspect(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"ghcr.io/myorg/solutions/my-solution@1.0.0", "--catalog", "my-registry"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conflicting options")
}

func TestCommandInspect_CatalogFlagWithoutRef(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandInspect(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	// --catalog with short name, but no config so ResolveCatalogURL will fail.
	cmd.SetArgs([]string{"my-solution@1.0.0", "--catalog", "nonexistent"})

	err := cmd.Execute()
	require.Error(t, err)
}

func TestCommandInspect_EmbedderBinaryName(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandInspect(cliParams, ioStreams, "mycli/catalog")

	require.NotNil(t, cmd)
	assert.Equal(t, "inspect <name[@version]>", cmd.Use)
	assert.NotNil(t, cmd.RunE)
	assert.Contains(t, cmd.Long, "mycli catalog inspect")
}

func TestCommandInspect_VersionWithReferrersError(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandInspect(cliParams, ioStreams, "scafctl/catalog")
	ctx := newCatalogTestCtx(t)
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"my-solution", "--version", "^1.0.0", "--referrers", "--catalog", "my-registry"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--version cannot be used with --referrers")
}

func TestCommandInspect_VersionConflictWithAtVersion(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandInspect(cliParams, ioStreams, "scafctl/catalog")
	ctx := newCatalogTestCtx(t)
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"my-solution@1.0.0", "--version", "^1.0.0"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot use --version with an explicit version")
}

func TestWriteInspectDetail(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	outputOpts := kvx.NewOutputOptions(ioStreams)
	outputOpts.Format = kvx.OutputFormatJSON

	info := catalog.ArtifactInfo{
		Reference: catalog.Reference{
			Name:    "my-app",
			Kind:    catalog.ArtifactKindSolution,
			Version: semver.MustParse("2.1.0"),
		},
		Digest:    "sha256:abc123",
		Size:      1024,
		CreatedAt: time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC),
		Catalog:   "local",
		Annotations: map[string]string{
			catalog.AnnotationOrigin: "built",
		},
	}

	err := writeInspectDetail(info, outputOpts)
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &result))

	assert.Equal(t, "my-app", result["name"])
	assert.Equal(t, "2.1.0", result["version"])
	assert.Equal(t, "solution", result["kind"])
	assert.Equal(t, "sha256:abc123", result["digest"])
	assert.Equal(t, "built", result["origin"])
	assert.Equal(t, "local", result["catalog"])
}

func TestWriteInspectDetail_NilVersion(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	outputOpts := kvx.NewOutputOptions(ioStreams)
	outputOpts.Format = kvx.OutputFormatJSON

	info := catalog.ArtifactInfo{
		Reference: catalog.Reference{
			Name: "bare-app",
			Kind: catalog.ArtifactKindSolution,
		},
		Digest:    "sha256:def456",
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	err := writeInspectDetail(info, outputOpts)
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &result))
	assert.Equal(t, "bare-app", result["name"])
	assert.Equal(t, "", result["version"])
}

func TestCommandInspect_ValidationErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		args     []string
		wantText string
	}{
		{
			name:     "invalid kind",
			args:     []string{"exec", "--kind", "plugin"},
			wantText: "invalid kind",
		},
		{
			name:     "pre-release with referrers",
			args:     []string{"my-solution@1.0.0", "--pre-release", "--referrers", "--catalog", "my-registry"},
			wantText: "--pre-release cannot be used with --referrers",
		},
		{
			name:     "referrers on local catalog",
			args:     []string{"my-solution@1.0.0", "--referrers", "--catalog", "local"},
			wantText: "--referrers cannot be used with the local catalog",
		},
		{
			name:     "conflicting kind on full OCI reference",
			args:     []string{"ghcr.io/myorg/solutions/my-solution@1.0.0", "--kind", "provider"},
			wantText: "conflicts with artifact kind",
		},
		{
			name:     "version constraint with colon-tagged remote reference",
			args:     []string{"ghcr.io/myorg/solutions/my-solution:1.0.0", "--version", "^1.0.0"},
			wantText: "cannot use --version with an explicit version",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cliParams := settings.NewCliParams()
			ioStreams, _, _ := terminal.NewTestIOStreams()
			cmd := CommandInspect(cliParams, ioStreams, "scafctl/catalog")
			cmd.SetContext(newCatalogTestCtx(t))
			cmd.SetArgs(tc.args)

			err := cmd.Execute()
			require.Error(t, err)
			assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
			assert.Contains(t, err.Error(), tc.wantText)
		})
	}
}

func TestCommandInspect_LocalKindAndPreRelease(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataDir)

	localCat, err := catalog.NewLocalCatalog(logr.Discard())
	require.NoError(t, err)

	store := func(kind catalog.ArtifactKind, name, ver string) {
		t.Helper()
		_, storeErr := localCat.Store(context.Background(), catalog.Reference{
			Kind:    kind,
			Name:    name,
			Version: semver.MustParse(ver),
		}, []byte("content-"+name+"-"+ver), nil, nil, false)
		require.NoError(t, storeErr)
	}

	store(catalog.ArtifactKindProvider, "exec", "0.4.0")
	store(catalog.ArtifactKindProvider, "exec", "0.5.0-dev")
	store(catalog.ArtifactKindAuthHandler, "entra", "0.2.0")
	store(catalog.ArtifactKindSolution, "shared", "1.0.0")
	store(catalog.ArtifactKindProvider, "shared", "1.2.0")

	execInspect := func(args ...string) (map[string]any, string, error) {
		t.Helper()
		var outBuf, errBuf bytes.Buffer
		cliParams := settings.NewCliParams()
		ioStreams := terminal.NewIOStreams(nil, &outBuf, &errBuf, false)
		w := writer.New(ioStreams, cliParams)
		lgr := logr.Discard()
		ctx := logger.WithLogger(writer.WithWriter(context.Background(), w), &lgr)

		cmd := CommandInspect(cliParams, ioStreams, "scafctl/catalog")
		cmd.SetContext(ctx)
		cmd.SetArgs(append(args, "-o", "json"))
		runErr := cmd.Execute()
		if runErr != nil {
			return nil, errBuf.String(), runErr
		}
		var parsed map[string]any
		require.NoError(t, json.Unmarshal(outBuf.Bytes(), &parsed))
		return parsed, errBuf.String(), nil
	}

	// Omitted --kind probes [solution, provider, auth-handler] and finds unique provider/auth-handler.
	got, _, err := execInspect("exec")
	require.NoError(t, err)
	assert.Equal(t, "provider", got["kind"])
	assert.Equal(t, "0.4.0", got["version"], "unpinned without --pre-release prefers stable")

	gotAuth, _, err := execInspect("entra")
	require.NoError(t, err)
	assert.Equal(t, "auth-handler", gotAuth["kind"])
	assert.Equal(t, "0.2.0", gotAuth["version"])

	// --catalog local routes identically to local catalog.
	gotLocalFlag, _, err := execInspect("exec", "--kind", "provider", "--catalog", "local")
	require.NoError(t, err)
	assert.Equal(t, "0.4.0", gotLocalFlag["version"])

	// --pre-release selects 0.5.0-dev for unpinned lookup.
	gotPre, _, err := execInspect("exec", "--kind", "provider", "--pre-release")
	require.NoError(t, err)
	assert.Equal(t, "0.5.0-dev", gotPre["version"])

	// Exact name@0.5.0-dev resolves without --pre-release.
	gotExactPre, _, err := execInspect("exec@0.5.0-dev", "--kind", "provider")
	require.NoError(t, err)
	assert.Equal(t, "0.5.0-dev", gotExactPre["version"])

	// --version range respects --pre-release.
	gotRangeStable, _, err := execInspect("exec", "--kind", "provider", "--version", ">=0.4.0")
	require.NoError(t, err)
	assert.Equal(t, "0.4.0", gotRangeStable["version"])

	gotRangePre, _, err := execInspect("exec", "--kind", "provider", "--version", ">=0.4.0", "--pre-release")
	require.NoError(t, err)
	assert.Equal(t, "0.5.0-dev", gotRangePre["version"])

	// Ambiguous across kinds without --kind returns InvalidInput, both unpinned and with --version.
	_, errOut, err := execInspect("shared")
	require.Error(t, err)
	assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
	assert.Contains(t, errOut, `artifact "shared" matches multiple kinds (solution, provider); specify --kind`)

	_, errOutVer, err := execInspect("shared", "--version", ">=1.0.0")
	require.Error(t, err)
	assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
	assert.Contains(t, errOutVer, `artifact "shared" matches multiple kinds (solution, provider); specify --kind`)

	// Specifying --kind disambiguates cleanly.
	gotDisambig, _, err := execInspect("shared", "--kind", "provider")
	require.NoError(t, err)
	assert.Equal(t, "provider", gotDisambig["kind"])
	assert.Equal(t, "1.2.0", gotDisambig["version"])
}

func TestCommandInspect_RemoteCatalogAndFullOCIRef(t *testing.T) {
	t.Parallel()

	manifestDoc := `{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/json","digest":"sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a","size":2},"layers":[{"mediaType":"application/octet-stream","digest":"sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a","size":2}]}`
	manifestDigest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(manifestDoc)))

	repoTags := map[string][]string{
		"org/providers/exec":  {"0.4.0", "0.5.0-dev"},
		"org/solutions/multi": {"1.0.0"},
		"org/providers/multi": {"1.2.0"},
		"org/kindless-app":    {"2.0.0"},
	}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/v2/")
		if path == "" || r.URL.Path == "/v2" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if strings.Contains(path, "server-error") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if idx := strings.Index(path, "/referrers/"); idx > 0 {
			w.Header().Set("Content-Type", "application/vnd.oci.image.index.v1+json")
			_, _ = w.Write([]byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[]}`))
			return
		}
		if repo, ok := strings.CutSuffix(path, "/tags/list"); ok {
			tags, exists := repoTags[repo]
			if !exists {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"errors":[{"code":"NAME_UNKNOWN"}]}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"name": repo, "tags": tags})
			return
		}
		if idx := strings.Index(path, "/manifests/"); idx > 0 {
			repo := path[:idx]
			ref := path[idx+len("/manifests/"):]
			tags, exists := repoTags[repo]
			if !exists {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			found := ref == manifestDigest
			for _, tg := range tags {
				if tg == ref {
					found = true
					break
				}
			}
			if !found {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
			w.Header().Set("Docker-Content-Digest", manifestDigest)
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(manifestDoc)))
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(manifestDoc))
			}
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	host := strings.TrimPrefix(srv.URL, "https://")
	cfg := &appconfig.Config{
		Catalogs: []appconfig.CatalogConfig{
			{
				Name:     "test-remote",
				Type:     appconfig.CatalogTypeOCI,
				URL:      "oci://" + host + "/org",
				Insecure: true,
			},
		},
	}

	execRemote := func(args ...string) (string, string, error) {
		t.Helper()
		var outBuf, errBuf bytes.Buffer
		cliParams := settings.NewCliParams()
		ioStreams := terminal.NewIOStreams(nil, &outBuf, &errBuf, false)
		w := writer.New(ioStreams, cliParams)
		lgr := logr.Discard()
		ctx := appconfig.WithConfig(logger.WithLogger(writer.WithWriter(context.Background(), w), &lgr), cfg)

		cmd := CommandInspect(cliParams, ioStreams, "scafctl/catalog")
		cmd.SetContext(ctx)
		cmd.SetArgs(append(args, "--insecure"))
		err := cmd.Execute()
		return outBuf.String(), errBuf.String(), err
	}

	// 1. Named remote catalog: omitted --kind resolves unique provider, --pre-release & --version range work.
	out, _, err := execRemote("exec", "--catalog", "test-remote", "-o", "json")
	require.NoError(t, err)
	assert.Contains(t, out, `"kind": "provider"`)
	assert.Contains(t, out, `"version": "0.4.0"`)

	outPre, _, err := execRemote("exec", "--catalog", "test-remote", "--pre-release", "-o", "json")
	require.NoError(t, err)
	assert.Contains(t, outPre, `"version": "0.5.0-dev"`)

	outRangeStable, _, err := execRemote("exec", "--catalog", "test-remote", "--version", ">=0.4.0", "-o", "json")
	require.NoError(t, err)
	assert.Contains(t, outRangeStable, `"version": "0.4.0"`)

	outRangePre, _, err := execRemote("exec", "--catalog", "test-remote", "--version", ">=0.4.0", "--pre-release", "-o", "json")
	require.NoError(t, err)
	assert.Contains(t, outRangePre, `"version": "0.5.0-dev"`)

	// 2. Named remote catalog: multi-kind ambiguity fails with InvalidInput both unpinned and with --version.
	_, errAmbig, err := execRemote("multi", "--catalog", "test-remote")
	require.Error(t, err)
	assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
	assert.Contains(t, errAmbig, `artifact "multi" matches multiple kinds (solution, provider); specify --kind`)

	_, errAmbigVer, err := execRemote("multi", "--catalog", "test-remote", "--version", ">=1.0.0")
	require.Error(t, err)
	assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
	assert.Contains(t, errAmbigVer, `artifact "multi" matches multiple kinds (solution, provider); specify --kind`)

	// 3. Named remote catalog: not-found vs server 500 error propagation (never swallowed into FileNotFound).
	_, _, err = execRemote("missing", "--catalog", "test-remote")
	require.Error(t, err)
	assert.Equal(t, exitcode.FileNotFound, exitcode.GetCode(err))

	_, _, err = execRemote("missing", "--catalog", "test-remote", "--version", ">=1.0.0")
	require.Error(t, err)
	assert.Equal(t, exitcode.FileNotFound, exitcode.GetCode(err))

	_, _, err = execRemote("server-error", "--catalog", "test-remote", "--version", ">=1.0.0")
	require.Error(t, err)
	assert.Equal(t, exitcode.CatalogError, exitcode.GetCode(err))

	// 4. Full OCI reference: kinded path, --version range, and kindless Docker-style fallback.
	outFull, _, err := execRemote(host+"/org/providers/exec", "--version", ">=0.4.0", "--pre-release", "-o", "json")
	require.NoError(t, err)
	assert.Contains(t, outFull, `"version": "0.5.0-dev"`)

	outKindless, _, err := execRemote(host+"/org/kindless-app@2.0.0", "-o", "json")
	require.NoError(t, err)
	assert.Contains(t, outKindless, `"name": "kindless-app"`)
	assert.Contains(t, outKindless, `"version": "2.0.0"`)

	// 5. Referrers with omitted --kind and unpinned reference resolves kind+version first.
	outRef, _, err := execRemote("exec", "--referrers", "--catalog", "test-remote")
	require.NoError(t, err)
	assert.Contains(t, outRef, "No referrers found")
}
