// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package catalogtest

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot-import is the Ginkgo/Gomega house style.
	"github.com/onsi/gomega"

	"github.com/oakwood-commons/scafctl/e2e/internal/utils"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
)

// remoteRepositories queries the OCI /v2/_catalog endpoint of a test registry
// and returns the repository names it advertises. This is the ground-truth
// view of what paths exist server-side, independent of scafctl's own
// enumeration logic, so we can prove exactly which repository a push created.
func remoteRepositories(ctx context.Context, registryAddr string) []string {
	client := &http.Client{
		Transport: &http.Transport{
			//nolint:gosec // test-only registry served with a self-signed cert.
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("https://%s/v2/_catalog", registryAddr), nil)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	resp, err := client.Do(req)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	defer func() { _ = resp.Body.Close() }()
	gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusOK))

	var payload struct {
		Repositories []string `json:"repositories"`
	}
	gomega.Expect(json.NewDecoder(resp.Body).Decode(&payload)).To(gomega.Succeed())
	return payload.Repositories
}

var _ = Describe("catalog push of a pulled solution", func() {
	var isolatedEnv utils.Isolation

	BeforeEach(func() {
		isolatedEnv = utils.NewIsolatedEnv()
	})

	It("pushes to a different registry under the base name, stripping the origin registry path", func() {
		ctx := context.Background()

		// Two independent registries: the origin we pull from, and the target
		// we push to. They must not share a host so any leaked origin path is
		// unmistakable in the target's repository listing.
		originRegistry := utils.NewLocalOCIRegistry()
		targetRegistry := utils.NewLocalOCIRegistry()

		ref := catalog.Reference{
			Kind:    catalog.ArtifactKindSolution,
			Name:    "portable-solution",
			Version: semver.MustParse("1.0.0"),
		}

		// Seed the solution into the origin registry under a multi-segment
		// repository, giving its origin-qualified local tag a distinctive
		// registry/repo path we can later assert never leaks into the target.
		seedRemoteSolution(ctx, originRegistry, "origin-repo", ref,
			[]byte("name: portable-solution\nversion: 1.0.0\n"))

		// 1. Pull from the origin registry into the isolated local catalog.
		//    --no-verify because the seeded artifact carries no bundle.
		utils.Scafctl(
			"catalog", "pull", "portable-solution@1.0.0",
			"--catalog", originRegistry+"/origin-repo",
			"--insecure", "--no-verify",
		).WithEnv(isolatedEnv.Env).Exec()

		// The local tag must be origin-qualified: it carries the origin
		// registry host and repository path so cross-registry pulls never
		// collide locally.
		originQualifiedTag := fmt.Sprintf("%s/origin-repo/solutions/portable-solution:1.0.0", originRegistry)
		gomega.Expect(localTagNames(isolatedEnv.Root)).To(gomega.ContainElement(originQualifiedTag),
			"pulled artifact should be stored under an origin-qualified local tag")

		// 2. Push the pulled solution to a DIFFERENT registry. --no-sbom keeps
		//    the target repository listing to exactly the pushed artifact.
		utils.Scafctl(
			"catalog", "push", originQualifiedTag,
			"--catalog", targetRegistry,
			"--insecure", "--no-sbom",
		).WithEnv(isolatedEnv.Env).Exec()

		// 3a. The clean, base-name reference must resolve in the target
		//     registry, proving the artifact was pushed under solutions/
		//     portable-solution:1.0.0.
		targetCatalog, err := catalog.NewRemoteCatalog(catalog.RemoteCatalogConfig{
			Name:     targetRegistry,
			Registry: targetRegistry,
			Insecure: true,
			Logger:   logr.Discard(),
		})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		_, err = targetCatalog.Resolve(ctx, ref)
		gomega.Expect(err).NotTo(gomega.HaveOccurred(),
			"the solution should resolve under its base name in the target registry")

		// 3b. Server-side ground truth: the target registry must expose ONLY
		//     the clean base repository. Any nesting of the origin registry
		//     host or "origin-repo" path would mean the origin qualifier leaked
		//     into the remote tag/path.
		repos := remoteRepositories(ctx, targetRegistry)
		gomega.Expect(repos).To(gomega.ConsistOf("solutions/portable-solution"),
			"target registry should hold exactly the base-name repository, with no origin qualifier")
		for _, r := range repos {
			gomega.Expect(r).NotTo(gomega.ContainSubstring("origin-repo"),
				"target repository path must not contain the origin repository path")
			gomega.Expect(r).NotTo(gomega.ContainSubstring(originRegistry),
				"target repository path must not contain the origin registry host")
		}
	})
})
