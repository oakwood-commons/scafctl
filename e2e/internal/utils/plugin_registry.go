// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
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

// NewLocalOCIRegistry starts an httptest-backed OCI registry server that is
// compatible with scafctl's remote catalog flow.
func NewLocalOCIRegistry() string {
	srv := httptest.NewTLSServer(registry.New())
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
