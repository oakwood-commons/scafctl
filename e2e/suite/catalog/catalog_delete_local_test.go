// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package catalogtest

import (
	"context"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot-import is the Ginkgo/Gomega house style.
	"github.com/onsi/gomega"

	"github.com/oakwood-commons/scafctl/e2e/internal/utils"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
)

// remoteSolutionExists reports whether the given logical reference is still
// present in the remote repository. It is used to prove that a local --local
// delete evicts only the cached copy and never touches the registry.
func remoteSolutionExists(ctx context.Context, registryAddr, repository string, ref catalog.Reference) bool {
	remote, err := catalog.NewRemoteCatalog(catalog.RemoteCatalogConfig{
		Name:       registryAddr,
		Registry:   registryAddr,
		Repository: repository,
		Insecure:   true,
		Logger:     logr.Discard(),
	})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	exists, err := remote.Exists(ctx, ref)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	return exists
}

var _ = Describe("catalog delete --local", func() {
	var (
		registryAddr string
		isolatedEnv  utils.Isolation
	)

	BeforeEach(func() {
		registryAddr = utils.NewLocalOCIRegistry()
		isolatedEnv = utils.NewIsolatedEnv()
	})

	It("evicts the locally-cached copy of a remote artifact while the registry copy survives", func() {
		ctx := context.Background()

		ref := catalog.Reference{
			Kind:    catalog.ArtifactKindSolution,
			Name:    "shared-solution",
			Version: semver.MustParse("1.0.0"),
		}

		// Seed a solution into the registry and pull it into the isolated local
		// catalog so a locally-cached copy exists to delete.
		seedRemoteSolution(ctx, registryAddr, "source-a", ref,
			[]byte("name: shared-solution\nversion: 1.0.0\nsource: a\n"))

		utils.Scafctl(
			"catalog", "pull", "shared-solution@1.0.0",
			"--catalog", registryAddr+"/source-a",
			"--insecure", "--no-verify",
		).WithEnv(isolatedEnv.Env).Exec()

		localCatalog, err := catalog.NewLocalCatalogAt(isolatedEnv.Root+"/scafctl/catalog", logr.Discard())
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		infos, err := localCatalog.List(ctx, catalog.ArtifactKindSolution, "shared-solution")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(infos).To(gomega.HaveLen(1), "the pulled copy should be present locally before delete")

		// Delete the locally-cached copy by its full remote reference with
		// --local: this must remove the local copy only.
		utils.Scafctl(
			"catalog", "delete",
			registryAddr+"/source-a/solutions/shared-solution@1.0.0",
			"--local",
		).WithEnv(isolatedEnv.Env).Exec()

		// Re-open the catalog so the assertion reads the on-disk index the CLI
		// just wrote, not this test's stale in-memory copy from before delete.
		localCatalog, err = catalog.NewLocalCatalogAt(isolatedEnv.Root+"/scafctl/catalog", logr.Discard())
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		infos, err = localCatalog.List(ctx, catalog.ArtifactKindSolution, "shared-solution")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(infos).To(gomega.BeEmpty(), "the local copy should be gone after --local delete")

		// The registry copy must be untouched.
		gomega.Expect(remoteSolutionExists(ctx, registryAddr, "source-a", ref)).To(gomega.BeTrue(),
			"--local delete must not remove the artifact from the remote registry")
	})
})
