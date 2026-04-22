// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeOCIRegistry is an in-memory OCI Distribution-compliant registry
// backed by httptest.Server, suitable for unit-testing RemoteCatalog methods.
type fakeOCIRegistry struct {
	mu        sync.RWMutex
	blobs     map[string][]byte            // digest -> data
	manifests map[string][]byte            // repo:tag-or-digest -> manifest bytes
	tags      map[string]map[string]string // repo -> tag -> digest
}

func newFakeOCIRegistry() *fakeOCIRegistry {
	return &fakeOCIRegistry{
		blobs:     make(map[string][]byte),
		manifests: make(map[string][]byte),
		tags:      make(map[string]map[string]string),
	}
}

func (r *fakeOCIRegistry) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		path := req.URL.Path

		// GET /v2/ - version check
		if path == "/v2/" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Catalog: GET /v2/_catalog
		if path == "/v2/_catalog" {
			r.handleCatalog(w)
			return
		}

		// Tags list: GET /v2/<repo>/tags/list
		if strings.HasSuffix(path, "/tags/list") {
			r.handleTagsList(w, req, path)
			return
		}

		// Referrers: GET /v2/<repo>/referrers/<digest>
		if strings.Contains(path, "/referrers/") {
			r.handleReferrers(w, req, path)
			return
		}

		// Manifests: /v2/<repo>/manifests/<ref>
		if strings.Contains(path, "/manifests/") {
			r.handleManifests(w, req, path)
			return
		}

		// Blob uploads: POST /v2/<repo>/blobs/uploads/
		if strings.Contains(path, "/blobs/uploads/") && req.Method == http.MethodPost {
			r.handleBlobUploadInit(w, req, path)
			return
		}

		// Blob upload complete: PUT /v2/<repo>/blobs/uploads/<uuid>
		if strings.Contains(path, "/blobs/uploads/") && req.Method == http.MethodPut {
			r.handleBlobUploadComplete(w, req)
			return
		}

		// Blobs: /v2/<repo>/blobs/<digest>
		if strings.Contains(path, "/blobs/") {
			r.handleBlobs(w, req, path)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	})
}

func (r *fakeOCIRegistry) handleCatalog(w http.ResponseWriter) {
	r.mu.RLock()
	repos := make([]string, 0, len(r.tags))
	for repo := range r.tags {
		repos = append(repos, repo)
	}
	r.mu.RUnlock()

	sort.Strings(repos)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
		"repositories": repos,
	})
}

func (r *fakeOCIRegistry) handleTagsList(w http.ResponseWriter, _ *http.Request, path string) {
	// Extract repo from path: /v2/<repo>/tags/list
	repo := strings.TrimPrefix(path, "/v2/")
	repo = strings.TrimSuffix(repo, "/tags/list")

	r.mu.RLock()
	repoTags, ok := r.tags[repo]
	r.mu.RUnlock()

	if !ok {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, `{"errors":[{"code":"NAME_UNKNOWN"}]}`)
		return
	}

	tagList := make([]string, 0, len(repoTags))
	for t := range repoTags {
		tagList = append(tagList, t)
	}
	sort.Strings(tagList)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
		"name": repo,
		"tags": tagList,
	})
}

func (r *fakeOCIRegistry) handleReferrers(w http.ResponseWriter, _ *http.Request, path string) {
	// Extract repo and subject digest: /v2/<repo>/referrers/<digest>
	parts := strings.SplitN(path, "/referrers/", 2)
	repo := strings.TrimPrefix(parts[0], "/v2/")
	subjectDigest := parts[1]

	r.mu.RLock()
	defer r.mu.RUnlock()

	var referrers []ocispec.Descriptor

	// Scan all manifests in this repo for ones with a matching Subject
	prefix := repo + ":"
	for key, data := range r.manifests {
		if !strings.HasPrefix(key, prefix) {
			continue
		}

		var m ocispec.Manifest
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		if m.Subject != nil && m.Subject.Digest.String() == subjectDigest {
			d := digest.FromBytes(data)
			referrers = append(referrers, ocispec.Descriptor{
				MediaType:    ocispec.MediaTypeImageManifest,
				Digest:       d,
				Size:         int64(len(data)),
				ArtifactType: m.Config.MediaType,
				Annotations:  m.Annotations,
			})
		}
	}

	w.Header().Set("Content-Type", ocispec.MediaTypeImageIndex)
	json.NewEncoder(w).Encode(ocispec.Index{ //nolint:errcheck
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: referrers,
	})
}

