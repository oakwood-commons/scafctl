// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0
//go:build linux || darwin

package packagetest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot-import is the Ginkgo/Gomega house style.
	"github.com/onsi/gomega"

	"github.com/oakwood-commons/scafctl/e2e/internal/utils"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
)

// This spec is the negative counterpart to the short-named-pulled-provider spec:
// when the local catalog holds the same name+version provider pulled from two
// different origins (and no local build to break the tie), an *unsourced*
// solution reference cannot pick one deterministically. The build must fail
// with an ambiguous-reference error listing the origin-qualified alternatives,
// rather than silently guessing an origin.
var _ = Describe("solution build with an ambiguous provider short name reference", func() {
	var (
		registryAddr string
		producerEnv  utils.Isolation
		consumerEnv  utils.Isolation
	)

	BeforeEach(func() {
		registryAddr = utils.NewLocalOCIRegistry()
		producerEnv = utils.NewIsolatedEnv()
		consumerEnv = utils.NewIsolatedEnv()
	})

	It("fails with an ambiguous-reference error when two origins provide the same provider", func() {
		ctx := context.Background()
		hostPlatform := runtime.GOOS + "/" + runtime.GOARCH

		// --- Producer: package the provider once, then push it to two distinct
		// repositories on the shared registry. Same name+version, two origins.
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

		originA := registryAddr + "/repo-a"
		originB := registryAddr + "/repo-b"
		for _, origin := range []string{originA, originB} {
			utils.Scafctl(
				"catalog", "push",
				"test-provider@1.0.0",
				"--catalog", origin,
				"--insecure",
			).WithEnv(producerEnv.Env).Exec()
		}

		// --- Consumer: pull the provider from BOTH origins into one pristine
		// local catalog. With no local build present, the two remote-origin
		// copies coexist and neither wins by precedence.
		for _, origin := range []string{originA, originB} {
			utils.Scafctl(
				"catalog", "pull",
				"test-provider@1.0.0",
				"--catalog", origin,
				"--insecure",
			).WithEnv(consumerEnv.Env).Exec()
		}

		// Both copies must exist locally, each tagged with its own origin.
		consumerCatalog, err := catalog.NewLocalCatalogAt(
			filepath.Join(consumerEnv.Root, "scafctl", "catalog"),
			logr.Discard(),
		)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		infos, err := consumerCatalog.List(ctx, catalog.ArtifactKindProvider, "test-provider")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(infos).To(gomega.HaveLen(2),
			"both origin pulls should coexist as separate local artifacts")
		canonicals := map[string]struct{}{}
		for _, info := range infos {
			canonicals[info.Canonical] = struct{}{}
		}
		gomega.Expect(canonicals).To(gomega.HaveKey(originA), "repo-a origin should be recorded")
		gomega.Expect(canonicals).To(gomega.HaveKey(originB), "repo-b origin should be recorded")

		// --- Build an UNSOURCED solution: the plugin is referenced by
		// name/kind/version only. Resolution matches two copies from different
		// origins and must refuse to guess.
		solutionPath := filepath.Join(GinkgoT().TempDir(), "solution.yaml")
		solutionText := `# e2e fixture: unsourced reference to an ambiguous provider.
apiVersion: scafctl.io/v1
kind: Solution

metadata:
  name: ambiguous-provider
  version: 1.0.0
  description: A solution whose unsourced provider exists under two origins

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
		).WithEnv(consumerEnv.Env).ExpectFailure().Exec()

		stderr := string(buildSession.Err.Contents())
		gomega.Expect(stderr).To(gomega.ContainSubstring("ambiguous"),
			"failure should name the ambiguity so the user can disambiguate")
		// The error must list both origin-qualified candidates so the user can
		// copy one to disambiguate.
		gomega.Expect(stderr).To(gomega.ContainSubstring(
			fmt.Sprintf("%s/repo-a/providers/test-provider:1.0.0", registryAddr),
		))
		gomega.Expect(stderr).To(gomega.ContainSubstring(
			fmt.Sprintf("%s/repo-b/providers/test-provider:1.0.0", registryAddr),
		))

		// No lock file should be written on a failed build.
		lockPath := filepath.Join(filepath.Dir(solutionPath), "solution.lock")
		_, statErr := os.Stat(lockPath)
		gomega.Expect(os.IsNotExist(statErr)).To(gomega.BeTrue(),
			"a failed build must not leave a lock file behind")
	})
})
