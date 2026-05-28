// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// ociRegistry is a minimal in-memory OCI Distribution registry for integration
// testing. It implements enough of the OCI Distribution Spec to support:
//   - GET /v2/ (version check)
//   - GET /v2/_catalog (repository enumeration)
//   - GET /v2/<repo>/tags/list (tag listing)
//   - HEAD/GET /v2/<repo>/manifests/<ref> (manifest retrieval)
//   - HEAD/GET /v2/<repo>/blobs/<digest> (blob retrieval)
//   - PUT /v2/<repo>/manifests/<ref> (manifest push)
//   - POST /v2/<repo>/blobs/uploads/ + PUT (blob push)
//
// This enables integration tests to exercise real OCI catalog code paths
// (push, pull, list, resolve) without network dependencies.
type ociRegistry struct {
	mu sync.RWMutex

	// repos maps "namespace/name" -> tag -> manifest digest
	repos map[string]map[string]string

	// manifests maps digest -> content
	manifests map[string][]byte

	// blobs maps digest -> content
	blobs map[string][]byte

	// uploads maps upload UUID -> accumulated bytes
	uploads map[string][]byte
}

// newOCIRegistry creates an empty in-memory OCI registry.
func newOCIRegistry() *ociRegistry {
	return &ociRegistry{
		repos:     make(map[string]map[string]string),
		manifests: make(map[string][]byte),
		blobs:     make(map[string][]byte),
		uploads:   make(map[string][]byte),
	}
}

// startOCIRegistry starts an httptest.Server backed by the registry.
// The server is automatically closed when the test completes.
// Returns the host:port address (no scheme).
func startOCIRegistry(t *testing.T) (addr string, reg *ociRegistry) {
	t.Helper()
	reg = newOCIRegistry()
	srv := httptest.NewTLSServer(reg)
	t.Cleanup(srv.Close)
	// Strip "https://" prefix to get host:port
	addr = strings.TrimPrefix(srv.URL, "https://")
	return addr, reg
}

// ServeHTTP routes requests to the appropriate handler.
func (r *ociRegistry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	path := req.URL.Path

	// GET /v2/ — version check
	if path == "/v2/" || path == "/v2" {
		w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
		w.WriteHeader(http.StatusOK)
		return
	}

	// GET /v2/_catalog — repository listing
	if path == "/v2/_catalog" && req.Method == http.MethodGet {
		r.handleCatalog(w)
		return
	}

	// Route /v2/<repo>/...
	if strings.HasPrefix(path, "/v2/") {
		// Strip "/v2/" prefix, then parse
		rest := strings.TrimPrefix(path, "/v2/")

		// Check for blob uploads: <repo>/blobs/uploads or <repo>/blobs/uploads/<uuid>
		if idx := strings.Index(rest, "/blobs/uploads"); idx > 0 {
			repo := rest[:idx]
			suffix := rest[idx+len("/blobs/uploads"):]
			r.handleBlobUpload(w, req, repo, suffix)
			return
		}

		// Check for blobs: <repo>/blobs/<digest>
		if idx := strings.Index(rest, "/blobs/"); idx > 0 {
			repo := rest[:idx]
			digest := rest[idx+len("/blobs/"):]
			r.handleBlob(w, req, repo, digest)
			return
		}

		// Check for manifests: <repo>/manifests/<ref>
		if idx := strings.Index(rest, "/manifests/"); idx > 0 {
			repo := rest[:idx]
			ref := rest[idx+len("/manifests/"):]
			r.handleManifest(w, req, repo, ref)
			return
		}

		// Check for tags: <repo>/tags/list
		if idx := strings.Index(rest, "/tags/list"); idx > 0 {
			repo := rest[:idx]
			r.handleTags(w, repo)
			return
		}
	}

	http.NotFound(w, req)
}

// handleCatalog returns the list of repositories.
func (r *ociRegistry) handleCatalog(w http.ResponseWriter) {
	r.mu.RLock()
	repos := make([]string, 0, len(r.repos))
	for name := range r.repos {
		repos = append(repos, name)
	}
	r.mu.RUnlock()

	resp := struct {
		Repositories []string `json:"repositories"`
	}{Repositories: repos}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleTags returns tags for a repository.
func (r *ociRegistry) handleTags(w http.ResponseWriter, repo string) {
	r.mu.RLock()
	tags, ok := r.repos[repo]
	r.mu.RUnlock()

	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errors": []map[string]any{
				{"code": "NAME_UNKNOWN", "message": "repository not found"},
			},
		})
		return
	}

	tagNames := make([]string, 0, len(tags))
	for tag := range tags {
		tagNames = append(tagNames, tag)
	}

	resp := struct {
		Name string   `json:"name"`
		Tags []string `json:"tags"`
	}{Name: repo, Tags: tagNames}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleManifest handles GET/HEAD/PUT for manifests.