func (r *fakeOCIRegistry) handleManifests(w http.ResponseWriter, req *http.Request, path string) {
	// Parse: /v2/<repo>/manifests/<reference>
	idx := strings.LastIndex(path, "/manifests/")
	repo := strings.TrimPrefix(path[:idx], "/v2/")
	ref := path[idx+len("/manifests/"):]

	switch req.Method {
	case http.MethodHead, http.MethodGet:
		r.mu.RLock()
		// Try as tag first, then as raw digest
		dgst := ref
		if repoTags, ok := r.tags[repo]; ok {
			if d, found := repoTags[ref]; found {
				dgst = d
			}
		}
		key := repo + ":" + dgst
		data, ok := r.manifests[key]
		r.mu.RUnlock()

		if !ok {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintf(w, `{"errors":[{"code":"MANIFEST_UNKNOWN"}]}`)
			return
		}

		w.Header().Set("Content-Type", ocispec.MediaTypeImageManifest)
		w.Header().Set("Docker-Content-Digest", dgst)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
		if req.Method == http.MethodGet {
			w.Write(data) //nolint:errcheck
		}

	case http.MethodPut:
		data, err := io.ReadAll(req.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		dgst := digest.FromBytes(data).String()

		r.mu.Lock()
		key := repo + ":" + dgst
		r.manifests[key] = data

		if _, ok := r.tags[repo]; !ok {
			r.tags[repo] = make(map[string]string)
		}
		// If ref is a tag (not a digest), map it
		if !strings.HasPrefix(ref, "sha256:") {
			r.tags[repo][ref] = dgst
		}
		r.mu.Unlock()

		w.Header().Set("Docker-Content-Digest", dgst)
		w.WriteHeader(http.StatusCreated)

	case http.MethodDelete:
		r.mu.Lock()
		// Resolve tag to digest if needed
		resolvedRef := ref
		if repoTags, ok := r.tags[repo]; ok {
			if d, found := repoTags[ref]; found {
				resolvedRef = d
			}
		}
		key := repo + ":" + resolvedRef
		if _, ok := r.manifests[key]; !ok {
			r.mu.Unlock()
			w.WriteHeader(http.StatusNotFound)
			return
		}
		delete(r.manifests, key)
		// Remove tag references pointing to this digest
		if repoTags, ok := r.tags[repo]; ok {
			for t, d := range repoTags {
				if d == resolvedRef {
					delete(repoTags, t)
				}
			}
		}
		r.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (r *fakeOCIRegistry) handleBlobUploadInit(w http.ResponseWriter, _ *http.Request, path string) {
	repo := strings.TrimPrefix(path, "/v2/")
	repo = strings.TrimSuffix(repo, "/blobs/uploads/")

	// Return a monolithic upload URL
	uploadID := fmt.Sprintf("%s-upload", repo)
	w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/uploads/%s", repo, uploadID))
	w.Header().Set("Docker-Upload-UUID", uploadID)
	w.WriteHeader(http.StatusAccepted)
}

func (r *fakeOCIRegistry) handleBlobUploadComplete(w http.ResponseWriter, req *http.Request) {
	dgst := req.URL.Query().Get("digest")
	if dgst == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	data, err := io.ReadAll(req.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	r.mu.Lock()
	r.blobs[dgst] = data
	r.mu.Unlock()

	w.Header().Set("Docker-Content-Digest", dgst)
	w.WriteHeader(http.StatusCreated)
}

func (r *fakeOCIRegistry) handleBlobs(w http.ResponseWriter, req *http.Request, path string) {
	// Parse: /v2/<repo>/blobs/<digest>
	idx := strings.LastIndex(path, "/blobs/")
	dgst := path[idx+len("/blobs/"):]

	switch req.Method {
	case http.MethodHead:
		r.mu.RLock()
		data, ok := r.blobs[dgst]
		r.mu.RUnlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
		w.Header().Set("Docker-Content-Digest", dgst)
		w.WriteHeader(http.StatusOK)

	case http.MethodGet:
		r.mu.RLock()
		data, ok := r.blobs[dgst]
		r.mu.RUnlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
		w.Header().Set("Docker-Content-Digest", dgst)
		w.Write(data) //nolint:errcheck

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// newTestRemoteCatalog creates a RemoteCatalog pointing at a fake OCI registry.
// Uses TLS with InsecureSkipVerify because oras-go defaults to HTTPS.
// The caller must call ts.Close() when done (or defer it).
func newTestRemoteCatalog(t *testing.T) (*RemoteCatalog, *httptest.Server) {
	t.Helper()

	reg := newFakeOCIRegistry()
	ts := httptest.NewTLSServer(reg.handler())

	host := strings.TrimPrefix(ts.URL, "https://")
	cat, err := NewRemoteCatalog(RemoteCatalogConfig{
		Name:       "test-catalog",
		Registry:   host,
		Repository: "myorg/artifacts",
		Insecure:   true,
		Logger:     logr.Discard(),
	})
	require.NoError(t, err)

	// Override the auth client's HTTP transport to trust the test server's TLS cert
	cat.client.Client = ts.Client()

	return cat, ts
}

func TestRemoteCatalog_StoreAndFetch(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()
	ref := testRef("my-solution", "1.0.0")
	content := []byte("name: my-solution\nversion: 1.0.0\n")

	// Store
	info, err := cat.Store(ctx, ref, content, nil, nil, false)
	require.NoError(t, err)
	assert.Equal(t, "my-solution", info.Reference.Name)
	assert.Equal(t, "1.0.0", info.Reference.Version.String())
	assert.NotEmpty(t, info.Digest)
	assert.Equal(t, "test-catalog", info.Catalog)
	assert.NotZero(t, info.CreatedAt)

	// Fetch
	data, fetchInfo, err := cat.Fetch(ctx, ref)
	require.NoError(t, err)
	assert.Equal(t, content, data)
	assert.Equal(t, "my-solution", fetchInfo.Reference.Name)
	assert.NotEmpty(t, fetchInfo.Digest)
}

func TestRemoteCatalog_StoreWithBundle(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()
	ref := testRef("bundled-sol", "2.0.0")
	content := []byte("name: bundled-sol\n")
	bundle := []byte("fake-tar-data-here")

	info, err := cat.Store(ctx, ref, content, bundle, map[string]string{
		"custom-key": "custom-value",
	}, false)
	require.NoError(t, err)
	assert.Equal(t, "bundled-sol", info.Reference.Name)
	assert.NotEmpty(t, info.Digest)

	// FetchWithBundle
	fetchContent, fetchBundle, fetchInfo, err := cat.FetchWithBundle(ctx, ref)
	require.NoError(t, err)
	assert.Equal(t, content, fetchContent)
	assert.Equal(t, bundle, fetchBundle)
	assert.NotEmpty(t, fetchInfo.Digest)
}

func TestRemoteCatalog_StoreRejectsExisting(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()
	ref := testRef("dup-sol", "1.0.0")
	content := []byte("name: dup-sol\n")

	_, err := cat.Store(ctx, ref, content, nil, nil, false)
	require.NoError(t, err)

	// Second store without force should fail
	_, err = cat.Store(ctx, ref, content, nil, nil, false)
	require.Error(t, err)
	var existsErr *ArtifactExistsError
	assert.ErrorAs(t, err, &existsErr)
}

func TestRemoteCatalog_StoreForceOverwrites(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()
	ref := testRef("force-sol", "1.0.0")

	_, err := cat.Store(ctx, ref, []byte("v1"), nil, nil, false)
	require.NoError(t, err)

	// Force overwrite
	_, err = cat.Store(ctx, ref, []byte("v2"), nil, nil, true)
	require.NoError(t, err)

	data, _, err := cat.Fetch(ctx, ref)
	require.NoError(t, err)
	assert.Equal(t, []byte("v2"), data)
}

func TestRemoteCatalog_FetchNotFound(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	_, _, err := cat.Fetch(t.Context(), testRef("nonexistent", "1.0.0"))
	require.Error(t, err)
	var notFound *ArtifactNotFoundError
	assert.ErrorAs(t, err, &notFound)
}

func TestRemoteCatalog_FetchWithBundleNoBundleLayer(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()
	ref := testRef("no-bundle", "1.0.0")
	content := []byte("name: no-bundle\n")

	_, err := cat.Store(ctx, ref, content, nil, nil, false)
	require.NoError(t, err)

	fetchContent, fetchBundle, _, err := cat.FetchWithBundle(ctx, ref)
	require.NoError(t, err)
	assert.Equal(t, content, fetchContent)
	assert.Nil(t, fetchBundle)
}

func TestRemoteCatalog_Exists(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()
	ref := testRef("exists-sol", "1.0.0")

	exists, err := cat.Exists(ctx, ref)
	require.NoError(t, err)
	assert.False(t, exists)

	_, err = cat.Store(ctx, ref, []byte("data"), nil, nil, false)
	require.NoError(t, err)

	exists, err = cat.Exists(ctx, ref)
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestRemoteCatalog_Delete(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()
	ref := testRef("del-sol", "1.0.0")

	_, err := cat.Store(ctx, ref, []byte("data"), nil, nil, false)
	require.NoError(t, err)

	err = cat.Delete(ctx, ref)
	require.NoError(t, err)

	exists, err := cat.Exists(ctx, ref)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestRemoteCatalog_DeleteNotFound(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	err := cat.Delete(t.Context(), testRef("ghost", "1.0.0"))
	require.Error(t, err)
	var notFound *ArtifactNotFoundError
	assert.ErrorAs(t, err, &notFound)
}

func TestRemoteCatalog_Resolve_WithVersion(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()
	ref := testRef("resolve-sol", "1.0.0")

	_, err := cat.Store(ctx, ref, []byte("data"), nil, nil, false)
	require.NoError(t, err)

	info, err := cat.Resolve(ctx, ref)
	require.NoError(t, err)
	assert.Equal(t, "resolve-sol", info.Reference.Name)
	assert.NotEmpty(t, info.Digest)
}

func TestRemoteCatalog_Resolve_Latest(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()

	// Store multiple versions
	for _, v := range []string{"1.0.0", "2.0.0", "1.5.0"} {
		ref := testRef("latest-sol", v)
		_, err := cat.Store(ctx, ref, []byte("v"+v), nil, nil, false)
		require.NoError(t, err)
	}

	// Resolve without version should return latest (2.0.0)
	ref := testRef("latest-sol", "")
	info, err := cat.Resolve(ctx, ref)
	require.NoError(t, err)
	assert.Equal(t, "2.0.0", info.Reference.Version.String())
}

func TestRemoteCatalog_Resolve_NotFound(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	_, err := cat.Resolve(t.Context(), testRef("missing", "9.9.9"))
	require.Error(t, err)
	var notFound *ArtifactNotFoundError
	assert.ErrorAs(t, err, &notFound)
}

// TestRemoteCatalog_Resolve_KindlessProbing verifies that Resolve with an
// empty Kind probes across all kind paths (solutions/, providers/, auth-handlers/)
// and returns the correct artifact. This is the pull flow when --kind is omitted.
func TestRemoteCatalog_Resolve_KindlessProbing(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()

	// Store a solution with explicit kind
	ref := Reference{
		Kind:    ArtifactKindSolution,
		Name:    "probe-sol",
		Version: semver.MustParse("1.0.0"),
	}
	_, err := cat.Store(ctx, ref, []byte("probe-data"), nil, nil, false)
	require.NoError(t, err)

	// Resolve with empty kind — should probe and find under solutions/
	kindlessRef := Reference{
		Name:    "probe-sol",
		Version: semver.MustParse("1.0.0"),
	}
	info, err := cat.Resolve(ctx, kindlessRef)
	require.NoError(t, err)
	assert.Equal(t, "probe-sol", info.Reference.Name)
	assert.Equal(t, ArtifactKindSolution, info.Reference.Kind)
	assert.Equal(t, "1.0.0", info.Reference.Version.String())
}

// TestRemoteCatalog_Resolve_KindlessLatest verifies kindless resolve finds
// the latest version across kind probing.
func TestRemoteCatalog_Resolve_KindlessLatest(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()

	// Store multiple versions under provider kind
	for _, v := range []string{"1.0.0", "3.0.0", "2.0.0"} {
		ref := Reference{
			Kind:    ArtifactKindProvider,
			Name:    "probe-prov",
			Version: semver.MustParse(v),
		}
		_, err := cat.Store(ctx, ref, []byte("v"+v), nil, nil, false)
		require.NoError(t, err)
	}

	// Resolve without kind or version — should find latest (3.0.0) under providers/
	kindlessRef := Reference{Name: "probe-prov"}
	info, err := cat.Resolve(ctx, kindlessRef)
	require.NoError(t, err)
	assert.Equal(t, "probe-prov", info.Reference.Name)
	assert.Equal(t, ArtifactKindProvider, info.Reference.Kind)
	assert.Equal(t, "3.0.0", info.Reference.Version.String())
}

func TestRemoteCatalog_ListTags(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()

	for _, v := range []string{"1.0.0", "2.0.0", "3.0.0-rc.1"} {
		ref := testRef("tags-sol", v)
		_, err := cat.Store(ctx, ref, []byte("v"+v), nil, nil, false)
		require.NoError(t, err)
	}

	ref := testRef("tags-sol", "")
	tags, err := cat.ListTags(ctx, ref)
	require.NoError(t, err)
	require.NotEmpty(t, tags)

	// All should be semver
	for _, tag := range tags {
		assert.True(t, tag.IsSemver, "tag %q should be semver", tag.Tag)
	}

	// Should be sorted descending
	if len(tags) >= 2 {
		v0, _ := semver.NewVersion(tags[0].Version)
		v1, _ := semver.NewVersion(tags[1].Version)
		assert.True(t, v0.GreaterThan(v1), "tags should be sorted descending: %s > %s", v0, v1)
	}
}

func TestRemoteCatalog_List_WithName(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()

	for _, v := range []string{"1.0.0", "2.0.0"} {
		ref := testRef("list-sol", v)
		_, err := cat.Store(ctx, ref, []byte("v"+v), nil, nil, false)
		require.NoError(t, err)
	}

	infos, err := cat.List(ctx, ArtifactKindProvider, "list-sol")
	require.NoError(t, err)
	assert.Len(t, infos, 2)
	for _, info := range infos {
		assert.Equal(t, "list-sol", info.Reference.Name)
		assert.Equal(t, ArtifactKindProvider, info.Reference.Kind)
	}
}

func TestRemoteCatalog_List_NoName(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()

	// Listing without name enumerates via _catalog. Empty registry returns empty list.
	infos, err := cat.List(ctx, ArtifactKindProvider, "")
	require.NoError(t, err)
	assert.Empty(t, infos)

	// Store a solution so the registry has something to enumerate
	ref := Reference{Kind: ArtifactKindSolution, Name: "discovered-app", Version: semver.MustParse("1.0.0")}
	_, err = cat.Store(ctx, ref, []byte("content"), nil, nil, false)
	require.NoError(t, err)

	// Listing all kinds should discover it
	infos, err = cat.List(ctx, "", "")
	require.NoError(t, err)
	assert.NotEmpty(t, infos)

	found := false
	for _, info := range infos {
		if info.Reference.Name == "discovered-app" && info.Reference.Kind == ArtifactKindSolution {
			found = true
			break
		}
	}
	assert.True(t, found, "expected to discover 'discovered-app' solution")

	// Listing with kind filter should only return matching kind
	infos, err = cat.List(ctx, ArtifactKindProvider, "")
	require.NoError(t, err)
	assert.Empty(t, infos, "no providers were stored, should be empty")
}

func TestRemoteCatalog_List_AcrossKinds(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()

	// Store a solution and a provider with the same name
	solRef := Reference{Kind: ArtifactKindSolution, Name: "shared-name", Version: semver.MustParse("1.0.0")}
	_, err := cat.Store(ctx, solRef, []byte("sol"), nil, nil, false)
	require.NoError(t, err)

	provRef := Reference{Kind: ArtifactKindProvider, Name: "shared-name", Version: semver.MustParse("2.0.0")}
	_, err = cat.Store(ctx, provRef, []byte("prov"), nil, nil, false)
	require.NoError(t, err)

	// List with empty kind should search across all kinds
	infos, err := cat.List(ctx, "", "shared-name")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(infos), 2, "should find artifacts across multiple kind paths")
}

func TestRemoteCatalog_Tag(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()
	ref := testRef("tag-sol", "1.0.0")

	_, err := cat.Store(ctx, ref, []byte("data"), nil, nil, false)
	require.NoError(t, err)

	// Tag with alias
	oldVersion, err := cat.Tag(ctx, ref, "stable")
	require.NoError(t, err)
	assert.Empty(t, oldVersion, "first alias should have no old version")

	// Verify alias appears in tags
	tags, err := cat.ListTags(ctx, testRef("tag-sol", ""))
	require.NoError(t, err)

	found := false
	for _, tag := range tags {
		if tag.Tag == "stable" {
			found = true
			assert.False(t, tag.IsSemver)
		}
	}
	assert.True(t, found, "stable alias should appear in tags")
}

func TestRemoteCatalog_Tag_ReplacesExisting(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()

	// Store two versions
	ref1 := testRef("retag-sol", "1.0.0")
	_, err := cat.Store(ctx, ref1, []byte("v1"), nil, nil, false)
	require.NoError(t, err)

	ref2 := testRef("retag-sol", "2.0.0")
	_, err = cat.Store(ctx, ref2, []byte("v2"), nil, nil, false)
	require.NoError(t, err)

	// Tag v1 as "latest"
	_, err = cat.Tag(ctx, ref1, "latest")
	require.NoError(t, err)

	// Re-tag v2 as "latest" — should report the old digest changed
	oldVersion, err := cat.Tag(ctx, ref2, "latest")
	require.NoError(t, err)
	assert.NotEmpty(t, oldVersion, "should report old digest when retag changes target")
}

func TestRemoteCatalog_Tag_NotFound(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	_, err := cat.Tag(t.Context(), testRef("ghost", "1.0.0"), "stable")
	require.Error(t, err)
	var notFound *ArtifactNotFoundError
	assert.ErrorAs(t, err, &notFound)
}

func TestRemoteCatalog_Attach_And_Referrers(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()
	ref := testRef("attach-sol", "1.0.0")

	_, err := cat.Store(ctx, ref, []byte("data"), nil, nil, false)
	require.NoError(t, err)

	// Attach an SBOM
	sbomData := []byte(`{"bomFormat":"CycloneDX"}`)
	desc, err := cat.Attach(ctx, ref, "application/vnd.cyclonedx+json", sbomData, map[string]string{
		"description": "test SBOM",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, desc.Digest)

	// List referrers
	referrers, err := cat.Referrers(ctx, ref, "")
	require.NoError(t, err)
	assert.NotEmpty(t, referrers)
}

func TestRemoteCatalog_Attach_NotFound(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	_, err := cat.Attach(t.Context(), testRef("ghost", "1.0.0"), "text/plain", []byte("data"), nil)
	require.Error(t, err)
	var notFound *ArtifactNotFoundError
	assert.ErrorAs(t, err, &notFound)
}

func TestRemoteCatalog_Referrers_NotFound(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	_, err := cat.Referrers(t.Context(), testRef("ghost", "1.0.0"), "")
	require.Error(t, err)
	var notFound *ArtifactNotFoundError
	assert.ErrorAs(t, err, &notFound)
}

func TestRemoteCatalog_StoreAnnotations(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()
	ref := testRef("annotated-sol", "1.0.0")
	annotations := map[string]string{
		AnnotationDescription: "A test solution",
		AnnotationVendor:      "Test Corp",
	}

	info, err := cat.Store(ctx, ref, []byte("data"), nil, annotations, false)
	require.NoError(t, err)

	// The annotations map should include the custom annotations plus the auto-injected ones
	assert.Equal(t, "A test solution", info.Annotations[AnnotationDescription])
	assert.Equal(t, "Test Corp", info.Annotations[AnnotationVendor])
	assert.Equal(t, "provider", info.Annotations[AnnotationArtifactType])
	assert.Equal(t, "annotated-sol", info.Annotations[AnnotationArtifactName])
	assert.Equal(t, "1.0.0", info.Annotations[AnnotationVersion])
	assert.NotEmpty(t, info.Annotations[AnnotationCreated])
}

func TestRemoteCatalog_SolutionKind(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()
	ref := Reference{
		Kind:    ArtifactKindSolution,
		Name:    "my-solution",
		Version: semver.MustParse("0.1.0"),
	}

	_, err := cat.Store(ctx, ref, []byte("name: my-solution\n"), nil, nil, false)
	require.NoError(t, err)

	data, info, err := cat.Fetch(ctx, ref)
	require.NoError(t, err)
	assert.Equal(t, []byte("name: my-solution\n"), data)
	assert.Equal(t, ArtifactKindSolution, info.Reference.Kind)
}

func BenchmarkRemoteCatalog_StoreAndFetch(b *testing.B) {
	reg := newFakeOCIRegistry()
	ts := httptest.NewTLSServer(reg.handler())
	defer ts.Close()

	host := strings.TrimPrefix(ts.URL, "https://")
	cat, err := NewRemoteCatalog(RemoteCatalogConfig{
		Name:       "bench",
		Registry:   host,
		Repository: "myorg/artifacts",
		Insecure:   true,
		Logger:     logr.Discard(),
	})
	require.NoError(b, err)
	cat.client.Client = ts.Client()

	ctx := b.Context()
	content := []byte("name: bench-sol\nversion: 1.0.0\n")

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		ref := Reference{
			Kind:    ArtifactKindSolution,
			Name:    "bench-sol",
			Version: semver.MustParse("1.0.0"),
		}
		_, _ = cat.Store(ctx, ref, content, nil, nil, true)
		_, _, _ = cat.Fetch(ctx, ref)
	}
}

func TestInferKindFromRemote_Found(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()

	// Store a solution artifact
	ref := Reference{
		Kind:    ArtifactKindSolution,
		Name:    "my-sol",
		Version: semver.MustParse("1.0.0"),
	}
	_, err := cat.Store(ctx, ref, []byte("sol data"), nil, nil, false)
	require.NoError(t, err)

	// Infer kind from remote
	kind, err := InferKindFromRemote(ctx, cat, "my-sol", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, ArtifactKindSolution, kind)
}

func TestInferKindFromRemote_NotFound(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	_, err := InferKindFromRemote(t.Context(), cat, "ghost", "1.0.0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found in remote catalog")
}

func TestInferKindFromRemote_Provider(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()

	// Store a provider artifact
	ref := Reference{
		Kind:    ArtifactKindProvider,
		Name:    "my-provider",
		Version: semver.MustParse("2.0.0"),
	}
	_, err := cat.Store(ctx, ref, []byte("provider data"), nil, nil, false)
	require.NoError(t, err)

	kind, err := InferKindFromRemote(ctx, cat, "my-provider", "2.0.0")
	require.NoError(t, err)
	assert.Equal(t, ArtifactKindProvider, kind)
}

func TestRemoteCatalog_Delete_TagFallback(t *testing.T) {
	t.Parallel()

	// Create a fake registry that rejects DELETE-by-digest with a "dangling tag" error
	// but accepts DELETE-by-tag.
	reg := newFakeOCIRegistry()
	origHandler := reg.handler()

	wrapper := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Intercept DELETE on manifests with a digest reference
		if r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/manifests/sha256:") {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"errors":[{"code":"MANIFEST_INVALID","message":"google manifest dangling tag: Manifest is still referenced by one or more tags"}]}`)) //nolint:errcheck
			return
		}
		origHandler.ServeHTTP(w, r)
	})

	ts := httptest.NewTLSServer(wrapper)
	defer ts.Close()

	host := strings.TrimPrefix(ts.URL, "https://")
	cat, err := NewRemoteCatalog(RemoteCatalogConfig{
		Name:       "gcp-test",
		Registry:   host,
		Repository: "myorg/artifacts",
		Insecure:   true,
		Logger:     logr.Discard(),
	})
	require.NoError(t, err)
	cat.client.Client = ts.Client()

	ctx := t.Context()
	ref := Reference{
		Kind:    ArtifactKindSolution,
		Name:    "tagged-sol",
		Version: semver.MustParse("1.0.0"),
	}

	// Store an artifact
	_, err = cat.Store(ctx, ref, []byte("tagged data"), nil, nil, false)
	require.NoError(t, err)

	// Delete should succeed via tag fallback
	err = cat.Delete(ctx, ref)
	require.NoError(t, err)
}

func TestRemoteCatalog_Delete_DigestRef(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()
	ref := Reference{
		Kind:    ArtifactKindSolution,
		Name:    "digest-sol",
		Version: semver.MustParse("1.0.0"),
	}

	// Store an artifact
	info, err := cat.Store(ctx, ref, []byte("digest data"), nil, nil, false)
	require.NoError(t, err)

	// Delete using a digest-based reference (Version == nil)
	digestRef := Reference{
		Kind:   ArtifactKindSolution,
		Name:   "digest-sol",
		Digest: info.Digest,
	}
	err = cat.Delete(ctx, digestRef)
	require.NoError(t, err)
}

func TestRemoteCatalog_Delete_DanglingTag_DigestRef_NoFallback(t *testing.T) {
	t.Parallel()

	// Create a fake registry that rejects digest DELETE with "dangling tag".
	// Since ref.Version is nil, the fallback should NOT fire and we get the original error.
	reg := newFakeOCIRegistry()
	origHandler := reg.handler()

	wrapper := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/manifests/sha256:") {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"errors":[{"code":"MANIFEST_INVALID","message":"dangling tag still referenced"}]}`)) //nolint:errcheck
			return
		}
		origHandler.ServeHTTP(w, r)
	})

	ts := httptest.NewTLSServer(wrapper)
	defer ts.Close()

	host := strings.TrimPrefix(ts.URL, "https://")
	cat, err := NewRemoteCatalog(RemoteCatalogConfig{
		Name:       "digest-test",
		Registry:   host,
		Repository: "myorg/artifacts",
		Insecure:   true,
		Logger:     logr.Discard(),
	})
	require.NoError(t, err)
	cat.client.Client = ts.Client()

	ctx := t.Context()

	// Store with version
	storeRef := Reference{
		Kind:    ArtifactKindSolution,
		Name:    "digest-sol",
		Version: semver.MustParse("1.0.0"),
	}
	info, err := cat.Store(ctx, storeRef, []byte("data"), nil, nil, false)
	require.NoError(t, err)

	// Delete with digest-only ref (Version == nil) -- tag fallback should NOT fire
	digestRef := Reference{
		Kind:   ArtifactKindSolution,
		Name:   "digest-sol",
		Digest: info.Digest,
	}
	err = cat.Delete(ctx, digestRef)
	require.Error(t, err, "digest-based delete should not trigger tag fallback")
	assert.Contains(t, err.Error(), "failed to delete artifact")
}

func TestRemoteCatalog_Delete_ScopeRejection_TagFallback(t *testing.T) {
	t.Parallel()

	// Simulate a Quay-style registry that rejects the "delete" auth scope action
	// with a 400 error on digest-based manifest requests.
	reg := newFakeOCIRegistry()
	origHandler := reg.handler()

	wrapper := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Intercept DELETE on manifests with a digest reference
		if r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/manifests/sha256:") {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"errors":[{"code":"UNAUTHORIZED","message":"Unable to decode repository and actions: repository:repo/name:delete,pull"}]}`)) //nolint:errcheck
			return
		}
		origHandler.ServeHTTP(w, r)
	})

	ts := httptest.NewTLSServer(wrapper)
	defer ts.Close()

	host := strings.TrimPrefix(ts.URL, "https://")
	cat, err := NewRemoteCatalog(RemoteCatalogConfig{
		Name:       "quay-test",
		Registry:   host,
		Repository: "myorg/artifacts",
		Insecure:   true,
		Logger:     logr.Discard(),
	})
	require.NoError(t, err)
	cat.client.Client = ts.Client()

	ctx := t.Context()
	ref := Reference{
		Kind:    ArtifactKindSolution,
		Name:    "quay-sol",
		Version: semver.MustParse("2.0.0"),
	}

	// Store an artifact
	_, err = cat.Store(ctx, ref, []byte("quay data"), nil, nil, false)
	require.NoError(t, err)

	// Delete should succeed via tag fallback when 400+delete is detected
	err = cat.Delete(ctx, ref)
	require.NoError(t, err)

	// Verify it's gone
	_, err = cat.Resolve(ctx, ref)
	assert.True(t, IsNotFound(err))
}

