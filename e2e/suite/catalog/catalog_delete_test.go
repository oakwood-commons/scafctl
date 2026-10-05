// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package catalogtest

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot-import is the Ginkgo/Gomega house style.
	"github.com/onsi/gomega"

	"github.com/oakwood-commons/scafctl/e2e/internal/utils"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
)

var _ = Describe("catalog delete", func() {
	var iso utils.Isolation

	BeforeEach(func() {
		iso = utils.NewIsolatedEnv()
	})

	Describe("from the local catalog", func() {
		It("deletes the only copy of an artifact", func() {
			packageSolution(iso, writeSolutionVariant("baseline"), "1.0.0")

			session := utils.Scafctl("catalog", "delete", "solution", demoName+"@1.0.0").WithEnv(iso.Env).Exec()
			gomega.Expect(output(session)).To(gomega.ContainSubstring("Deleted"))

			utils.Scafctl("catalog", "inspect", demoName+"@1.0.0").WithEnv(iso.Env).ExpectFailure().Exec()
		})

		Describe("when a built copy and copies from two registries share name, kind and version", func() {
			var (
				regA, regB       string
				built            packageResult
				digestA, digestB string
			)

			BeforeEach(func() {
				regA = utils.NewLocalOCIRegistry()
				regB = utils.NewLocalOCIRegistry()
				built = packageSolution(iso, writeSolutionVariant("baseline"), "1.0.0")
				digestA = publishVariant(regA, "1.0.0", "content-a")
				digestB = publishVariant(regB, "1.0.0", "content-b")

				for _, addr := range []string{regA, regB} {
					utils.Scafctl(
						"catalog", "pull", solutionRef(addr, demoName, "1.0.0"),
						"--insecure",
					).WithEnv(iso.Env).Exec()
				}
				gomega.Expect(localCopies(iso, demoName, "1.0.0")).To(gomega.HaveLen(3))
			})

			It("refuses to guess and hints an --origin value per origin", func() {
				session := utils.Scafctl("catalog", "delete", "solution", demoName+"@1.0.0").
					WithEnv(iso.Env).ExpectFailure().Exec()

				gomega.Expect(session.ExitCode()).To(gomega.Equal(exitcode.InvalidInput))
				out := output(session)
				gomega.Expect(out).To(gomega.ContainSubstring("--origin built"))
				gomega.Expect(out).To(gomega.ContainSubstring(fmt.Sprintf("--origin %s", catalogURL(regA))))
				gomega.Expect(out).To(gomega.ContainSubstring(fmt.Sprintf("--origin %s", catalogURL(regB))))

				// Nothing was deleted.
				gomega.Expect(localCopies(iso, demoName, "1.0.0")).To(gomega.HaveLen(3))
			})

			It("deletes only the built copy with --origin built", func() {
				utils.Scafctl(
					"catalog", "delete", "solution", demoName+"@1.0.0",
					"--origin", builtOrigin,
				).WithEnv(iso.Env).Exec()

				copies := localCopies(iso, demoName, "1.0.0")
				gomega.Expect(copies).To(gomega.HaveLen(2))
				gomega.Expect(copies).NotTo(gomega.HaveKey(builtOrigin))
				gomega.Expect(copies).To(gomega.HaveKeyWithValue(remoteOrigin(regA), digestA))
				gomega.Expect(copies).To(gomega.HaveKeyWithValue(remoteOrigin(regB), digestB))
			})

			It("deletes only the selected pulled copy, then the last copy needs no --origin", func() {
				utils.Scafctl(
					"catalog", "delete", "solution", demoName+"@1.0.0",
					"--origin", catalogURL(regA),
				).WithEnv(iso.Env).Exec()

				copies := localCopies(iso, demoName, "1.0.0")
				gomega.Expect(copies).To(gomega.HaveLen(2))
				gomega.Expect(copies).NotTo(gomega.HaveKey(remoteOrigin(regA)))
				gomega.Expect(copies).To(gomega.HaveKeyWithValue(builtOrigin, built.Digest))
				gomega.Expect(copies).To(gomega.HaveKeyWithValue(remoteOrigin(regB), digestB))

				utils.Scafctl(
					"catalog", "delete", "solution", demoName+"@1.0.0",
					"--origin", builtOrigin,
				).WithEnv(iso.Env).Exec()

				// One copy left: the short reference is unambiguous again.
				gomega.Expect(localDigest(iso, demoName, "1.0.0")).To(gomega.Equal(digestB))
				utils.Scafctl("catalog", "delete", "solution", demoName+"@1.0.0").WithEnv(iso.Env).Exec()
				utils.Scafctl("catalog", "inspect", demoName+"@1.0.0").WithEnv(iso.Env).ExpectFailure().Exec()
			})

			It("leaves the source registries untouched", func() {
				for _, origin := range []string{builtOrigin, catalogURL(regA), catalogURL(regB)} {
					utils.Scafctl(
						"catalog", "delete", "solution", demoName+"@1.0.0",
						"--origin", origin,
					).WithEnv(iso.Env).Exec()
				}

				gomega.Expect(remoteTags(iso, regA, "solutions", demoName)).To(gomega.ContainElement("1.0.0"))
				gomega.Expect(remoteTags(iso, regB, "solutions", demoName)).To(gomega.ContainElement("1.0.0"))
				gomega.Expect(remoteDigest(iso, regA, demoName, "1.0.0")).To(gomega.Equal(digestA))
				gomega.Expect(remoteDigest(iso, regB, demoName, "1.0.0")).To(gomega.Equal(digestB))
			})

			It("deletes every local artifact with --all --force", func() {
				session := utils.Scafctl("catalog", "delete", "--all", "--force").WithEnv(iso.Env).Exec()
				gomega.Expect(output(session)).To(gomega.MatchRegexp(`Deleted \d+ artifact\(s\)`))

				utils.Scafctl("catalog", "inspect", demoName+"@1.0.0").WithEnv(iso.Env).ExpectFailure().Exec()
			})
		})
	})

	Describe("from a remote registry", func() {
		var (
			registryAddr string
			keptDigest   string
			localBuilt   packageResult
		)

		BeforeEach(func() {
			registryAddr = utils.NewLocalOCIRegistry()
			publishVariant(registryAddr, "1.0.0", "to-delete")
			keptDigest = publishVariant(registryAddr, "1.1.0", "to-keep")
			localBuilt = packageSolution(iso, writeSolutionVariant("baseline"), "1.1.0")
			gomega.Expect(remoteTags(iso, registryAddr, "solutions", demoName)).To(gomega.ConsistOf("1.0.0", "1.1.0"))
		})

		assertOnlyTargetRemoved := func(deleteOutput string) {
			GinkgoHelper()

			gomega.Expect(deleteOutput).To(gomega.ContainSubstring("Deleted"))
			gomega.Expect(remoteTags(iso, registryAddr, "solutions", demoName)).To(gomega.ConsistOf("1.1.0"))
			gomega.Expect(remoteDigest(iso, registryAddr, demoName, "1.1.0")).To(gomega.Equal(keptDigest))

			// The deleted version can no longer be pulled.
			pull := utils.Scafctl(
				"catalog", "pull", solutionRef(registryAddr, demoName, "1.0.0"),
				"--insecure",
			).WithEnv(iso.Env).ExpectFailure().Exec()
			gomega.Expect(pull.ExitCode()).To(gomega.Equal(exitcode.FileNotFound))

			// The local catalog is unaffected by a remote delete.
			gomega.Expect(localDigest(iso, demoName, "1.1.0")).To(gomega.Equal(localBuilt.Digest))
		}

		It("deletes one version through a full remote reference", func() {
			session := utils.Scafctl(
				"catalog", "delete", solutionRef(registryAddr, demoName, "1.0.0"),
				"--insecure",
			).WithEnv(iso.Env).Exec()

			assertOnlyTargetRemoved(output(session))
		})

		It("deletes one version through the kind subcommand with --catalog", func() {
			session := utils.Scafctl(
				"catalog", "delete", "solution", demoName+"@1.0.0",
				"--catalog", catalogURL(registryAddr),
				"--insecure",
			).WithEnv(iso.Env).Exec()

			assertOnlyTargetRemoved(output(session))
		})
	})
})
