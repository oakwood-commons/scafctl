// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0
//go:build (linux && amd64) || (darwin && arm64)

package packagetest

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot-import is the Ginkgo/Gomega house style.
	"github.com/onsi/gomega"

	"github.com/oakwood-commons/scafctl/e2e/internal/utils"
)

// currentPlatform is the build platform string (GOOS/GOARCH) for the plugin
// binaries built by this file's tests. The file's build tag restricts it to
// linux/amd64 and darwin/arm64, the two platforms the test provider supports.
func currentPlatform() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

// buildProviderBinary packages and pushes a single-platform test provider
// artifact under name@version to the given registry/repository. Returns the
// path to the built binary (kept alive for the caller's build platform).
func buildProviderBinary(isolatedEnv utils.Isolation, registryAddr, repository, name, version string) string { //nolint:unparam // version is parameterized for test clarity even though current cases all use "1.0.0"
	pluginDir := "./e2e/testdata/plugins/providers/my-plugin"
	bin := filepath.Join(GinkgoT().TempDir(), name+"-bin")
	utils.BuildPluginBinaryForPlatform(bin, pluginDir, currentPlatform())

	packageSession := utils.Scafctl(
		"package", "plugin",
		"--name", name,
		"--kind", "provider",
		"--version", version,
		"--platform", currentPlatform()+"="+bin,
		"--force",
	).WithEnv(isolatedEnv.Env).Exec()
	gomega.Expect(string(packageSession.Out.Contents())).To(gomega.ContainSubstring(name))

	pushSession := utils.Scafctl(
		"catalog", "push",
		fmt.Sprintf("%s@%s", name, version),
		"--catalog", registryAddr+"/"+repository,
		"--insecure",
	).WithEnv(isolatedEnv.Env).Exec()
	gomega.Expect(pushSession.ExitCode()).To(gomega.Equal(0), string(pushSession.Err.Contents()))

	return bin
}

// deleteBuiltProvider removes the locally built copy (kind/name:version, no
// origin) of a provider. Tests that seed the same name+version into multiple
// remote repositories via buildProviderBinary must call this after each push
// so the leftover built copy doesn't win resolution by "prefer built" and mask
// the pulled-copy scenario under test.
func deleteBuiltProvider(isolatedEnv utils.Isolation, name, version string) { //nolint:unparam // version is parameterized for test clarity even though current cases all use "1.0.0"
	session := utils.Scafctl(
		"catalog", "delete",
		fmt.Sprintf("%s@%s", name, version),
		"--kind", "provider",
	).WithEnv(isolatedEnv.Env).Exec()
	gomega.Expect(session.ExitCode()).To(gomega.Equal(0), string(session.Err.Contents()))
}

func pullProvider(isolatedEnv utils.Isolation, registryAddr, repository, name, version string) { //nolint:unparam // version is parameterized for test clarity even though current cases all use "1.0.0"
	session := utils.Scafctl(
		"catalog", "pull",
		fmt.Sprintf("%s@%s", name, version),
		"--catalog", registryAddr+"/"+repository,
		"--kind", "provider",
		"--insecure", "--no-verify",
	).WithEnv(isolatedEnv.Env).Exec()
	gomega.Expect(session.ExitCode()).To(gomega.Equal(0), string(session.Err.Contents()))
}

// writeShortNameSolution writes a solution whose bundle references providerName
// by short (unsourced) name -- no source: block -- so vendoring resolves it
// against whatever is already in the local catalog.
func writeShortNameSolution(dir, name, providerName, version string) string {
	solutionPath := filepath.Join(dir, "solution.yaml")
	text := fmt.Sprintf(`# e2e fixture: solution referencing a provider by short (local) name.
apiVersion: scafctl.io/v1
kind: Solution

metadata:
  name: %[1]s
  version: 1.0.0
  description: solution referencing a local-catalog provider by short name

spec:
  resolvers:
    testResolver:
      description: testResolver
      type: any
      resolve:
        with:
          - provider: %[2]s
            inputs:
              input: hello
bundle:
  plugins:
    - name: %[2]s
      kind: provider
      version: %[3]s
`, name, providerName, version)
	gomega.Expect(os.WriteFile(solutionPath, []byte(text), 0o600)).To(gomega.Succeed())
	return solutionPath
}

