// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0
//go:build (linux && amd64) || (darwin && arm64)

package packagetest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot-import is the Ginkgo/Gomega house style.
	"github.com/onsi/gomega"

	"github.com/oakwood-commons/scafctl/e2e/internal/utils"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
)

func fileDigest(path string) string {
	data, err := os.ReadFile(path)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func assertPluginDigests(lock *bundler.LockFile, linuxBin, darwinBin string) {
	gomega.Expect(lock).NotTo(gomega.BeNil())

	plugin := lock.FindPlugin("test-provider", "provider")
	gomega.Expect(plugin).NotTo(gomega.BeNil(), "expected lock entry for the test-provider plugin")
	gomega.Expect(plugin.Version).To(gomega.Equal("1.0.0"))
	gomega.Expect(plugin.Digests).To(gomega.HaveKey("linux/amd64"))
	gomega.Expect(plugin.Digests).To(gomega.HaveKey("darwin/arm64"))
	gomega.Expect(plugin.Digests["linux/amd64"]).To(gomega.Equal(fileDigest(linuxBin)))
	gomega.Expect(plugin.Digests["darwin/arm64"]).To(gomega.Equal(fileDigest(darwinBin)))

	buildPlatform := runtime.GOOS + "/" + runtime.GOARCH
	gomega.Expect(plugin.Digest).To(gomega.Equal(plugin.Digests[buildPlatform]))
}

func writeTestRegistryConfig(root, registryAddr string) {
	configDir := filepath.Join(root, "scafctl")
	gomega.Expect(os.MkdirAll(configDir, 0o755)).To(gomega.Succeed())

	configPath := filepath.Join(configDir, "config.yaml")
	configText := fmt.Sprintf(`catalogs:
  - name: local
    type: filesystem
  - name: test-registry
    type: oci
    url: oci://%s/scafctl
    insecure: true
settings:
  disableOfficialCatalog: true
`, registryAddr)
	gomega.Expect(os.WriteFile(configPath, []byte(configText), 0o600)).To(gomega.Succeed())
}

var _ = Describe("solution build with external provider", func() {
	var (
		registryAddr string
		isolatedEnv  utils.Isolation
	)

	BeforeEach(func() {
		registryAddr = utils.NewLocalOCIRegistry()
		isolatedEnv = utils.NewIsolatedEnv()
		writeTestRegistryConfig(isolatedEnv.Root, registryAddr)
	})

	It("packages and pushes a provider to a remote registry before building the solution", func() {
		pluginDir := "./e2e/testdata/plugins/providers/my-plugin"
		buildDir := GinkgoT().TempDir()
		linuxBin := filepath.Join(buildDir, "my-plugin-linux-amd64")
		darwinBin := filepath.Join(buildDir, "my-plugin-darwin-arm64")

		utils.BuildPluginBinaryForPlatform(linuxBin, pluginDir, "linux/amd64")
		utils.BuildPluginBinaryForPlatform(darwinBin, pluginDir, "darwin/arm64")

		packageCmd := utils.Scafctl(
			"package", "plugin",
			"--name", "test-provider",
			"--kind", "provider",
			"--version", "1.0.0",
			"--platform", "linux/amd64="+linuxBin,
			"--platform", "darwin/arm64="+darwinBin,
		).WithEnv(isolatedEnv.Env)
		packageSession := packageCmd.Exec()
		gomega.Expect(string(packageSession.Out.Contents())).To(gomega.ContainSubstring("test-provider"))

		pushCmd := utils.Scafctl(
			"catalog", "push",
			"test-provider@1.0.0",
			"--catalog", registryAddr+"/scafctl",
			"--insecure",
		).WithEnv(isolatedEnv.Env)
		pushSession := pushCmd.Exec()
		gomega.Expect(string(pushSession.Out.Contents())).To(gomega.ContainSubstring("test-provider"))

		listCmd := utils.Scafctl(
			"catalog", "list",
			"--catalog", registryAddr+"/scafctl",
			"--insecure",
			"-o", "json",
		).WithEnv(isolatedEnv.Env)
		listSession := listCmd.Exec()
		gomega.Expect(string(listSession.Out.Contents())).To(gomega.ContainSubstring("test-provider"))

		sourceRegistry := registryAddr + "/scafctl"
		solutionPath := filepath.Join(GinkgoT().TempDir(), "solution.yaml")
		solutionText := fmt.Sprintf(`# e2e fixture: solution using a registry-backed test provider.
apiVersion: scafctl.io/v1
kind: Solution

metadata:
  name: external-provider
  version: 1.0.0
  description: A solution using a local test registry provider

spec:
  resolvers:
    testResolver:
      description: testResolver
      type: any
      resolve:
        with:
          - provider: test-provider
            inputs:
              input: hello
bundle:
  plugins:
    - name: test-provider
      kind: provider
      version: 1.0.0
      source:
        registry: %s
        artifact: test-provider
`, sourceRegistry)
		gomega.Expect(os.WriteFile(solutionPath, []byte(solutionText), 0o600)).To(gomega.Succeed())

		buildSolutionCmd := utils.Scafctl(
			"package", "solution",
			"-f", solutionPath,
			"--version", "1.0.0",
			"--debug",
		).WithEnv(isolatedEnv.Env)
		buildSolutionSession := buildSolutionCmd.Exec()
		gomega.Expect(string(buildSolutionSession.Out.Contents())).To(gomega.ContainSubstring("external-provider"))

		pushSolutionCmd := utils.Scafctl(
			"catalog", "push",
			"external-provider@1.0.0",
			"--catalog", registryAddr+"/scafctl",
			"--insecure",
		).WithEnv(isolatedEnv.Env)
		pushSolutionSession := pushSolutionCmd.Exec()
		gomega.Expect(string(pushSolutionSession.Out.Contents())).To(gomega.ContainSubstring("external-provider"))

		lockPath := filepath.Join(filepath.Dir(solutionPath), bundler.DefaultLockFileName)
		_, err := os.Stat(lockPath)
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "expected on-disk solution lock file for external provider")

		lockData, err := os.ReadFile(lockPath)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(string(lockData)).To(gomega.ContainSubstring("test-provider"))

		localCatalog, err := catalog.NewLocalCatalogAt(filepath.Join(isolatedEnv.Root, "scafctl", "catalog"), logr.Discard())
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		localRef := catalog.Reference{
			Kind:    catalog.ArtifactKindSolution,
			Name:    "external-provider",
			Version: semver.MustParse("1.0.0"),
		}
		_, localLayers, localInfo, err := localCatalog.FetchWithLayer(context.Background(), localRef, catalog.MediaTypeSolutionLock)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(localInfo.Reference.Name).To(gomega.Equal("external-provider"))
		gomega.Expect(localLayers).To(gomega.HaveKey(catalog.MediaTypeSolutionLock))
		localLock, err := bundler.ParseLockJSON(localLayers[catalog.MediaTypeSolutionLock])
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		assertPluginDigests(localLock, linuxBin, darwinBin)

		runResolverCmd := utils.Scafctl(
			"run", "resolver",
			"-f", solutionPath,
			"-o", "json",
		).WithEnv(isolatedEnv.Env)
		runResolverSession := runResolverCmd.Exec()
		gomega.Expect(runResolverSession.ExitCode()).To(gomega.Equal(0), string(runResolverSession.Err.Contents()))
		gomega.Expect(string(runResolverSession.Out.Contents())).To(gomega.ContainSubstring("hello"))

		remoteCatalog, err := catalog.NewRemoteCatalog(catalog.RemoteCatalogConfig{
			Name:       "test-registry",
			Registry:   registryAddr,
			Repository: "scafctl",
			Insecure:   true,
			Logger:     logr.Discard(),
		})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		_, remoteLayers, remoteInfo, err := remoteCatalog.FetchWithLayer(context.Background(), localRef, catalog.MediaTypeSolutionLock)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(remoteInfo.Reference.Name).To(gomega.Equal("external-provider"))
		gomega.Expect(remoteLayers).To(gomega.HaveKey(catalog.MediaTypeSolutionLock))
		remoteLock, err := bundler.ParseLockJSON(remoteLayers[catalog.MediaTypeSolutionLock])
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		assertPluginDigests(remoteLock, linuxBin, darwinBin)
	})
})
