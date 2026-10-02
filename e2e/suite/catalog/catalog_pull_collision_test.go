// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package catalogtest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot-import is the Ginkgo/Gomega house style.
	"github.com/onsi/gomega"

	"github.com/oakwood-commons/scafctl/e2e/internal/utils"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
)

// seedRemoteSolution stores a bundle-less solution artifact into the given
// remote repository at the shared logical reference solution/shared-solution.
// The content bytes are unique per source so the resulting manifest digests
// differ, which is what lets us prove the two pulls coexist under distinct
// origin-qualified local tags rather than collapsing into one.
func seedRemoteSolution(ctx context.Context, registryAddr, repository string, ref catalog.Reference, content []byte) {
	remote, err := catalog.NewRemoteCatalog(catalog.RemoteCatalogConfig{
		Name:       registryAddr,
		Registry:   registryAddr,
		Repository: repository,
		Insecure:   true,
		Logger:     logr.Discard(),
	})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// No bundle data: a solution artifact does not require a bundle layer, and
	// the collision scenario only depends on the primary content layer.
	_, err = remote.Store(ctx, ref, content, nil, map[string]string{
		"description": "shared solution from " + repository,
	}, false)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
}

// localTagNames reads the local OCI store's index.json and returns every
// artifact's org.opencontainers.image.ref.name tag. Reading the raw index is
// the only way to observe the fully qualified, origin-qualified tags that the
// remote->local copy writes (List surfaces only the version fragment).
func localTagNames(root string) []string {
	indexPath := filepath.Join(root, "scafctl", "catalog", "index.json")
	data, err := os.ReadFile(indexPath)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	var index ocispec.Index
	gomega.Expect(json.Unmarshal(data, &index)).To(gomega.Succeed())

	var tags []string
	for _, m := range index.Manifests {
		if name := m.Annotations[ocispec.AnnotationRefName]; name != "" {
			tags = append(tags, name)
		}
	}
	return tags
}

var _ = Describe("catalog pull from multiple sources", func() {
	var (
		registryAddr string
		isolatedEnv  utils.Isolation
	)

	BeforeEach(func() {
		registryAddr = utils.NewLocalOCIRegistry()
		isolatedEnv = utils.NewIsolatedEnv()
	})

	It("stores same-name/version solutions from two repositories under distinct origin-qualified tags", func() {
		ctx := context.Background()

		ref := catalog.Reference{
			Kind:    catalog.ArtifactKindSolution,
			Name:    "shared-solution",
			Version: semver.MustParse("1.0.0"),
		}

		// Seed the same logical reference into two separate source repositories
		// backed by one registry, with distinct content so their digests differ.
		seedRemoteSolution(ctx, registryAddr, "source-a", ref,
			[]byte("name: shared-solution\nversion: 1.0.0\nsource: a\n"))
		seedRemoteSolution(ctx, registryAddr, "source-b", ref,
			[]byte("name: shared-solution\nversion: 1.0.0\nsource: b\n"))

		// Pull both into the same isolated local catalog. --no-verify because
		// these seeded artifacts carry no bundle to verify; no --force, so a
		// collision would surface as an "already exists" failure.
		utils.Scafctl(
			"catalog", "pull", "shared-solution@1.0.0",
			"--catalog", registryAddr+"/source-a",
			"--insecure", "--no-verify",
		).WithEnv(isolatedEnv.Env).Exec()

		utils.Scafctl(
			"catalog", "pull", "shared-solution@1.0.0",
			"--catalog", registryAddr+"/source-b",
			"--insecure", "--no-verify",
		).WithEnv(isolatedEnv.Env).Exec()

		// Inspect the shared local catalog directly.
		localPath := filepath.Join(isolatedEnv.Root, "scafctl", "catalog")
		localCatalog, err := catalog.NewLocalCatalogAt(localPath, logr.Discard())
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		infos, err := localCatalog.List(ctx, catalog.ArtifactKindSolution, "shared-solution")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(infos).To(gomega.HaveLen(2),
			"both source pulls should coexist as separate local artifacts")

		// The two artifacts must be distinct blobs and tagged with their origin.
		digests := map[string]struct{}{}
		canonicals := map[string]struct{}{}
		for _, info := range infos {
			digests[info.Digest] = struct{}{}
			canonicals[info.Canonical] = struct{}{}
		}
		gomega.Expect(digests).To(gomega.HaveLen(2), "the two sources should resolve to different digests")
		gomega.Expect(canonicals).To(gomega.HaveKey(registryAddr+"/source-a"),
			"source-a origin should be recorded on one artifact")
		gomega.Expect(canonicals).To(gomega.HaveKey(registryAddr+"/source-b"),
			"source-b origin should be recorded on the other artifact")

		// Both fully qualified, origin-qualified tags must be present in the
		// local OCI index, proving the copy did not overwrite one with the other.
		wantTagA := fmt.Sprintf("%s/source-a/solutions/shared-solution:1.0.0", registryAddr)
		wantTagB := fmt.Sprintf("%s/source-b/solutions/shared-solution:1.0.0", registryAddr)
		gomega.Expect(localTagNames(isolatedEnv.Root)).To(gomega.ContainElements(wantTagA, wantTagB),
			"both origin-qualified tags should exist in the local index")
	})
})

