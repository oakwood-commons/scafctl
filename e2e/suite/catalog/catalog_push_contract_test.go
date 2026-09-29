// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package catalogtest

import (
	"context"
	"path/filepath"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/onsi/gomega/gexec"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot-import is the Ginkgo/Gomega house style.
	"github.com/onsi/gomega"

	"github.com/oakwood-commons/scafctl/e2e/internal/utils"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
)

// Push contract specs. These lock in the user-visible push contracts
// (temp/catalog-push-short-name-contracts.md): --catalog is always the
// destination, the positional reference always selects the local source,
// selection is resolved exactly once, ambiguity is an error, and --as
// relocates. Assertions check remote state, exit codes, and stable output
// fragments; resolution permutations are covered by pkg/catalog unit tests.

const (
	contractSrcRepo  = "src"
	contractDestRepo = "dest"
	contractName     = "x"
	contractVersion  = "1.0.0"
)

func contractRef(kind catalog.ArtifactKind, name string) catalog.Reference {
	return catalog.Reference{Kind: kind, Name: name, Version: semver.MustParse(contractVersion)}
}

func contractLocalCatalog(root string) *catalog.LocalCatalog {
	local, err := catalog.NewLocalCatalogAt(filepath.Join(root, "scafctl", "catalog"), logr.Discard())
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	return local
}

// seedBuilt stores a locally built artifact and returns its digest.
func seedBuilt(ctx context.Context, root string, ref catalog.Reference, content []byte) string {
	info, err := contractLocalCatalog(root).Store(ctx, ref, content, nil, nil, false)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	return info.Digest
}

// seedRemote stores an artifact directly in a remote repository (bypassing
// the push command under test) and returns its digest.
func seedRemote(ctx context.Context, registryAddr, repository string, ref catalog.Reference, content []byte, force bool) string { //nolint:unparam // repository is parameterized for test clarity even though current cases all use contractSrcRepo
	info, err := assertRemoteCatalog(registryAddr, repository).Store(ctx, ref, content, nil, nil, force)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	return info.Digest
}

// remoteDigest returns the digest at ref in the repository, or "" when the
// artifact does not exist.
func remoteDigest(ctx context.Context, registryAddr, repository string, ref catalog.Reference) string {
	info, err := assertRemoteCatalog(registryAddr, repository).Resolve(ctx, ref)
	if err != nil {
		gomega.Expect(catalog.IsNotFound(err)).To(gomega.BeTrue(), "unexpected remote error: %v", err)
		return ""
	}
	return info.Digest
}

// localDigestFrom returns the digest of the local copy of ref whose source
// canonical is canonical ("" for locally built).
func localDigestFrom(ctx context.Context, root string, ref catalog.Reference, canonical string) string {
	infos, err := contractLocalCatalog(root).List(ctx, ref.Kind, ref.Name)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	for _, info := range infos {
		if info.Canonical == canonical && info.Reference.Version != nil && info.Reference.Version.Equal(ref.Version) {
			return info.Digest
		}
	}
	Fail("no local copy of " + ref.Name + " from " + canonical)
	return ""
}

func combinedOutput(s *gexec.Session) string {
	return string(s.Out.Contents()) + string(s.Err.Contents())
}

