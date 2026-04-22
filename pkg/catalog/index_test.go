// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	orasauth "oras.land/oras-go/v2/registry/remote/auth"
)

// buildTestIndexServer creates an httptest.Server that serves a catalog-index
// OCI artifact with the given artifacts. Returns the server and the
// registry host (without scheme) for use in RemoteCatalog.
func buildTestIndexServer(t testing.TB, artifacts []DiscoveredArtifact) *httptest.Server {
	t.Helper()

	index := Index{Artifacts: artifacts}
	layerData, err := json.Marshal(index)
	require.NoError(t, err)

	layerDigest := digest.FromBytes(layerData)
	layerDesc := ocispec.Descriptor{
		MediaType: "application/vnd.scafctl.catalog-index.v1+json",
		Digest:    layerDigest,
		Size:      int64(len(layerData)),
	}

	manifest := ocispec.Manifest{
		MediaType: ocispec.MediaTypeImageManifest,
		Layers:    []ocispec.Descriptor{layerDesc},
	}
	manifestData, err := json.Marshal(manifest)
	require.NoError(t, err)

	manifestDigest := digest.FromBytes(manifestData)

	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// HEAD /v2/<repo>/manifests/<tag> — resolve
		if r.Method == http.MethodHead && strings.HasSuffix(path, "/manifests/"+catalogIndexTag) {
			w.Header().Set("Content-Type", ocispec.MediaTypeImageManifest)
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(manifestData)))
			w.Header().Set("Docker-Content-Digest", manifestDigest.String())
			w.WriteHeader(http.StatusOK)
			return
		}

		// GET /v2/<repo>/manifests/<digest> — fetch manifest
		if r.Method == http.MethodGet && strings.Contains(path, "/manifests/") {
			w.Header().Set("Content-Type", ocispec.MediaTypeImageManifest)
			w.Header().Set("Docker-Content-Digest", manifestDigest.String())
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(manifestData)
			return
		}

		// GET /v2/<repo>/blobs/<digest> — fetch layer
		if r.Method == http.MethodGet && strings.Contains(path, "/blobs/") {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Docker-Content-Digest", layerDigest.String())
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(layerData)
			return
		}

		http.NotFound(w, r)
	}))
}

func TestFetchCatalogIndex_Success(t *testing.T) {
	t.Parallel()

	artifacts := []DiscoveredArtifact{
		{Kind: ArtifactKindSolution, Name: "hello-world"},
		{Kind: ArtifactKindSolution, Name: "starter-kit"},
		{Kind: ArtifactKindProvider, Name: "terraform"},
	}

	srv := buildTestIndexServer(t, artifacts)
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "https://")
	cat := &RemoteCatalog{
		name:     "test",
		registry: host,
		client:   &orasauth.Client{Client: srv.Client()},
		logger:   logr.Discard(),
	}

	result, err := cat.fetchCatalogIndex(t.Context())
	require.NoError(t, err)
	assert.Len(t, result, 3)
	assert.Equal(t, "hello-world", result[0].Name)
	assert.Equal(t, ArtifactKindSolution, result[0].Kind)
	assert.Equal(t, "starter-kit", result[1].Name)
	assert.Equal(t, "terraform", result[2].Name)
}

func TestFetchCatalogIndex_WithRepository(t *testing.T) {
	t.Parallel()

	artifacts := []DiscoveredArtifact{
		{Kind: ArtifactKindSolution, Name: "my-app"},
	}

	srv := buildTestIndexServer(t, artifacts)
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "https://")
	cat := &RemoteCatalog{
		name:       "test",
		registry:   host,
		repository: "oakwood-commons",
		client:     &orasauth.Client{Client: srv.Client()},
		logger:     logr.Discard(),
	}

	result, err := cat.fetchCatalogIndex(t.Context())
	require.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "my-app", result[0].Name)
}

func TestFetchCatalogIndex_NotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "https://")
	cat := &RemoteCatalog{
		name:     "test",
		registry: host,
		client:   &orasauth.Client{Client: srv.Client()},
		logger:   logr.Discard(),
	}

	_, err := cat.fetchCatalogIndex(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolving catalog index")
}

func TestFetchCatalogIndex_InvalidJSON(t *testing.T) {
	t.Parallel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			badData := []byte("not-json")
			d := digest.FromBytes(badData)
			w.Header().Set("Content-Type", ocispec.MediaTypeImageManifest)
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(badData)))
			w.Header().Set("Docker-Content-Digest", d.String())
			w.WriteHeader(http.StatusOK)
			return
		}
		badData := []byte("not-json")
		d := digest.FromBytes(badData)
		w.Header().Set("Content-Type", ocispec.MediaTypeImageManifest)
		w.Header().Set("Docker-Content-Digest", d.String())
		_, _ = w.Write(badData)
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "https://")
	cat := &RemoteCatalog{
		name:     "test",
		registry: host,
		client:   &orasauth.Client{Client: srv.Client()},
		logger:   logr.Discard(),
	}

	_, err := cat.fetchCatalogIndex(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing catalog index manifest")
}

func TestBuildIndexRepositoryPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		registry   string
		repository string
		want       string
	}{
		{
			name:     "registry only",
			registry: "ghcr.io",
			want:     "ghcr.io/catalog-index",
		},
		{
			name:       "registry with repository",
			registry:   "ghcr.io",
			repository: "oakwood-commons",
			want:       "ghcr.io/oakwood-commons/catalog-index",
		},
		{
			name:       "nested repository",
			registry:   "ghcr.io",
			repository: "oakwood-commons/scafctl",
			want:       "ghcr.io/oakwood-commons/scafctl/catalog-index",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cat := &RemoteCatalog{
				registry:   tt.registry,
				repository: tt.repository,
			}
			assert.Equal(t, tt.want, cat.buildIndexRepositoryPath())
		})
	}
}

// errEnumerator always returns the given error.
type errEnumerator struct {
	err error
}

func (e *errEnumerator) enumerate(_ context.Context) ([]string, error) {
	return nil, e.err
}

func TestListRepositories_FallsBackToIndex(t *testing.T) {
	t.Parallel()

	artifacts := []DiscoveredArtifact{
		{Kind: ArtifactKindSolution, Name: "hello-world"},
		{Kind: ArtifactKindProvider, Name: "terraform"},
	}

	srv := buildTestIndexServer(t, artifacts)
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "https://")
	cat := &RemoteCatalog{
		name:     "test",
		registry: host,
		client:   &orasauth.Client{Client: srv.Client()},
		logger:   logr.Discard(),
		enumerator: &errEnumerator{
			err: ErrEnumerationNotSupported,
		},
	}

	result, err := cat.ListRepositories(t.Context())
	require.NoError(t, err)
	require.Len(t, result, 2)
	assert.Equal(t, "hello-world", result[0].Name)
	assert.Equal(t, "terraform", result[1].Name)
}

func TestListRepositories_FallbackFailsReturnsOriginalError(t *testing.T) {
	t.Parallel()

	// No index server — fallback will fail.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "https://")
	cat := &RemoteCatalog{
		name:     "test",
		registry: host,
		client:   &orasauth.Client{Client: srv.Client()},
		logger:   logr.Discard(),
		enumerator: &errEnumerator{
			err: ErrEnumerationNotSupported,
		},
	}

	_, err := cat.ListRepositories(t.Context())
	require.Error(t, err)
	assert.True(t, IsEnumerationNotSupported(err))
}

func TestListRepositories_NonEnumerationErrorNoFallback(t *testing.T) {
	t.Parallel()

	cat := &RemoteCatalog{
		name:     "test",
		registry: "localhost:0",
		client:   &orasauth.Client{},
		logger:   logr.Discard(),
		enumerator: &errEnumerator{
			err: fmt.Errorf("network error"),
		},
	}

	_, err := cat.ListRepositories(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "network error")
	assert.False(t, IsEnumerationNotSupported(err))
}

func TestListRepositories_IndexStrategy_SkipsEnumeration(t *testing.T) {
	t.Parallel()

	artifacts := []DiscoveredArtifact{
		{Kind: ArtifactKindSolution, Name: "from-index"},
	}

	srv := buildTestIndexServer(t, artifacts)
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "https://")
	cat := &RemoteCatalog{
		name:              "test",
		registry:          host,
		client:            &orasauth.Client{Client: srv.Client()},
		logger:            logr.Discard(),
		discoveryStrategy: "index",
		// enumerator would succeed, but should never be called
		enumerator: &errEnumerator{err: fmt.Errorf("should not be called")},
	}

	result, err := cat.ListRepositories(t.Context())
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "from-index", result[0].Name)
}

func TestListRepositories_APIStrategy_NoFallback(t *testing.T) {
	t.Parallel()

	// With api strategy, enumeration not supported should NOT fall back to index.
	cat := &RemoteCatalog{
		name:              "test",
		registry:          "localhost:0",
		client:            &orasauth.Client{},
		logger:            logr.Discard(),
		discoveryStrategy: "api",
		enumerator: &errEnumerator{
			err: ErrEnumerationNotSupported,
		},
	}

	_, err := cat.ListRepositories(t.Context())
	require.Error(t, err)
	assert.True(t, IsEnumerationNotSupported(err))
}

func TestListRepositories_AutoStrategy_FallsBackToIndex(t *testing.T) {
	t.Parallel()

	artifacts := []DiscoveredArtifact{
		{Kind: ArtifactKindSolution, Name: "hello-world"},
	}

	srv := buildTestIndexServer(t, artifacts)
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "https://")
	cat := &RemoteCatalog{
		name:              "test",
		registry:          host,
		client:            &orasauth.Client{Client: srv.Client()},
		logger:            logr.Discard(),
		discoveryStrategy: "auto",
		enumerator: &errEnumerator{
			err: ErrEnumerationNotSupported,
		},
	}

	result, err := cat.ListRepositories(t.Context())
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "hello-world", result[0].Name)
}

func BenchmarkFetchCatalogIndex(b *testing.B) {
	artifacts := make([]DiscoveredArtifact, 50)
	for i := range artifacts {
		artifacts[i] = DiscoveredArtifact{
			Kind: ArtifactKindSolution,
			Name: fmt.Sprintf("app-%d", i),
		}
	}

	srv := buildTestIndexServer(b, artifacts)
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "https://")
	cat := &RemoteCatalog{
		name:     "bench",
		registry: host,
		client:   &orasauth.Client{Client: srv.Client()},
		logger:   logr.Discard(),
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = cat.fetchCatalogIndex(context.Background())
	}
}