// --- CopyTo tests ---

func TestRemoteCatalog_CopyTo_SingleTag(t *testing.T) {
	t.Parallel()

	remoteCat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()

	// Store an artifact in the remote catalog.
	ref := Reference{Kind: ArtifactKindSolution, Name: "copy-test", Version: semver.MustParse("1.0.0")}
	content := []byte("name: copy-test\nversion: 1.0.0\n")
	_, err := remoteCat.Store(ctx, ref, content, nil, map[string]string{
		AnnotationArtifactName: "copy-test",
		AnnotationArtifactType: "solution",
		AnnotationVersion:      "1.0.0",
	}, false)
	require.NoError(t, err)

	// Create a local catalog to copy into.
	localCat := newTestLocalCatalog(t)

	// Copy from remote to local.
	info, err := remoteCat.CopyTo(ctx, ref, localCat, CopyOptions{})
	require.NoError(t, err)
	assert.Equal(t, "copy-test", info.Reference.Name)

	// List artifacts -- there should be exactly one entry, not two.
	artifacts, err := localCat.List(ctx, ArtifactKindSolution, "copy-test")
	require.NoError(t, err)
	assert.Len(t, artifacts, 1, "CopyTo should produce exactly one local entry, not duplicate tags")

	// Delete should remove it in a single call.
	err = localCat.Delete(ctx, ref)
	require.NoError(t, err)

	artifacts, err = localCat.List(ctx, ArtifactKindSolution, "copy-test")
	require.NoError(t, err)
	assert.Empty(t, artifacts, "artifact should be gone after a single delete")
}

