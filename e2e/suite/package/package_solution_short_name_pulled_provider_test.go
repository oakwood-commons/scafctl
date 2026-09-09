// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0
//go:build linux || darwin

package packagetest

import (
	"context"
	"os"
	"path/filepath"
	"runtime"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot-import is the Ginkgo/Gomega house style.
	"github.com/onsi/gomega"

	"github.com/oakwood-commons/scafctl/e2e/internal/utils"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
)

// This spec exercises the *unsourced* plugin path: a provider is pulled from a
// remote registry into a fresh local catalog, then a solution that references
// it by name/kind/version only (no `source:` block) is built. The build must
// resolve the pulled copy from the local catalog (via VendorPlugins) and pin
// an accurate lock. The sibling external-provider spec covers the complementary
// *sourced* path (a plugin carrying a `source:` block, resolved via
// VendorPluginsFQN).
var _ = Describe("solution build with an unsourced, pulled provider", func() {
	var (
		registryAddr string
		producerEnv  utils.Isolation
		consumerEnv  utils.Isolation
	)

	BeforeEach(func() {
		registryAddr = utils.NewLocalOCIRegistry()
		// Two independent XDG sandboxes sharing one registry: the producer
		// packages and pushes the provider; the consumer pulls it into a
		// pristine local catalog so the build resolves the *pulled* copy (and
		// its remote origin), not a locally-packaged one.
		producerEnv = utils.NewIsolatedEnv()
		consumerEnv = utils.NewIsolatedEnv()
	})

	It("pulls a provider then builds a solution that references it without a source block", func() {
		ctx := context.Background()
		hostPlatform := runtime.GOOS + "/" + runtime.GOARCH

		// --- Producer: build, package, and push the provider to the registry.
		pluginDir := "./e2e/testdata/plugins/providers/my-plugin"
		hostBin := filepath.Join(GinkgoT().TempDir(), "my-plugin-host")
		utils.BuildPluginBinaryForPlatform(hostBin, pluginDir, hostPlatform)

		utils.Scafctl(
			"package", "plugin",
			"--name", "test-provider",
			"--kind", "provider",
			"--version", "1.0.0",
			"--platform", hostPlatform+"="+hostBin,
		).WithEnv(producerEnv.Env).Exec()

		utils.Scafctl(
			"catalog", "push",
			"test-provider@1.0.0",
			"--catalog", registryAddr+"/scafctl",
			"--insecure",
		).WithEnv(producerEnv.Env).Exec()

		// --- Consumer: pull the provider into a fresh local catalog. Provider
		// pulls need no --no-verify (only solution bundles are verified). This
		// is the sole copy in the consumer's catalog and it carries the remote
		// origin recovered from the pushed artifact's annotations.
		utils.Scafctl(
			"catalog", "pull",
			"test-provider@1.0.0",
			"--catalog", registryAddr+"/scafctl",
			"--insecure",
		).WithEnv(consumerEnv.Env).Exec()

		consumerCatalog, err := catalog.NewLocalCatalogAt(
			filepath.Join(consumerEnv.Root, "scafctl", "catalog"),
			logr.Discard(),
		)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		infos, err := consumerCatalog.List(ctx, catalog.ArtifactKindProvider, "test-provider")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(infos).To(gomega.HaveLen(1),
			"exactly one pulled provider copy should exist in the consumer's local catalog")
		gomega.Expect(infos[0].Canonical).To(gomega.Equal(registryAddr+"/scafctl"),
			"the pulled copy should record the remote origin it came from")

		// --- Build an UNSOURCED solution: the plugin is referenced by
		// name/kind/version only, with NO source block, so the build resolves
		// it from the local catalog (the pulled copy) via VendorPlugins.
		solutionPath := filepath.Join(GinkgoT().TempDir(), "solution.yaml")
		solutionText := `# e2e fixture: solution referencing an unsourced, pulled provider.
apiVersion: scafctl.io/v1
kind: Solution

metadata:
  name: unsourced-provider
  version: 1.0.0
  description: A solution using an unsourced provider pulled from a remote registry

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
`
		gomega.Expect(os.WriteFile(solutionPath, []byte(solutionText), 0o600)).To(gomega.Succeed())

		buildSession := utils.Scafctl(
			"package", "solution",
			"-f", solutionPath,
			"--version", "1.0.0",
			"--debug",
		).WithEnv(consumerEnv.Env).Exec()
		gomega.Expect(buildSession.ExitCode()).To(gomega.Equal(0), string(buildSession.Err.Contents()))
		gomega.Expect(string(buildSession.Out.Contents())).To(gomega.ContainSubstring("unsourced-provider"))

		// --- The on-disk lock (YAML) must accurately pin the pulled provider.
		lockPath := filepath.Join(filepath.Dir(solutionPath), bundler.DefaultLockFileName)
		lock, err := bundler.LoadLockFile(lockPath)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(lock).NotTo(gomega.BeNil(), "expected an on-disk solution lock file")

		plugin := lock.FindPlugin("test-provider", "provider")
		gomega.Expect(plugin).NotTo(gomega.BeNil(), "expected a lock entry for the unsourced pulled provider")
		gomega.Expect(plugin.Version).To(gomega.Equal("1.0.0"))
		// `package plugin` with a single --platform publishes an OCI image index
		// with one platform entry, so the lock pins that platform's content
		// digest. Both the per-platform entry and the primary Digest must equal
		// the sha256 of the pulled binary.
		gomega.Expect(plugin.Digests).To(gomega.HaveKeyWithValue(hostPlatform, fileDigest(hostBin)),
			"lock should pin the host platform's content digest to the pulled binary")
		gomega.Expect(plugin.Digest).To(gomega.Equal(fileDigest(hostBin)),
			"primary lock digest should match the build platform's content digest")
		// The lock must attribute the plugin to the remote origin it was
		// pulled from, proving the resolved origin flows into the lock entry.
		gomega.Expect(plugin.ResolvedCanonical).To(gomega.Equal(registryAddr+"/scafctl"),
			"lock should record the remote origin the provider was pulled from")

		// --- Sanity: the pinned provider actually runs, proving the lock points
		// at a usable binary rather than merely being syntactically present.
		runResolverSession := utils.Scafctl(
			"run", "resolver",
			"-f", solutionPath,
			"-o", "json",
		).WithEnv(consumerEnv.Env).Exec()
		gomega.Expect(runResolverSession.ExitCode()).To(gomega.Equal(0), string(runResolverSession.Err.Contents()))
		gomega.Expect(string(runResolverSession.Out.Contents())).To(gomega.ContainSubstring("hello"))
	})
})