var _ = Describe("solution build with local provider resolution", func() {
	var (
		registryAddr string
		isolatedEnv  utils.Isolation
	)

	BeforeEach(func() {
		registryAddr = utils.NewLocalOCIRegistry()
		isolatedEnv = utils.NewIsolatedEnv()
		writeTestRegistryConfig(isolatedEnv.Root, registryAddr)
	})

	// 1. Pull a provider from a single remote catalog, then reference it by
	// short name in a solution's bundle. Vendoring must resolve the sole
	// pulled copy without any ambiguity.
	It("packages a solution referencing a provider pulled from one remote catalog by short name", func() {
		buildProviderBinary(isolatedEnv, registryAddr, "scafctl", "single-origin-provider", "1.0.0")
		deleteBuiltProvider(isolatedEnv, "single-origin-provider", "1.0.0")
		pullProvider(isolatedEnv, registryAddr, "scafctl", "single-origin-provider", "1.0.0")

		solutionPath := writeShortNameSolution(GinkgoT().TempDir(), "short-name-solution", "single-origin-provider", "1.0.0")

		buildSession := utils.Scafctl(
			"package", "solution",
			"-f", solutionPath,
			"--version", "1.0.0",
			"--debug",
		).WithEnv(isolatedEnv.Env).Exec()
		gomega.Expect(buildSession.ExitCode()).To(gomega.Equal(0), string(buildSession.Err.Contents()))
		gomega.Expect(string(buildSession.Out.Contents())).To(gomega.ContainSubstring("short-name-solution"))
	})

	// 2. Seed the SAME provider name/version in two distinct remote
	// repositories (distinct content, so distinct digests), pull both into the
	// local catalog, then reference it by short name. With no built copy to
	// prefer and two different digests, this must fail as ambiguous rather
	// than silently picking one.
	It("fails to package a solution when a short-named provider is ambiguous across two pulled origins", func() {
		buildProviderBinary(isolatedEnv, registryAddr, "repo-a", "ambiguous-provider", "1.0.0")
		deleteBuiltProvider(isolatedEnv, "ambiguous-provider", "1.0.0")
		buildProviderBinary(isolatedEnv, registryAddr, "repo-b", "ambiguous-provider", "1.0.0")
		deleteBuiltProvider(isolatedEnv, "ambiguous-provider", "1.0.0")
		pullProvider(isolatedEnv, registryAddr, "repo-a", "ambiguous-provider", "1.0.0")
		pullProvider(isolatedEnv, registryAddr, "repo-b", "ambiguous-provider", "1.0.0")

		solutionPath := writeShortNameSolution(GinkgoT().TempDir(), "ambiguous-solution", "ambiguous-provider", "1.0.0")

		utils.Scafctl(
			"package", "solution",
			"-f", solutionPath,
			"--version", "1.0.0",
			"--debug",
		).WithEnv(isolatedEnv.Env).ExpectFailure().MatchStderr("ambiguous").Exec()
	})

	// 3. A locally built provider and a pulled provider share the same
	// artifact name (a manifest annotation name collision). The built copy
	// must win outright -- no ambiguity error, and the build must select the
	// built content, not the pulled one.
	It("prefers a locally built provider over a pulled provider with the same name", func() {
		buildProviderBinary(isolatedEnv, registryAddr, "scafctl", "name-clash-provider", "1.0.0")
		deleteBuiltProvider(isolatedEnv, "name-clash-provider", "1.0.0")
		pullProvider(isolatedEnv, registryAddr, "scafctl", "name-clash-provider", "1.0.0")

		// Now author a DIFFERENT build of the same name+version locally
		// (force overwrite is not needed: the built tag is distinct from the
		// origin-qualified pulled tag).
		builtBin := filepath.Join(GinkgoT().TempDir(), "name-clash-provider-built")
		utils.BuildPluginBinaryForPlatform(builtBin, "./e2e/testdata/plugins/providers/my-plugin", currentPlatform())
		packageSession := utils.Scafctl(
			"package", "plugin",
			"--name", "name-clash-provider",
			"--kind", "provider",
			"--version", "1.0.0",
			"--platform", currentPlatform()+"="+builtBin,
			"--force",
		).WithEnv(isolatedEnv.Env).Exec()
		gomega.Expect(packageSession.ExitCode()).To(gomega.Equal(0), string(packageSession.Err.Contents()))

		solutionPath := writeShortNameSolution(GinkgoT().TempDir(), "name-clash-solution", "name-clash-provider", "1.0.0")

		buildSession := utils.Scafctl(
			"package", "solution",
			"-f", solutionPath,
			"--version", "1.0.0",
			"--debug",
		).WithEnv(isolatedEnv.Env).Exec()
		gomega.Expect(buildSession.ExitCode()).To(gomega.Equal(0), string(buildSession.Err.Contents()))
		gomega.Expect(string(buildSession.Out.Contents())).To(gomega.ContainSubstring("name-clash-solution"))
	})
})