func TestRemoteCatalog_CopyTo_WithBundle(t *testing.T) {
	t.Parallel()

	remoteCat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()

	ref := Reference{Kind: ArtifactKindSolution, Name: "bundled-copy", Version: semver.MustParse("2.0.0")}
	content := []byte("name: bundled-copy\n")
	bundle := []byte("fake-tar-bundle")
	_, err := remoteCat.Store(ctx, ref, content, bundle, map[string]string{
		AnnotationArtifactName: "bundled-copy",
		AnnotationArtifactType: "solution",
		AnnotationVersion:      "2.0.0",
	}, false)
	require.NoError(t, err)

	localCat := newTestLocalCatalog(t)
	_, err = remoteCat.CopyTo(ctx, ref, localCat, CopyOptions{})
	require.NoError(t, err)

	// Verify the content is fetchable from local.
	data, _, fetchInfo, err := localCat.FetchWithBundle(ctx, ref)
	require.NoError(t, err)
	assert.Equal(t, content, data)
	assert.Equal(t, "bundled-copy", fetchInfo.Reference.Name)

	// Single entry, single delete.
	artifacts, err := localCat.List(ctx, ArtifactKindSolution, "bundled-copy")
	require.NoError(t, err)
	assert.Len(t, artifacts, 1)
}

