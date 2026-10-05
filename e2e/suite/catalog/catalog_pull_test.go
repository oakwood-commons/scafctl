// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package catalogtest

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot-import is the Ginkgo/Gomega house style.
	"github.com/onsi/gomega"

	"github.com/oakwood-commons/scafctl/e2e/internal/utils"
)

var _ = Describe("catalog pull", func() {
	var (
		iso          utils.Isolation
		registryAddr string
		published    string // digest of demo@1.0.0 in the registry
	)

	BeforeEach(func() {
		iso = utils.NewIsolatedEnv()
		registryAddr = utils.NewLocalOCIRegistry()
		published = publishVariant(registryAddr, "1.0.0", "published")
	})

	It("pulls an artifact from a full remote reference", func() {
		session := utils.Scafctl(
			"catalog", "pull", solutionRef(registryAddr, demoName, "1.0.0"),
			"--insecure",
		).WithEnv(iso.Env).Exec()
		gomega.Expect(output(session)).To(gomega.ContainSubstring("Pulled"))

		// With a single local copy the short reference resolves unambiguously.
		gomega.Expect(localDigest(iso, demoName, "1.0.0")).To(gomega.Equal(published))
	})

	It("pulls through the solution kind subcommand to the same digest", func() {
		session := utils.Scafctl(
			"catalog", "pull", "solution", demoName+"@1.0.0",
			"--catalog", catalogURL(registryAddr),
			"--insecure",
		).WithEnv(iso.Env).Exec()
		gomega.Expect(output(session)).To(gomega.ContainSubstring("Pulled"))

		gomega.Expect(localDigest(iso, demoName, "1.0.0")).To(gomega.Equal(published))
	})

	It("keeps copies of the same name, kind and version from different registries", func() {
		otherAddr := utils.NewLocalOCIRegistry()
		otherDigest := publishVariant(otherAddr, "1.0.0", "other-content")
		gomega.Expect(otherDigest).NotTo(gomega.Equal(published))

		for _, addr := range []string{registryAddr, otherAddr} {
			session := utils.Scafctl(
				"catalog", "pull", solutionRef(addr, demoName, "1.0.0"),
				"--insecure",
			).WithEnv(iso.Env).Exec()
			gomega.Expect(output(session)).NotTo(gomega.ContainSubstring("already exists"))
		}

		copies := localCopies(iso, demoName, "1.0.0")
		gomega.Expect(copies).To(gomega.HaveLen(2))
		gomega.Expect(copies).To(gomega.HaveKeyWithValue(remoteOrigin(registryAddr), published))
		gomega.Expect(copies).To(gomega.HaveKeyWithValue(remoteOrigin(otherAddr), otherDigest))
	})

	It("keeps a built copy and a pulled copy side by side without --force", func() {
		built := packageSolution(iso, writeSolutionVariant("baseline"), "1.0.0")
		gomega.Expect(built.Digest).NotTo(gomega.Equal(published))

		session := utils.Scafctl(
			"catalog", "pull", solutionRef(registryAddr, demoName, "1.0.0"),
			"--insecure",
		).WithEnv(iso.Env).Exec()
		gomega.Expect(output(session)).NotTo(gomega.ContainSubstring("already exists"))

		copies := localCopies(iso, demoName, "1.0.0")
		gomega.Expect(copies).To(gomega.HaveLen(2))
		gomega.Expect(copies).To(gomega.HaveKeyWithValue(builtOrigin, built.Digest))
		gomega.Expect(copies).To(gomega.HaveKeyWithValue(remoteOrigin(registryAddr), published))
	})

	It("stores the pulled artifact under another name and version with --as", func() {
		utils.Scafctl(
			"catalog", "pull", solutionRef(registryAddr, demoName, "1.0.0"),
			"--as", "local@2.0.0",
			"--insecure",
		).WithEnv(iso.Env).Exec()

		// A rename rewrites the stored artifact, so its digest differs from the
		// published one; the source name is not stored locally.
		gomega.Expect(localDigest(iso, "local", "2.0.0")).NotTo(gomega.Equal(published))
		utils.Scafctl("catalog", "inspect", demoName+"@1.0.0").WithEnv(iso.Env).ExpectFailure().Exec()
	})

	It("honours --as through the solution kind subcommand", func() {
		utils.Scafctl(
			"catalog", "pull", "solution", demoName+"@1.0.0",
			"--catalog", catalogURL(registryAddr),
			"--as", "local@2.0.0",
			"--insecure",
		).WithEnv(iso.Env).Exec()

		gomega.Expect(localDigest(iso, "local", "2.0.0")).NotTo(gomega.Equal(published))
		utils.Scafctl("catalog", "inspect", demoName+"@1.0.0").WithEnv(iso.Env).ExpectFailure().Exec()
	})
})

var _ = Describe("catalog pull of plugin kinds", func() {
	var (
		iso          utils.Isolation
		registryAddr string
	)

	BeforeEach(func() {
		iso = utils.NewIsolatedEnv()
		registryAddr = utils.NewLocalOCIRegistry()
	})

	DescribeTable("pulls through the kind subcommand",
		func(k pluginKind) {
			published := publishPlugin(k, registryAddr, "1.0.0")

			session := utils.Scafctl(
				"catalog", "pull", k.Kind, k.Name+"@1.0.0",
				"--catalog", catalogURL(registryAddr),
				"--insecure",
			).WithEnv(iso.Env).Exec()
			gomega.Expect(output(session)).To(gomega.ContainSubstring("Pulled"))

			gomega.Expect(localPluginDigest(iso, k, "1.0.0")).To(gomega.Equal(published))
		},
		Entry("provider", providerPlugin),
		Entry("auth-handler", authHandlerPlugin),
	)

	DescribeTable("pulls from a full remote reference, inferring the kind from the path",
		func(k pluginKind) {
			published := publishPlugin(k, registryAddr, "1.0.0")

			session := utils.Scafctl(
				"catalog", "pull",
				fmt.Sprintf("%s/%s/%s@1.0.0", catalogURL(registryAddr), k.Plural, k.Name),
				"--insecure",
			).WithEnv(iso.Env).Exec()
			gomega.Expect(output(session)).To(gomega.ContainSubstring("Pulled"))

			gomega.Expect(localPluginDigest(iso, k, "1.0.0")).To(gomega.Equal(published))
		},
		Entry("provider", providerPlugin),
		Entry("auth-handler", authHandlerPlugin),
	)
})