func (r *ociRegistry) handleManifest(w http.ResponseWriter, req *http.Request, repo, ref string) {
	switch req.Method {
	case http.MethodGet, http.MethodHead:
		r.getManifest(w, req, repo, ref)
	case http.MethodPut:
		r.putManifest(w, req, repo, ref)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (r *ociRegistry) getManifest(w http.ResponseWriter, req *http.Request, repo, ref string) {
	r.mu.RLock()

	// Resolve tag to digest if ref is not a digest
	digest := ref
	if !strings.HasPrefix(ref, "sha256:") {
		tags, ok := r.repos[repo]
		if !ok {
			r.mu.RUnlock()
			w.WriteHeader(http.StatusNotFound)
			return
		}
		d, ok := tags[ref]
		if !ok {
			r.mu.RUnlock()
			w.WriteHeader(http.StatusNotFound)
			return
		}
		digest = d
	}

	data, ok := r.manifests[digest]
	r.mu.RUnlock()

	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	w.Header().Set("Docker-Content-Digest", digest)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))

	if req.Method == http.MethodGet {
		_, _ = w.Write(data)
	}
}

func (r *ociRegistry) putManifest(w http.ResponseWriter, req *http.Request, repo, ref string) {
	body, err := readAllBody(req.Body)
	if err != nil && err.Error() != "EOF" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(body))

	r.mu.Lock()
	r.manifests[digest] = body
	if _, ok := r.repos[repo]; !ok {
		r.repos[repo] = make(map[string]string)
	}
	r.repos[repo][ref] = digest
	// If ref is a digest, also store as the digest key
	if strings.HasPrefix(ref, "sha256:") {
		r.repos[repo][ref] = ref
	}
	r.mu.Unlock()

	w.Header().Set("Docker-Content-Digest", digest)
	w.WriteHeader(http.StatusCreated)
}

// handleBlob handles GET/HEAD for blobs.
func (r *ociRegistry) handleBlob(w http.ResponseWriter, req *http.Request, _, digest string) {
	r.mu.RLock()
	data, ok := r.blobs[digest]
	r.mu.RUnlock()

	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Docker-Content-Digest", digest)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))

	if req.Method == http.MethodGet {
		_, _ = w.Write(data)
	}
}

// handleBlobUpload handles POST (initiate) and PUT (complete) for blob uploads.
func (r *ociRegistry) handleBlobUpload(w http.ResponseWriter, req *http.Request, repo, suffix string) {
	switch req.Method {
	case http.MethodPost:
		// Initiate upload — check for single-post monolithic upload (digest query param)
		if digest := req.URL.Query().Get("digest"); digest != "" {
			body, _ := readAllBody(req.Body)
			r.mu.Lock()
			r.blobs[digest] = body
			r.mu.Unlock()

			w.Header().Set("Docker-Content-Digest", digest)
			w.WriteHeader(http.StatusCreated)
			return
		}

		// Two-step upload: return upload UUID
		r.mu.Lock()
		uuid := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s-%d", repo, len(r.uploads)))))[:32]
		r.uploads[uuid] = nil
		r.mu.Unlock()

		w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/uploads/%s", repo, uuid))
		w.Header().Set("Docker-Upload-UUID", uuid)
		w.WriteHeader(http.StatusAccepted)

	case http.MethodPut:
		// Complete upload
		uuid := strings.TrimPrefix(suffix, "/")
		body, _ := readAllBody(req.Body)

		digest := req.URL.Query().Get("digest")
		if digest == "" {
			digest = fmt.Sprintf("sha256:%x", sha256.Sum256(body))
		}

		r.mu.Lock()
		// Combine any previously PATCHed data with this final chunk
		if prev, ok := r.uploads[uuid]; ok && len(prev) > 0 {
			body = append(prev, body...)
		}
		r.blobs[digest] = body
		delete(r.uploads, uuid)
		r.mu.Unlock()

		w.Header().Set("Docker-Content-Digest", digest)
		w.WriteHeader(http.StatusCreated)

	case http.MethodPatch:
		// Chunked upload — accumulate data
		uuid := strings.TrimPrefix(suffix, "/")
		body, _ := readAllBody(req.Body)

		r.mu.Lock()
		r.uploads[uuid] = append(r.uploads[uuid], body...)
		r.mu.Unlock()

		w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/uploads/%s", repo, uuid))
		w.Header().Set("Docker-Upload-UUID", uuid)
		w.WriteHeader(http.StatusAccepted)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// readAllBody reads up to 10 MiB from r. Returns what was read even on error.
func readAllBody(r interface{ Read([]byte) (int, error) }) ([]byte, error) {
	const maxBody = 10 << 20
	var buf []byte
	tmp := make([]byte, 4096)
	for len(buf) < maxBody {
		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			return buf, err
		}
	}
	return buf, nil
}