// --- Versionless Fetch tests ---

func TestRemoteCatalog_Fetch_VersionlessResolvesLatest(t *testing.T) {
	t.Parallel()

	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()

	// Store two versions.
	ref1 := Reference{Kind: ArtifactKindSolution, Name: "multi-ver", Version: semver.MustParse("1.0.0")}
	ref2 := Reference{Kind: ArtifactKindSolution, Name: "multi-ver", Version: semver.MustParse("2.0.0")}
	_, err := cat.Store(ctx, ref1, []byte("v1"), nil, nil, false)
	require.NoError(t, err)
	_, err = cat.Store(ctx, ref2, []byte("v2"), nil, nil, false)
	require.NoError(t, err)

	// Fetch without version -- should resolve to 2.0.0.
	versionlessRef := Reference{Kind: ArtifactKindSolution, Name: "multi-ver"}
	data, info, err := cat.Fetch(ctx, versionlessRef)
	require.NoError(t, err)
	assert.Equal(t, []byte("v2"), data)
	assert.Equal(t, "2.0.0", info.Reference.Version.String())
}

func TestRemoteCatalog_FetchWithBundle_VersionlessResolvesLatest(t *testing.T) {
	t.Parallel()

	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()

	ref1 := Reference{Kind: ArtifactKindSolution, Name: "bundled-ver", Version: semver.MustParse("1.0.0")}
	ref2 := Reference{Kind: ArtifactKindSolution, Name: "bundled-ver", Version: semver.MustParse("3.0.0")}
	_, err := cat.Store(ctx, ref1, []byte("v1"), []byte("bundle-v1"), nil, false)
	require.NoError(t, err)
	_, err = cat.Store(ctx, ref2, []byte("v3"), []byte("bundle-v3"), nil, false)
	require.NoError(t, err)

	versionlessRef := Reference{Kind: ArtifactKindSolution, Name: "bundled-ver"}
	data, bundle, info, err := cat.FetchWithBundle(ctx, versionlessRef)
	require.NoError(t, err)
	assert.Equal(t, []byte("v3"), data)
	assert.Equal(t, []byte("bundle-v3"), bundle)
	assert.Equal(t, "3.0.0", info.Reference.Version.String())
}