var _ = Describe("catalog pull with --as to rename an artifact", func() {
	var (
		registryAddr string
		isolatedEnv  utils.Isolation
	)

	BeforeEach(func() {
		registryAddr = utils.NewLocalOCIRegistry()
		isolatedEnv = utils.NewIsolatedEnv()
	})

	It("pulls a solution and renames it locally with --as", func() {
		ctx := context.Background()

		ref := catalog.Reference{
			Kind:    catalog.ArtifactKindSolution,
			Name:    "original-solution",
			Version: semver.MustParse("1.0.0"),
		}
		content := []byte("name: original-solution\nversion: 1.0.0\n")
		seedRemoteSolution(ctx, registryAddr, "source", ref, content)

		utils.Scafctl(
			"catalog", "pull", "original-solution@1.0.0",
			"--catalog", registryAddr+"/source",
			"--insecure", "--no-verify", "--as", "renamed-solution",
		).WithEnv(isolatedEnv.Env).Exec()

		localPath := filepath.Join(isolatedEnv.Root, "scafctl", "catalog")
		localCatalog, err := catalog.NewLocalCatalogAt(localPath, logr.Discard())
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		infos, err := localCatalog.List(ctx, catalog.ArtifactKindSolution, "renamed-solution")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(infos).To(gomega.HaveLen(1),
			"the pulled artifact should exist under the new name")

		fetched, _, err := localCatalog.Fetch(ctx, infos[0].Reference)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(fetched).To(gomega.Equal(content),
			"the content of the renamed artifact should match the original")
	})
})

var _ = Describe("catalog pull by fully-qualified name and by short name with --catalog", func() {
	var (
		registryAddr string
		isolatedEnv  utils.Isolation
	)

	BeforeEach(func() {
		registryAddr = utils.NewLocalOCIRegistry()
		isolatedEnv = utils.NewIsolatedEnv()
	})

	It("pulls the same provider both as a full remote reference and as a short name with --catalog", func() {
		ctx := context.Background()

		ref := catalog.Reference{
			Kind:    catalog.ArtifactKindSolution,
			Name:    "original-solution",
			Version: semver.MustParse("1.0.0"),
		}
		repository := "source"
		content := []byte("name: original-solution\nversion: 1.0.0\n")
		seedRemoteSolution(ctx, registryAddr, repository, ref, content)

		// Full remote reference, e.g. ghcr.io/oakwood-commons/solutions/original-solution:1.0.0
		fqn := fmt.Sprintf("%s/solutions/original-solution@1.0.0", fmt.Sprintf("%s/%s", registryAddr, repository))
		utils.Scafctl(
			"catalog", "pull", fqn,
			"--insecure", "--no-verify",
		).WithEnv(isolatedEnv.Env).Exec()

		// Short name resolved against a catalog, e.g. `catalog pull exec:0.6.0 --catalog`.
		utils.Scafctl(
			"catalog", "pull", "original-solution@1.0.0",
			"--catalog", registryAddr+"/"+repository,
			"--insecure", "--no-verify", "--force",
		).WithEnv(isolatedEnv.Env).Exec()

		localPath := filepath.Join(isolatedEnv.Root, "scafctl", "catalog")
		localCatalog, err := catalog.NewLocalCatalogAt(localPath, logr.Discard())
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		infos, err := localCatalog.List(ctx, catalog.ArtifactKindSolution, "original-solution")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(infos).To(gomega.HaveLen(1),
			"both pull forms should resolve to the same local artifact")

		fetched, _, err := localCatalog.Fetch(ctx, infos[0].Reference)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(fetched).To(gomega.Equal(content),
			"the pulled content should match what was seeded remotely")
	})
})
