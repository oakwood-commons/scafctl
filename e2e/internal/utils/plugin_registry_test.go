// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/registry"
)

const testManifestMediaType = "application/vnd.oci.image.manifest.v1+json"

func putTestManifest(t *testing.T, base, repo, ref, body string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, base+"/v2/"+repo+"/manifests/"+ref, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", testManifestMediaType)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT %s: status %d", ref, resp.StatusCode)
	}
}

func statusOf(t *testing.T, method, url string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", testManifestMediaType)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func TestTagPruningHandler_DeleteByDigestRemovesTags(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(tagPruningHandler{inner: registry.New()})
	t.Cleanup(srv.Close)

	const repo = "scafctl/solutions/demo"
	bodyA := `{"schemaVersion":2,"mediaType":"` + testManifestMediaType + `","config":{"mediaType":"application/vnd.oci.empty.v1+json","size":0,"digest":"sha256:aa"},"layers":[],"annotations":{"v":"a"}}`
	bodyB := `{"schemaVersion":2,"mediaType":"` + testManifestMediaType + `","config":{"mediaType":"application/vnd.oci.empty.v1+json","size":0,"digest":"sha256:aa"},"layers":[],"annotations":{"v":"b"}}`
	sum := sha256.Sum256([]byte(bodyA))
	digestA := "sha256:" + hex.EncodeToString(sum[:])

	putTestManifest(t, srv.URL, repo, "1.0.0", bodyA)
	putTestManifest(t, srv.URL, repo, "1.1.0", bodyB)

	if got := statusOf(t, http.MethodDelete, srv.URL+"/v2/"+repo+"/manifests/"+digestA); got != http.StatusAccepted {
		t.Fatalf("DELETE by digest: status %d, want %d", got, http.StatusAccepted)
	}

	if got := statusOf(t, http.MethodGet, srv.URL+"/v2/"+repo+"/manifests/1.0.0"); got != http.StatusNotFound {
		t.Errorf("deleted tag 1.0.0: status %d, want %d", got, http.StatusNotFound)
	}
	if got := statusOf(t, http.MethodGet, srv.URL+"/v2/"+repo+"/manifests/1.1.0"); got != http.StatusOK {
		t.Errorf("untouched tag 1.1.0: status %d, want %d", got, http.StatusOK)
	}
}

func TestTagPruningHandler_PassesThroughOtherRequests(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(tagPruningHandler{inner: registry.New()})
	t.Cleanup(srv.Close)

	if got := statusOf(t, http.MethodGet, srv.URL+"/v2/"); got != http.StatusOK {
		t.Errorf("GET /v2/: status %d, want %d", got, http.StatusOK)
	}

	missing := "sha256:" + strings.Repeat("0", 64)
	if got := statusOf(t, http.MethodDelete, srv.URL+"/v2/none/manifests/"+missing); got != http.StatusNotFound {
		t.Errorf("DELETE unknown digest: status %d, want %d", got, http.StatusNotFound)
	}
}
