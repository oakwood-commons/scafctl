// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package catalogtest

import (
	"fmt"
	"path/filepath"
	"runtime"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot-import is the Ginkgo/Gomega house style.
	"github.com/onsi/gomega"

	"github.com/oakwood-commons/scafctl/e2e/internal/utils"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
)

const (
	// pluginFixtureDir is the provider fixture, relative to the repo root.
	pluginFixtureDir = "./e2e/testdata/plugins/providers/my-plugin"

	// authHandlerFixtureDir is the fake auth-handler fixture, relative to the repo root.
	authHandlerFixtureDir = "./e2e/testdata/plugins/auth-handlers/my-auth-handler"

	// authHandlerName is the artifact name the auth-handler fixture is packaged as.
	authHandlerName = "test-auth-handler"
)

var _ = Describe("catalog push", func() {
	var (
		iso          utils.Isolation
		registryAddr string
		built        packageResult
	)

	BeforeEach(func() {
		iso = utils.NewIsolatedEnv()
		registryAddr = utils.NewLocalOCIRegistry()
		built = packageSolution(iso, writeSolutionVariant("baseline"), "1.0.0")
	})

	It("pushes a built solution to a full remote reference", func() {
		session := utils.Scafctl(
			"catalog", "push", solutionRef(registryAddr, demoName, "1.0.0"),
			"--insecure",
		).WithEnv(iso.Env).Exec()
		gomega.Expect(output(session)).To(gomega.ContainSubstring("Pushed"))

		gomega.Expect(remoteTags(iso, registryAddr, "solutions", demoName)).To(gomega.ContainElement("1.0.0"))
		gomega.Expect(remoteDigest(iso, registryAddr, demoName, "1.0.0")).To(gomega.Equal(built.Digest))
	})

	It("publishes under a different name and version with --as", func() {
		session := utils.Scafctl(
			"catalog", "push", solutionRef(registryAddr, demoName, "1.0.0"),
			"--as", "other@2.0.0",
			"--insecure",
		).WithEnv(iso.Env).Exec()
		gomega.Expect(output(session)).To(gomega.ContainSubstring("other@2.0.0"))

		gomega.Expect(remoteTags(iso, registryAddr, "solutions", "other")).To(gomega.ContainElement("2.0.0"))
		gomega.Expect(remoteNames(iso, registryAddr)).NotTo(gomega.ContainElement(demoName))
	})

	It("attaches an SBOM unless --no-sbom is given", func() {
		withSBOM := utils.Scafctl(
			"catalog", "push", solutionRef(registryAddr, demoName, "1.0.0"),
			"--insecure",
		).WithEnv(iso.Env).Exec()
		gomega.Expect(output(withSBOM)).To(gomega.ContainSubstring("SBOM attached"))

		withoutSBOM := utils.Scafctl(
			"catalog", "push", solutionRef(registryAddr, demoName, "1.0.0"),
			"--as", "nosbom@1.0.0",
			"--no-sbom",
			"--insecure",
		).WithEnv(iso.Env).Exec()
		gomega.Expect(output(withoutSBOM)).NotTo(gomega.ContainSubstring("SBOM"))
	})

	DescribeTable("pushes through the solution kind subcommand and its aliases",
		func(kindCommand string) {
			session := utils.Scafctl(
				"catalog", "push", kindCommand, demoName+"@1.0.0",
				"--catalog", catalogURL(registryAddr),
				"--insecure",
			).WithEnv(iso.Env).Exec()
			gomega.Expect(output(session)).To(gomega.ContainSubstring("Pushed"))

			gomega.Expect(remoteTags(iso, registryAddr, "solutions", demoName)).To(gomega.ContainElement("1.0.0"))
		},
		Entry("solution", "solution"),
		Entry("sol alias", "sol"),
		Entry("solutions alias", "solutions"),
	)

	It("pushes a provider through the provider kind subcommand", func() {
		pluginBin := filepath.Join(GinkgoT().TempDir(), "my-plugin")
		platform := runtime.GOOS + "/" + runtime.GOARCH
		utils.BuildPluginBinaryForPlatform(pluginBin, pluginFixtureDir, platform)

		utils.Scafctl(
			"package", "plugin",
			"--name", "test-provider",
			"--kind", "provider",
			"--version", "1.0.0",
			"--platform", platform+"="+pluginBin,
		).WithEnv(iso.Env).Exec()

		session := utils.Scafctl(
			"catalog", "push", "provider", "test-provider@1.0.0",
			"--catalog", catalogURL(registryAddr),
			"--insecure",
		).WithEnv(iso.Env).Exec()
		gomega.Expect(output(session)).To(gomega.ContainSubstring("Pushed"))

		gomega.Expect(remoteTags(iso, registryAddr, "providers", "test-provider")).To(gomega.ContainElement("1.0.0"))
	})

	It("pushes an auth-handler through the auth-handler kind subcommand", func() {
		handlerBin := filepath.Join(GinkgoT().TempDir(), "my-auth-handler")
		platform := runtime.GOOS + "/" + runtime.GOARCH
		utils.BuildPluginBinaryForPlatform(handlerBin, authHandlerFixtureDir, platform)

		utils.Scafctl(
			"package", "plugin",
			"--name", authHandlerName,
			"--kind", "auth-handler",
			"--version", "1.0.0",
			"--platform", platform+"="+handlerBin,
		).WithEnv(iso.Env).Exec()

		session := utils.Scafctl(
			"catalog", "push", "auth-handler", authHandlerName+"@1.0.0",
			"--catalog", catalogURL(registryAddr),
			"--insecure",
		).WithEnv(iso.Env).Exec()
		gomega.Expect(output(session)).To(gomega.ContainSubstring("Pushed"))

		gomega.Expect(remoteTags(iso, registryAddr, "auth-handlers", authHandlerName)).To(gomega.ContainElement("1.0.0"))
	})

	Describe("when a built and a pulled copy share name, kind and version", func() {
		var (
			sourceAddr string // registry the pulled copy came from
			pulledDig  string // digest of the pulled copy (content X)
			destAddr   string // registry the specs push to
		)

		BeforeEach(func() {
			sourceAddr = utils.NewLocalOCIRegistry()
			destAddr = utils.NewLocalOCIRegistry()

			// The local catalog already holds the built copy (content
			// "baseline"). Publish different content to the source registry
			// and pull it, so both copies sit under demo@1.0.0.
			pulledDig = publishVariant(sourceAddr, "1.0.0", "pulled-content")
			gomega.Expect(pulledDig).NotTo(gomega.Equal(built.Digest))

			utils.Scafctl(
				"catalog", "pull", solutionRef(sourceAddr, demoName, "1.0.0"),
				"--insecure",
			).WithEnv(iso.Env).Exec()
		})

		It("refuses to guess and hints an --origin value per origin", func() {
			session := utils.Scafctl(
				"catalog", "push", solutionRef(destAddr, demoName, "1.0.0"),
				"--insecure",
			).WithEnv(iso.Env).ExpectFailure().Exec()

			gomega.Expect(session.ExitCode()).To(gomega.Equal(exitcode.InvalidInput))
			out := output(session)
			gomega.Expect(out).To(gomega.ContainSubstring("--origin built"))
			gomega.Expect(out).To(gomega.ContainSubstring(fmt.Sprintf("--origin %s", catalogURL(sourceAddr))))

			// Nothing was pushed.
			gomega.Expect(remoteNames(iso, destAddr)).To(gomega.BeEmpty())
		})

		It("pushes the built copy with --origin built", func() {
			utils.Scafctl(
				"catalog", "push", solutionRef(destAddr, demoName, "1.0.0"),
				"--origin", "built",
				"--insecure",
			).WithEnv(iso.Env).Exec()

			gomega.Expect(remoteDigest(iso, destAddr, demoName, "1.0.0")).To(gomega.Equal(built.Digest))
		})

		It("pushes the pulled copy with --origin <registry>", func() {
			utils.Scafctl(
				"catalog", "push", solutionRef(destAddr, demoName, "1.0.0"),
				"--origin", catalogURL(sourceAddr),
				"--insecure",
			).WithEnv(iso.Env).Exec()

			gomega.Expect(remoteDigest(iso, destAddr, demoName, "1.0.0")).To(gomega.Equal(pulledDig))
		})
	})
})