var _ = Describe("catalog push contracts", func() {
	var (
		ctx           context.Context
		registryAddr  string
		srcCanonical  string
		destCanonical string
		isolatedEnv   utils.Isolation
		ref           catalog.Reference
	)

	BeforeEach(func() {
		ctx = context.Background()
		registryAddr = utils.NewLocalOCIRegistry()
		srcCanonical = registryAddr + "/" + contractSrcRepo
		destCanonical = registryAddr + "/" + contractDestRepo
		isolatedEnv = utils.NewIsolatedEnv()
		ref = contractRef(catalog.ArtifactKindSolution, contractName)
	})

	push := func(args ...string) *utils.ExecOption {
		base := make([]string, 0, 2+len(args)+2)
		base = append(base, "catalog", "push")
		base = append(base, args...)
		base = append(base, "--insecure", "--no-sbom")
		return utils.Scafctl(base...).WithEnv(isolatedEnv.Env)
	}

	pullFromSrc := func() {
		utils.Scafctl(
			"catalog", "pull", contractName+"@"+contractVersion,
			"--catalog", srcCanonical,
			"--insecure", "--no-verify",
		).WithEnv(isolatedEnv.Env).Exec()
	}

	It("requires --catalog and describes it as the destination", func() {
		seedBuilt(ctx, isolatedEnv.Root, ref, []byte("name: x\nbuilt: true\n"))

		s := push(contractName + "@" + contractVersion).ExpectFailure().Exec()

		gomega.Expect(s.ExitCode()).To(gomega.Equal(exitcode.InvalidInput))
		gomega.Expect(combinedOutput(s)).To(gomega.And(
			gomega.ContainSubstring("--catalog"),
			gomega.ContainSubstring("destination"),
		))
	})

	It("pushes a built artifact to the destination catalog", func() {
		builtDigest := seedBuilt(ctx, isolatedEnv.Root, ref, []byte("name: x\nbuilt: true\n"))

		push(contractName+"@"+contractVersion, "--catalog", destCanonical).Exec()

		gomega.Expect(remoteDigest(ctx, registryAddr, contractDestRepo, ref)).To(gomega.Equal(builtDigest))
	})

	Context("when a built and a pulled copy share name and version", func() {
		var builtDigest, pulledDigest string

		BeforeEach(func() {
			builtDigest = seedBuilt(ctx, isolatedEnv.Root, ref, []byte("name: x\nbuilt: true\n"))
			seedRemote(ctx, registryAddr, contractSrcRepo, ref, []byte("name: x\nupstream: true\n"), false)
			pullFromSrc()
			pulledDigest = localDigestFrom(ctx, isolatedEnv.Root, ref, srcCanonical)
			gomega.Expect(pulledDigest).NotTo(gomega.Equal(builtDigest), "fixture copies must differ")
		})

		It("errors with hints instead of picking one", func() {
			s := push(contractName+"@"+contractVersion, "--catalog", destCanonical).ExpectFailure().Exec()

			gomega.Expect(s.ExitCode()).To(gomega.Equal(exitcode.InvalidInput))
			gomega.Expect(combinedOutput(s)).To(gomega.And(
				gomega.ContainSubstring("--origin built"),
				gomega.ContainSubstring("--origin "+srcCanonical),
			))
			gomega.Expect(remoteDigest(ctx, registryAddr, contractDestRepo, ref)).To(gomega.BeEmpty(),
				"an ambiguous push must not write anything")
		})

		It("pushes exactly the pulled copy selected with --origin", func() {
			push(contractName+"@"+contractVersion, "--origin", srcCanonical, "--catalog", destCanonical).Exec()

			gomega.Expect(remoteDigest(ctx, registryAddr, contractDestRepo, ref)).To(gomega.Equal(pulledDigest),
				"the selected copy must be the one published (resolve once)")
		})

		It("pushes exactly the built copy selected with --origin built", func() {
			push(contractName+"@"+contractVersion, "--origin", catalog.OriginBuilt, "--catalog", destCanonical).Exec()

			gomega.Expect(remoteDigest(ctx, registryAddr, contractDestRepo, ref)).To(gomega.Equal(builtDigest))
		})

		It("pushes the copy selected by digest under its version tag", func() {
			push(contractName+"@"+pulledDigest, "--catalog", destCanonical).Exec()

			// Resolving by version proves the remote tag is the version, not the digest.
			gomega.Expect(remoteDigest(ctx, registryAddr, contractDestRepo, ref)).To(gomega.Equal(pulledDigest))
		})

		It("treats a fully qualified reference as the source mirror, not the destination", func() {
			srcBefore := remoteDigest(ctx, registryAddr, contractSrcRepo, ref)
			fqn := srcCanonical + "/solutions/" + contractName + "@" + contractVersion

			push(fqn, "--catalog", destCanonical).Exec()

			gomega.Expect(remoteDigest(ctx, registryAddr, contractDestRepo, ref)).To(gomega.Equal(pulledDigest))
			gomega.Expect(remoteDigest(ctx, registryAddr, contractSrcRepo, ref)).To(gomega.Equal(srcBefore),
				"the FQN names the source; it must not be written to")
		})

		It("rejects --origin combined with a fully qualified reference", func() {
			fqn := srcCanonical + "/solutions/" + contractName + "@" + contractVersion

			s := push(fqn, "--origin", catalog.OriginBuilt, "--catalog", destCanonical).ExpectFailure().Exec()

			gomega.Expect(s.ExitCode()).To(gomega.Equal(exitcode.InvalidInput))
			gomega.Expect(combinedOutput(s)).To(gomega.ContainSubstring("--origin"))
			gomega.Expect(remoteDigest(ctx, registryAddr, contractDestRepo, ref)).To(gomega.BeEmpty())
		})

		It("dry-run names the source origin and destination without writing", func() {
			s := push(contractName+"@"+contractVersion, "--origin", srcCanonical,
				"--catalog", destCanonical, "--dry-run").Exec()

			gomega.Expect(combinedOutput(s)).To(gomega.And(
				gomega.ContainSubstring(srcCanonical),
				gomega.ContainSubstring(destCanonical),
			))
			gomega.Expect(remoteDigest(ctx, registryAddr, contractDestRepo, ref)).To(gomega.BeEmpty())
		})
	})

	It("refuses to push a pulled copy back to its origin, even with --force", func() {
		seedRemote(ctx, registryAddr, contractSrcRepo, ref, []byte("name: x\nupstream: v1\n"), false)
		pullFromSrc()

		// Upstream republishes the same version after the pull; pushing the
		// stale local copy back with --force would silently revert it.
		upstreamNow := seedRemote(ctx, registryAddr, contractSrcRepo, ref, []byte("name: x\nupstream: v2\n"), true)

		s := push(contractName+"@"+contractVersion, "--catalog", srcCanonical, "--force").ExpectFailure().Exec()

		gomega.Expect(s.ExitCode()).To(gomega.Equal(exitcode.InvalidInput))
		gomega.Expect(remoteDigest(ctx, registryAddr, contractSrcRepo, ref)).To(gomega.Equal(upstreamNow),
			"the origin must be left untouched")
	})

	It("mirrors a pulled copy without publishing where it came from", func() {
		seedRemote(ctx, registryAddr, contractSrcRepo, ref, []byte("name: x\nupstream: true\n"), false)
		pullFromSrc()
		pulledDigest := localDigestFrom(ctx, isolatedEnv.Root, ref, srcCanonical)

		push(contractName+"@"+contractVersion, "--catalog", destCanonical).Exec()

		dest := assertRemoteCatalog(registryAddr, contractDestRepo)
		tags, err := dest.ListTags(ctx, ref)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(tags).To(gomega.HaveLen(1))
		gomega.Expect(tags[0].Tag).To(gomega.Equal(contractVersion),
			"the remote tag must be the bare version, with no source catalog in it")

		_, info, err := dest.Fetch(ctx, ref)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(info.Digest).To(gomega.Equal(pulledDigest), "content is published unchanged")
		gomega.Expect(info.Annotations).NotTo(gomega.Or(
			gomega.HaveKey(catalog.AnnotationSourceCanonical),
			gomega.HaveKey(catalog.AnnotationSourceName),
			gomega.HaveKey(catalog.AnnotationOrigin),
		))
		for k, v := range info.Annotations {
			gomega.Expect(v).NotTo(gomega.ContainSubstring(srcCanonical), "annotation %s leaks the source catalog", k)
		}
	})

	It("relocates the artifact under the --as name", func() {
		builtDigest := seedBuilt(ctx, isolatedEnv.Root, ref, []byte("name: x\nbuilt: true\n"))
		renamed := contractRef(catalog.ArtifactKindSolution, "renamed-x")

		push(contractName+"@"+contractVersion, "--as", renamed.Name, "--catalog", destCanonical).Exec()

		gomega.Expect(remoteDigest(ctx, registryAddr, contractDestRepo, renamed)).To(gomega.Equal(builtDigest),
			"push --as should publish under the new name")
		gomega.Expect(remoteDigest(ctx, registryAddr, contractDestRepo, ref)).To(gomega.BeEmpty(),
			"the original name must not be published when --as relocates")
	})

	It("rejects an --as value that is not a valid artifact name", func() {
		seedBuilt(ctx, isolatedEnv.Root, ref, []byte("name: x\nbuilt: true\n"))

		s := push(contractName+"@"+contractVersion, "--as", "a/b", "--catalog", destCanonical).ExpectFailure().Exec()

		gomega.Expect(s.ExitCode()).To(gomega.Equal(exitcode.InvalidInput))
		gomega.Expect(remoteDigest(ctx, registryAddr, contractDestRepo, ref)).To(gomega.BeEmpty())
	})

	It("errors when the name exists as more than one kind", func() {
		provider := contractRef(catalog.ArtifactKindProvider, contractName)
		seedBuilt(ctx, isolatedEnv.Root, ref, []byte("name: x\nbuilt: true\n"))
		seedBuilt(ctx, isolatedEnv.Root, provider, []byte("provider-binary"))

		s := push(contractName, "--catalog", destCanonical).ExpectFailure().Exec()

		gomega.Expect(s.ExitCode()).To(gomega.Equal(exitcode.InvalidInput))
		gomega.Expect(combinedOutput(s)).To(gomega.ContainSubstring("--kind"))
		gomega.Expect(remoteDigest(ctx, registryAddr, contractDestRepo, ref)).To(gomega.BeEmpty())
		gomega.Expect(remoteDigest(ctx, registryAddr, contractDestRepo, provider)).To(gomega.BeEmpty())
	})
})
