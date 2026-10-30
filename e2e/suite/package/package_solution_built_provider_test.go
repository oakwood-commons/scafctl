// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0
//go:build (linux && amd64) || (darwin && arm64)

package packagetest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot-import is the Ginkgo/Gomega house style.
	"github.com/onsi/gomega"

	"github.com/oakwood-commons/scafctl/e2e/internal/utils"
)

const (
	builtProviderPluginDir = "./e2e/testdata/plugins/providers/my-plugin"
	builtProviderSolution  = `# e2e fixture: solution using a locally built test provider.
apiVersion: scafctl.io/v1
kind: Solution

metadata:
  name: built-provider
  version: 1.0.0
  description: A solution using a locally built test provider

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
        artifact: test-provider
        built: true
`
	builtFlavor  = "built"
	remoteFlavor = "remote"
	builtOutput  = "Processed (" + builtFlavor + "): hello"
	remoteOutput = "Processed (" + remoteFlavor + "): hello"
)

// packageFlavoredPlugin builds the fixture plugin for the host platform with
// the given flavor marker and packages it into env's local catalog.
func packageFlavoredPlugin(env utils.Isolation, flavor string) {
	platform := runtime.GOOS + "/" + runtime.GOARCH
	bin := filepath.Join(GinkgoT().TempDir(), "my-plugin-"+flavor)
	utils.BuildPluginBinaryForPlatform(bin, builtProviderPluginDir, platform, "-X main.flavor="+flavor)

	utils.Scafctl(
		"package", "plugin",
		"--name", "test-provider",
		"--kind", "provider",
		"--version", "1.0.0",
		"--platform", platform+"="+bin,
	).WithEnv(env.Env).Exec()
}

// taggedProviderCopies returns ref.name -> digest for every tagged copy of a
// provider name@version in env's local catalog. It reads the OCI index
// directly because `catalog list` shows only one copy per name@version and
// `catalog inspect` only resolves solutions.
func taggedProviderCopies(env utils.Isolation, name, version string) map[string]string {
	GinkgoHelper()

	data, err := os.ReadFile(env.CatalogIndexPath())
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	var index struct {
		Manifests []struct {
			Digest      string            `json:"digest"`
			Annotations map[string]string `json:"annotations"`
		} `json:"manifests"`
	}
	gomega.Expect(json.Unmarshal(data, &index)).To(gomega.Succeed())

	copies := map[string]string{}
	for _, m := range index.Manifests {
		a := m.Annotations
		ref := a["org.opencontainers.image.ref.name"]
		if ref == "" || a["dev.scafctl.artifact.type"] != "provider" ||
			a["dev.scafctl.artifact.name"] != name || a["org.opencontainers.image.version"] != version {
			continue
		}
		copies[ref] = m.Digest
	}
	return copies
}

var _ = Describe("solution build with a locally built provider", func() {
	var (
		registryAddr string
		env          utils.Isolation
	)

	BeforeEach(func() {
		registryAddr = utils.NewLocalOCIRegistry()
		env = utils.NewIsolatedEnv()
		writeTestRegistryConfig(env.Root, registryAddr)
	})

	It("runs the built copy after the build, even when a pulled copy of the same version exists", func() {
		pluginRef := registryAddr + "/scafctl/providers/test-provider@1.0.0"

		By("publishing a remote copy of test-provider@1.0.0 from a separate environment")
		publisher := utils.NewIsolatedEnv()
		packageFlavoredPlugin(publisher, remoteFlavor)
		utils.Scafctl("catalog", "push", pluginRef, "--insecure").WithEnv(publisher.Env).Exec()

		By("building a local copy and pulling the published one into the same local catalog")
		packageFlavoredPlugin(env, builtFlavor)
		utils.Scafctl("catalog", "pull", pluginRef, "--insecure").WithEnv(env.Env).Exec()

		By("verifying the local catalog holds both copies with different digests")
		copies := taggedProviderCopies(env, "test-provider", "1.0.0")
		builtDigest := copies["provider/test-provider:1.0.0"]
		remoteDigest := copies[registryAddr+"/scafctl/providers/test-provider:1.0.0"]
		gomega.Expect(builtDigest).NotTo(gomega.BeEmpty(), "built copy missing: %v", copies)
		gomega.Expect(remoteDigest).NotTo(gomega.BeEmpty(), "pulled copy missing: %v", copies)
		gomega.Expect(builtDigest).NotTo(gomega.Equal(remoteDigest))

		solutionDir := GinkgoT().TempDir()
		solutionPath := filepath.Join(solutionDir, "solution.yaml")
		gomega.Expect(os.WriteFile(solutionPath, []byte(builtProviderSolution), 0o600)).To(gomega.Succeed())

		By("building the solution")
		utils.Scafctl(
			"package", "solution",
			"-f", solutionPath,
			"--version", "1.0.0",
		).WithEnv(env.Env).Exec()

		assertRunsBuilt := func(args ...string) {
			// The build caches the plugin binary; clear it so each run resolves
			// the plugin through the catalog rather than the binary cache.
			gomega.Expect(os.RemoveAll(env.PluginCacheDir())).To(gomega.Succeed())

			session := utils.Scafctl(append([]string{"run", "resolver"}, args...)...).
				WithEnv(env.Env).WithWorkDir(solutionDir).Exec()
			out := string(session.Out.Contents())
			gomega.Expect(out).To(gomega.ContainSubstring(builtOutput), string(session.Err.Contents()))
			gomega.Expect(out).NotTo(gomega.ContainSubstring(remoteOutput))
		}

		By("running the packaged solution from the catalog (strict lock mode)")
		assertRunsBuilt("built-provider@1.0.0", "-o", "json")

		By("running the solution file against its lock in strict mode")
		assertRunsBuilt("-f", solutionPath, "--lock-mode", "strict", "-o", "json")
	})
})