func TestRemoteCatalog_Fetch_VersionlessNotFound(t *testing.T) {
	t.Parallel()

	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()

	// Fetch a name that doesn't exist at all.
	ref := Reference{Kind: ArtifactKindSolution, Name: "nonexistent"}
	_, _, err := cat.Fetch(ctx, ref)
	require.Error(t, err)
}

func TestRemoteCatalog_CopyTo_SetsOriginAnnotation(t *testing.T) {
	t.Parallel()

	remoteCat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()

	ref := Reference{Kind: ArtifactKindSolution, Name: "origin-pull", Version: semver.MustParse("1.0.0")}
	_, err := remoteCat.Store(ctx, ref, []byte("name: origin-pull"), nil, map[string]string{
		AnnotationArtifactName: "origin-pull",
		AnnotationArtifactType: "solution",
		AnnotationVersion:      "1.0.0",
	}, false)
	require.NoError(t, err)

	localCat := newTestLocalCatalog(t)
	info, err := remoteCat.CopyTo(ctx, ref, localCat, CopyOptions{})
	require.NoError(t, err)

	// Resolve from local and verify origin annotation is present.
	resolved, err := localCat.Resolve(ctx, ref)
	require.NoError(t, err)
	assert.Contains(t, resolved.Annotations[AnnotationOrigin], "pulled from test-catalog")
	assert.Contains(t, resolved.Annotations[AnnotationOrigin], "myorg/artifacts")
	_ = info // ensure CopyTo returned successfully
}
