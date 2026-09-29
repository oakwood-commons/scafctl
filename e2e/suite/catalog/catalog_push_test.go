// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package catalogtest

import (
	"context"
	"path/filepath"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot-import is the Ginkgo/Gomega house style.
	"github.com/onsi/gomega"

	"github.com/oakwood-commons/scafctl/e2e/internal/utils"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
)

// seedLocalSolution stores a bundle-less solution artifact directly into the
// isolated environment's local catalog (XDG_DATA_HOME/scafctl/catalog), which
// is exactly where the CLI's `catalog push` reads from. Storing via the
// LocalCatalog API mirrors what `scafctl build` would produce, minus the
// solution compilation, keeping the test focused on the push path.
func seedLocalSolution(ctx context.Context, root string, ref catalog.Reference, content []byte) {
	localPath := filepath.Join(root, "scafctl", "catalog")
	local, err := catalog.NewLocalCatalogAt(localPath, logr.Discard())
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// No bundle data: a solution artifact does not require a bundle layer, and
	// the push scenario only depends on the primary content layer travelling
	// to the remote intact.
	_, err = local.Store(ctx, ref, content, nil, map[string]string{
		"description": "local solution to push",
	}, false)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
}

// assertRemoteCatalog builds a RemoteCatalog client for reading back what the
// push wrote, so assertions inspect the registry through the same code path
// production uses rather than poking at registry internals.
func assertRemoteCatalog(registryAddr, repository string) *catalog.RemoteCatalog {
	remote, err := catalog.NewRemoteCatalog(catalog.RemoteCatalogConfig{
		Name:       registryAddr,
		Registry:   registryAddr,
		Repository: repository,
		Insecure:   true,
		Logger:     logr.Discard(),
	})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	return remote
}

var _ = Describe("catalog push to a remote registry", func() {
	var (
		registryAddr string
		isolatedEnv  utils.Isolation
	)

	BeforeEach(func() {
		registryAddr = utils.NewLocalOCIRegistry()
		isolatedEnv = utils.NewIsolatedEnv()
	})

	It("pushes a local solution to the remote registry under its own name", func() {
		ctx := context.Background()

		ref := catalog.Reference{
			Kind:    catalog.ArtifactKindSolution,
			Name:    "my-solution",
			Version: semver.MustParse("1.0.0"),
		}
		content := []byte("name: my-solution\nversion: 1.0.0\n")
		seedLocalSolution(ctx, isolatedEnv.Root, ref, content)

		// --no-sbom keeps the push deterministic (no SBOM layer generation);
		// no --force, so a pre-existing artifact would surface as a failure.
		utils.Scafctl(
			"catalog", "push", "my-solution@1.0.0",
			"--catalog", registryAddr+"/target-repo",
			"--insecure", "--no-sbom",
		).WithEnv(isolatedEnv.Env).Exec()

		// The artifact must be resolvable in the remote at solutions/my-solution
		// and its content layer must match what was pushed.
		remote := assertRemoteCatalog(registryAddr, "target-repo")
		info, err := remote.Resolve(ctx, ref)
		gomega.Expect(err).NotTo(gomega.HaveOccurred(),
			"pushed solution should resolve in the remote registry")
		gomega.Expect(info.Reference.Name).To(gomega.Equal("my-solution"))

		fetched, _, err := remote.Fetch(ctx, ref)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(fetched).To(gomega.Equal(content),
			"remote content layer should match the pushed bytes")
	})
})
