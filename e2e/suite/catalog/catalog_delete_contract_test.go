// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package catalogtest

import (
	"context"

	"github.com/oakwood-commons/scafctl/e2e/internal/utils"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot-import is the Ginkgo/Gomega house style.
	"github.com/onsi/gomega"
)

// Delete contract specs. These lock in the user-visible delete contracts
// mirroring catalog_push_contract_test.go: when a built and a pulled copy
// share name and version, delete must not guess which one to remove --
// ambiguity is an error with --origin/--kind hints, and each --origin/digest
// selection must remove exactly the targeted copy and leave the other intact.

var _ = Describe("catalog delete contracts", func() {
	var (
		ctx          context.Context
		registryAddr string
		srcCanonical string
		isolatedEnv  utils.Isolation
		ref          catalog.Reference
	)

	BeforeEach(func() {
		ctx = context.Background()
		registryAddr = utils.NewLocalOCIRegistry()
		srcCanonical = registryAddr + "/" + contractSrcRepo
		isolatedEnv = utils.NewIsolatedEnv()
		ref = contractRef(catalog.ArtifactKindSolution, contractName)
	})

	del := func(args ...string) *utils.ExecOption {
		base := make([]string, 0, 2+len(args)+1)
		base = append(base, "catalog", "delete")
		base = append(base, args...)
		base = append(base, "--force")
		return utils.Scafctl(base...).WithEnv(isolatedEnv.Env)
	}

	pullFromSrc := func() {
		utils.Scafctl(
			"catalog", "pull", contractName+"@"+contractVersion,
			"--catalog", srcCanonical,
			"--insecure", "--no-verify",
		).WithEnv(isolatedEnv.Env).Exec()
	}

	existsFrom := func(canonical string) bool {
		infos, err := contractLocalCatalog(isolatedEnv.Root).List(ctx, ref.Kind, ref.Name)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		for _, info := range infos {
			if info.Canonical == canonical && info.Reference.Version != nil && info.Reference.Version.Equal(ref.Version) {
				return true
			}
		}
		return false
	}

	It("deletes an unambiguous built artifact", func() {
		seedBuilt(ctx, isolatedEnv.Root, ref, []byte("name: x\nbuilt: true\n"))

		del(contractName + "@" + contractVersion).Exec()

		gomega.Expect(existsFrom("")).To(gomega.BeFalse())
	})

	It("errors when the name is not found", func() {
		s := del(contractName + "@" + contractVersion).ExpectFailure().Exec()

		gomega.Expect(s.ExitCode()).To(gomega.Equal(exitcode.FileNotFound))
		gomega.Expect(combinedOutput(s)).To(gomega.ContainSubstring("not found"))
	})

	Context("when a built and a pulled copy share name and version", func() {
		var pulledDigest string

		BeforeEach(func() {
			seedBuilt(ctx, isolatedEnv.Root, ref, []byte("name: x\nbuilt: true\n"))
			seedRemote(ctx, registryAddr, contractSrcRepo, ref, []byte("name: x\nupstream: true\n"), false)
			pullFromSrc()
			pulledDigest = localDigestFrom(ctx, isolatedEnv.Root, ref, srcCanonical)
		})

		It("errors with hints instead of picking one", func() {
			s := del(contractName + "@" + contractVersion).ExpectFailure().Exec()

			gomega.Expect(s.ExitCode()).To(gomega.Equal(exitcode.InvalidInput))
			gomega.Expect(combinedOutput(s)).To(gomega.And(
				gomega.ContainSubstring("--origin built"),
				gomega.ContainSubstring("--origin "+srcCanonical),
			))
			gomega.Expect(existsFrom("")).To(gomega.BeTrue(), "an ambiguous delete must not remove anything")
			gomega.Expect(existsFrom(srcCanonical)).To(gomega.BeTrue(), "an ambiguous delete must not remove anything")
		})

		It("deletes exactly the pulled copy selected with --origin", func() {
			del(contractName+"@"+contractVersion, "--origin", srcCanonical).Exec()

			gomega.Expect(existsFrom(srcCanonical)).To(gomega.BeFalse())
			gomega.Expect(existsFrom("")).To(gomega.BeTrue(), "the built copy must survive")
		})

		It("deletes exactly the built copy selected with --origin built", func() {
			del(contractName+"@"+contractVersion, "--origin", catalog.OriginBuilt).Exec()

			gomega.Expect(existsFrom("")).To(gomega.BeFalse())
			gomega.Expect(existsFrom(srcCanonical)).To(gomega.BeTrue(), "the pulled copy must survive")
		})

		It("deletes the copy selected by digest", func() {
			del(contractName + "@" + pulledDigest).Exec()

			gomega.Expect(existsFrom(srcCanonical)).To(gomega.BeFalse())
			gomega.Expect(existsFrom("")).To(gomega.BeTrue(), "the built copy must survive")
		})

		It("errors when --origin does not match any local copy", func() {
			s := del(contractName+"@"+contractVersion, "--origin", registryAddr+"/nowhere").ExpectFailure().Exec()

			gomega.Expect(s.ExitCode()).To(gomega.Equal(exitcode.FileNotFound))
			gomega.Expect(combinedOutput(s)).To(gomega.ContainSubstring("no copy from"))
			gomega.Expect(existsFrom("")).To(gomega.BeTrue())
			gomega.Expect(existsFrom(srcCanonical)).To(gomega.BeTrue())
		})

		It("dry-run reports what would be deleted without removing it", func() {
			s := del(contractName+"@"+contractVersion, "--origin", srcCanonical, "--dry-run").Exec()

			gomega.Expect(combinedOutput(s)).To(gomega.ContainSubstring("Would delete"))
			gomega.Expect(existsFrom(srcCanonical)).To(gomega.BeTrue())
		})
	})

	It("errors when the name exists as more than one kind", func() {
		provider := contractRef(catalog.ArtifactKindProvider, contractName)
		seedBuilt(ctx, isolatedEnv.Root, ref, []byte("name: x\nbuilt: true\n"))
		seedBuilt(ctx, isolatedEnv.Root, provider, []byte("provider-binary"))

		s := del(contractName + "@" + contractVersion).ExpectFailure().Exec()

		gomega.Expect(s.ExitCode()).To(gomega.Equal(exitcode.InvalidInput))
		gomega.Expect(combinedOutput(s)).To(gomega.ContainSubstring("--kind"))
		gomega.Expect(existsFrom("")).To(gomega.BeTrue())
	})

	It("resolves kind ambiguity with --kind", func() {
		provider := contractRef(catalog.ArtifactKindProvider, contractName)
		seedBuilt(ctx, isolatedEnv.Root, ref, []byte("name: x\nbuilt: true\n"))
		seedBuilt(ctx, isolatedEnv.Root, provider, []byte("provider-binary"))

		del(contractName+"@"+contractVersion, "--kind", "solution").Exec()

		infos, err := contractLocalCatalog(isolatedEnv.Root).List(ctx, catalog.ArtifactKindSolution, contractName)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(infos).To(gomega.BeEmpty())

		providerInfos, err := contractLocalCatalog(isolatedEnv.Root).List(ctx, catalog.ArtifactKindProvider, contractName)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(providerInfos).NotTo(gomega.BeEmpty(), "the provider copy must survive")
	})
})
