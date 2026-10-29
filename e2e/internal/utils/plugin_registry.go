// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/registry"
	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot-import is the Ginkgo/Gomega house style.
	"github.com/onsi/gomega"
)

const pluginBuildTimeout = 2 * time.Minute

// RepoRoot returns the repository root for the checked-out workspace.
func RepoRoot() string {
	_, file, _, ok := runtime.Caller(0)
	gomega.Expect(ok).To(gomega.BeTrue(), "failed to resolve current file path")
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

// manifestDigestPath matches a manifest reference by digest, capturing the
// repository name and the digest.
var manifestDigestPath = regexp.MustCompile(`^/v2/(.+)/manifests/(sha256:[0-9a-f]{64})$`)

// tagPruningHandler wraps an OCI registry handler so that deleting a manifest
// by digest also removes the tags that point at it. The in-memory registry from
// go-containerregistry removes only the digest entry and leaves the tag
// resolvable, which real registries (Docker Distribution, GHCR) do not.
type tagPruningHandler struct {
	inner http.Handler
}

func (h tagPruningHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	match := manifestDigestPath.FindStringSubmatch(r.URL.Path)
	if r.Method != http.MethodDelete || match == nil {
		h.inner.ServeHTTP(w, r)
		return
	}
	repo, digest := match[1], match[2]

	// Collect tags before the digest entry is removed.
	tags := h.tagsForDigest(r.Context(), repo, digest)

	rec := httptest.NewRecorder()
	h.inner.ServeHTTP(rec, r)
	if rec.Code >= http.StatusOK && rec.Code < http.StatusMultipleChoices {
		for _, tag := range tags {
			req := httptest.NewRequestWithContext(r.Context(), http.MethodDelete, "/v2/"+repo+"/manifests/"+tag, nil)
			h.inner.ServeHTTP(httptest.NewRecorder(), req)
		}
	}

	maps.Copy(w.Header(), rec.Header())
	w.WriteHeader(rec.Code)
	_, _ = w.Write(rec.Body.Bytes())
}

// tagsForDigest returns the tags in repo whose manifest digest equals digest.
func (h tagPruningHandler) tagsForDigest(ctx context.Context, repo, digest string) []string {
	listRec := httptest.NewRecorder()
	h.inner.ServeHTTP(listRec, httptest.NewRequestWithContext(ctx, http.MethodGet, "/v2/"+repo+"/tags/list", nil))

	var list struct {
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil {
		return nil
	}

	var matched []string
	for _, tag := range list.Tags {
		headRec := httptest.NewRecorder()
		h.inner.ServeHTTP(headRec, httptest.NewRequestWithContext(ctx, http.MethodHead, "/v2/"+repo+"/manifests/"+tag, nil))
		if headRec.Header().Get("Docker-Content-Digest") == digest {
			matched = append(matched, tag)
		}
	}
	return matched
}

// NewLocalOCIRegistry starts an httptest-backed OCI registry server that is
// compatible with scafctl's remote catalog flow. Deleting a manifest by digest
// also removes the tags that point at it, matching real registries.
func NewLocalOCIRegistry() string {
	srv := httptest.NewTLSServer(tagPruningHandler{inner: registry.New()})
	GinkgoT().Cleanup(srv.Close)

	u, err := url.Parse(srv.URL)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	return u.Host
}

// BuildPluginBinaryForPlatform cross-compiles a plugin fixture for the given
// platform string (for example "linux/amd64") into the destination file path.
func BuildPluginBinaryForPlatform(outputPath, pluginDir, platform string) {
	parts := strings.SplitN(platform, "/", 2)
	gomega.Expect(parts).To(gomega.HaveLen(2), "platform must be GOOS/GOARCH, got %q", platform)

	goos, goarch := parts[0], parts[1]
	gomega.Expect(outputPath).NotTo(gomega.BeEmpty())
	gomega.Expect(pluginDir).NotTo(gomega.BeEmpty())

	gomega.Expect(os.MkdirAll(filepath.Dir(outputPath), 0o755)).To(gomega.Succeed())

	ctx, cancel := context.WithTimeout(context.Background(), pluginBuildTimeout)
	defer cancel()

	repoRoot := RepoRoot()
	pluginBuildArg := filepath.Clean(pluginDir)
	cmdDir := repoRoot
	if relPath, err := filepath.Rel(filepath.Join(repoRoot, "e2e"), filepath.Join(repoRoot, pluginDir)); err == nil && !strings.HasPrefix(relPath, "..") {
		cmdDir = filepath.Join(repoRoot, "e2e")
		pluginBuildArg = "." + string(filepath.Separator) + filepath.Clean(relPath)
	}

	cmd := exec.CommandContext(ctx, "go", "build", "-o", outputPath, pluginBuildArg)
	cmd.Dir = cmdDir
	cmd.Env = append(os.Environ(),
		"CGO_ENABLED=0",
		"GOOS="+goos,
		"GOARCH="+goarch,
	)

	if output, err := cmd.CombinedOutput(); err != nil {
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "failed to build plugin %s for %s: %s", pluginDir, platform, string(output))
	}
}
