//go:build linux || darwin

// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0
package packagetest

import (
	"path/filepath"

	"github.com/oakwood-commons/scafctl/e2e/internal/utils"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

var _ = ginkgo.Describe("plugin registry flow", func() {
	var (
		registryAddr string
		isolatedEnv  utils.Isolation
	)

	ginkgo.BeforeEach(func() {
		registryAddr = utils.NewLocalOCIRegistry()
		isolatedEnv = utils.NewIsolatedEnv()
	})

	ginkgo.It("packages and pushes a multi-platform plugin artifact to a remote OCI registry", func() {
		pluginBinDir := ginkgo.GinkgoT().TempDir()
		linuxBin := filepath.Join(pluginBinDir, "my-plugin-linux-amd64")
		darwinBin := filepath.Join(pluginBinDir, "my-plugin-darwin-arm64")

		utils.BuildPluginBinaryForPlatform(linuxBin, "./e2e/testdata/plugins/providers/my-plugin", "linux/amd64")
		utils.BuildPluginBinaryForPlatform(darwinBin, "./e2e/testdata/plugins/providers/my-plugin", "darwin/arm64")

		packageCmd := utils.Scafctl(
			"package", "plugin",
			"--name", "test-provider",
			"--kind", "provider",
			"--version", "1.0.0",
			"--platform", "linux/amd64="+linuxBin,
			"--platform", "darwin/arm64="+darwinBin,
		).WithEnv(isolatedEnv.Env)
		session := packageCmd.Exec()
		gomega.Expect(string(session.Out.Contents())).To(gomega.ContainSubstring("test-provider"))

		pushCmd := utils.Scafctl(
			"catalog", "push",
			"test-provider@1.0.0",
			"--catalog", registryAddr+"/scafctl",
			"--insecure",
		).WithEnv(isolatedEnv.Env)
		pushSession := pushCmd.Exec()
		gomega.Expect(string(pushSession.Out.Contents())).To(gomega.ContainSubstring("test-provider"))

		listCmd := utils.Scafctl(
			"catalog", "list",
			"--catalog", registryAddr+"/scafctl",
			"--insecure",
			"-o", "json",
		).WithEnv(isolatedEnv.Env)
		listSession := listCmd.Exec()
		gomega.Expect(string(listSession.Out.Contents())).To(gomega.ContainSubstring("test-provider"))
	})
})
