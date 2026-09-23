// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package packagetest

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot-import is the Ginkgo/Gomega house style.
	"github.com/onsi/gomega"

	"github.com/oakwood-commons/scafctl/e2e/internal/utils"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
)

// buildResult mirrors the subset of `scafctl package solution -o json`'s
// output this spec cares about. The full shape is defined by
// builder.BuildResult / packagecmd.SolutionOptions; this is a narrow,
// e2e-owned view so the spec doesn't need an import on the CLI package.
type buildResult struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	Digest     string `json:"digest"`
	DryRun     bool   `json:"dryRun"`
	CacheHit   bool   `json:"cacheHit"`
	InputFiles int    `json:"inputFileCount"`
	Bundle     struct {
		FileCount int `json:"fileCount"`
		Files     []struct {
			Path   string `json:"path"`
			Source string `json:"source"`
		} `json:"files"`
	} `json:"bundle"`
}

var _ = Describe("Solution build", func() {
	When("solution uses only built-in providers and bundle.include", func() {
		var (
			iso          utils.Isolation
			fixturePath  string
			solutionFile string
		)

		BeforeEach(func() {
			iso = utils.NewIsolatedEnv()
			fixturePath = utils.SolutionFixture("builtin-bundle")
			solutionFile = filepath.Join(fixturePath, "solution.yaml")
		})

		It("dry-run reports the expected bundle contents without building", func() {
			session := utils.Scafctl(
				"package", "solution",
				"-f", solutionFile,
				"--version", "1.0.0",
				"--dry-run",
			).WithEnv(iso.Env).Exec()

			out := string(session.Out.Contents())
			gomega.Expect(out).To(gomega.ContainSubstring("templates/deployment.yaml"))
			gomega.Expect(out).To(gomega.ContainSubstring("configs/dev.yaml"))
			gomega.Expect(out).To(gomega.ContainSubstring("configs/prod.yaml"))
			gomega.Expect(out).To(gomega.ContainSubstring("configs/staging.yaml"))
			gomega.Expect(out).To(gomega.ContainSubstring("Dry run: would build builtin-bundle@1.0.0"))
		})

		It("builds successfully and stores an OCI artifact in the local catalog", func() {
			session := utils.Scafctl(
				"package", "solution",
				"-f", solutionFile,
				"--version", "1.0.0",
				"-o", "json",
			).WithEnv(iso.Env).Exec()

			var result buildResult
			gomega.Expect(json.Unmarshal(session.Out.Contents(), &result)).To(gomega.Succeed())

			gomega.Expect(result.Name).To(gomega.Equal("builtin-bundle"))
			gomega.Expect(result.Version).To(gomega.Equal("1.0.0"))
			gomega.Expect(result.DryRun).To(gomega.BeFalse())
			gomega.Expect(result.Digest).To(gomega.HavePrefix("sha256:"))
			gomega.Expect(result.Bundle.FileCount).To(gomega.Equal(5))
		})

		It("catalog inspect reports metadata matching the build output", func() {
			buildSession := utils.Scafctl(
				"package", "solution",
				"-f", solutionFile,
				"--version", "1.0.0",
				"-o", "json",
			).WithEnv(iso.Env).Exec()

			var built buildResult
			gomega.Expect(json.Unmarshal(buildSession.Out.Contents(), &built)).To(gomega.Succeed())

			inspectSession := utils.Scafctl(
				"catalog", "inspect", "builtin-bundle@1.0.0",
				"-o", "yaml",
			).WithEnv(iso.Env).Exec()

			out := string(inspectSession.Out.Contents())
			gomega.Expect(out).To(gomega.ContainSubstring("digest: " + built.Digest))
			gomega.Expect(out).To(gomega.ContainSubstring("name: builtin-bundle"))
			gomega.Expect(out).To(gomega.ContainSubstring("version: 1.0.0"))
			gomega.Expect(out).To(gomega.ContainSubstring("category: developer-tools"))
		})

		It("extract --list-only reports exactly the expected bundled files", func() {
			utils.Scafctl(
				"package", "solution",
				"-f", solutionFile,
				"--version", "1.0.0",
			).WithEnv(iso.Env).Exec()

			session := utils.Scafctl(
				"extract", "bundle", "builtin-bundle@1.0.0",
				"--list-only",
			).WithEnv(iso.Env).Exec()

			out := string(session.Out.Contents())
			for _, expected := range []string{
				"templates/deployment.yaml",
				"configs/dev.yaml",
				"configs/prod.yaml",
				"configs/staging.yaml",
				"configs/test_test.yaml",
			} {
				gomega.Expect(out).To(gomega.ContainSubstring(expected))
			}
			gomega.Expect(out).To(gomega.ContainSubstring("Total: 5 file(s)"))
		})

		It("does not produce a lock file, since there are no plugins or catalog dependencies", func() {
			utils.Scafctl(
				"package", "solution",
				"-f", solutionFile,
				"--version", "1.0.0",
			).WithEnv(iso.Env).Exec()

			// No solution.lock should exist anywhere the build could have
			// written one: alongside the source solution file, or under the
			// isolated catalog root.
			_, err := os.Stat(filepath.Join(fixturePath, "solution.lock"))
			gomega.Expect(os.IsNotExist(err)).To(gomega.BeTrue(), "unexpected solution.lock next to fixture")

			// Every OCI manifest blob's layer media types must exclude the
			// lock media type: only the solution YAML, bundle manifest, and
			// bundle tar layers should be present for a built-in-only
			// solution (confirmed empirically against a real build).
			blobsDir := iso.CatalogBlobsDir()
			entries, err := os.ReadDir(blobsDir)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			foundManifest := false
			for _, entry := range entries {
				data, readErr := os.ReadFile(filepath.Join(blobsDir, entry.Name()))
				gomega.Expect(readErr).NotTo(gomega.HaveOccurred())

				var manifest struct {
					Layers []struct {
						MediaType string `json:"mediaType"`
					} `json:"layers"`
				}
				if json.Unmarshal(data, &manifest) != nil || len(manifest.Layers) == 0 {
					continue // not an OCI manifest blob (e.g. a raw layer/config blob)
				}
				foundManifest = true
				for _, layer := range manifest.Layers {
					gomega.Expect(layer.MediaType).NotTo(
						gomega.ContainSubstring("lock"),
						"unexpected lock layer in manifest for a built-in-only solution",
					)
				}
			}
			gomega.Expect(foundManifest).To(gomega.BeTrue(), "expected to find the solution's OCI manifest blob")
		})

		It("pushes to a remote registry and keeps built-in-only layer composition", func() {
			registryAddr := utils.NewLocalOCIRegistry()

			utils.Scafctl(
				"package", "solution",
				"-f", solutionFile,
				"--version", "1.0.0",
			).WithEnv(iso.Env).Exec()

			pushSession := utils.Scafctl(
				"catalog", "push", "builtin-bundle@1.0.0",
				"--catalog", registryAddr+"/scafctl",
				"--insecure",
			).WithEnv(iso.Env).Exec()
			gomega.Expect(string(pushSession.Out.Contents())).To(gomega.ContainSubstring("builtin-bundle"))

			ref := catalog.Reference{
				Kind:    catalog.ArtifactKindSolution,
				Name:    "builtin-bundle",
				Version: semver.MustParse("1.0.0"),
			}

			localCatalog, err := catalog.NewLocalCatalogAt(filepath.Join(iso.Root, "scafctl", "catalog"), logr.Discard())
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			_, localLayers, localInfo, err := localCatalog.FetchWithLayer(
				context.Background(),
				ref,
				catalog.MediaTypeSolutionBundle,
				catalog.MediaTypeSolutionLock,
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(localInfo.Reference.Name).To(gomega.Equal("builtin-bundle"))
			gomega.Expect(localLayers).To(gomega.HaveKey(catalog.MediaTypeSolutionBundle))
			gomega.Expect(localLayers).NotTo(gomega.HaveKey(catalog.MediaTypeSolutionLock))

			remoteCatalog, err := catalog.NewRemoteCatalog(catalog.RemoteCatalogConfig{
				Name:       "test-registry",
				Registry:   registryAddr,
				Repository: "scafctl",
				Insecure:   true,
				Logger:     logr.Discard(),
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			_, remoteLayers, remoteInfo, err := remoteCatalog.FetchWithLayer(
				context.Background(),
				ref,
				catalog.MediaTypeSolutionBundle,
				catalog.MediaTypeSolutionLock,
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(remoteInfo.Reference.Name).To(gomega.Equal("builtin-bundle"))
			gomega.Expect(remoteLayers).To(gomega.HaveKey(catalog.MediaTypeSolutionBundle))
			gomega.Expect(remoteLayers).NotTo(gomega.HaveKey(catalog.MediaTypeSolutionLock))
		})
	})
})
