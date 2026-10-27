// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package runtest

import (
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot-import is the Ginkgo/Gomega house style.
	"github.com/onsi/gomega"

	"github.com/oakwood-commons/scafctl/e2e/internal/utils"
)

// authHandler is the custom OAuth2 handler registered by WriteAuthConfig.
const authHandler = "e2e-oauth"

// allowLoopbackHTTP appends an httpClient allowlist for loopback to the
// isolated config so the http provider may reach the in-process fake resource
// (the SSRF policy blocks private ranges by default).
func allowLoopbackHTTP(env utils.Isolation) {
	GinkgoHelper()
	path := filepath.Join(env.Root, "scafctl", "config.yaml")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // test-controlled temp path.
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	defer f.Close()
	_, err = f.WriteString("\nhttpClient:\n  allowedPrivateCIDRs:\n    - 127.0.0.0/8\n")
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
}

var _ = Describe("run runtime injection", func() {
	Describe("auth handler", func() {
		var (
			env      utils.Isolation
			idp      *utils.FakeIDP
			resource *utils.ProtectedResource
			dir      string
		)

		BeforeEach(func() {
			idp = utils.NewFakeIDP()
			DeferCleanup(idp.Close)
			idp.EnableScopedTokens()
			resource = idp.NewProtectedResource()
			DeferCleanup(resource.Close)

			env = utils.NewIsolatedEnv()
			env.WriteAuthConfig(authHandler, idp)
			allowLoopbackHTTP(env)
			dir = stageFixture("run-auth")
		})

		It("injects the handler's token into an authProvider request", func() {
			utils.Scafctl("auth", "login", authHandler).WithEnv(env.Env).Exec()
			tokenOut := utils.Scafctl("auth", "token", authHandler).WithEnv(env.Env).Exec().Out.Contents()
			token := strings.TrimSpace(string(tokenOut))

			s := run(env, dir, "run", "resolver", "protected", "-f", fixturePath(dir),
				"-r", "url="+resource.URL("api:read"), "-o", "json")
			utils.ExpectExitCode(s, exitSuccess)

			gomega.Expect(resource.LastAuthHeader()).To(gomega.Equal("Bearer " + token))
			// The credential reaches the server but must never be echoed in output.
			gomega.Expect(string(s.Out.Contents())).NotTo(gomega.ContainSubstring(token))
			gomega.Expect(string(s.Err.Contents())).NotTo(gomega.ContainSubstring(token))
		})

		It("is rejected by a resource requiring a scope the handler lacks", func() {
			utils.Scafctl("auth", "login", authHandler).WithEnv(env.Env).Exec()

			s := run(env, dir, "run", "resolver", "protected", "-f", fixturePath(dir),
				"-r", "url="+resource.URL("api:write"), "-o", "json")
			// The http provider reports the HTTP status in its output rather than failing.
			utils.ExpectExitCode(s, exitSuccess)
			gomega.Expect(string(s.Out.Contents())).To(gomega.MatchRegexp("\"statusCode\":\\s*403"))
		})

		It("fails without injecting a token when not logged in", func() {
			s := run(env, dir, "run", "resolver", "protected", "-f", fixturePath(dir),
				"-r", "url="+resource.URL("api:read"), "-o", "json")
			utils.ExpectExitCode(s, exitGeneralError)
			gomega.Expect(string(s.Err.Contents())).To(gomega.ContainSubstring("not authenticated"))
			gomega.Expect(resource.LastAuthHeader()).To(gomega.BeEmpty())
		})
	})

	Describe("bundled data", func() {
		It("resolves bundled files relative to the solution, not the working directory", func() {
			env := utils.NewIsolatedEnv()
			elsewhere := GinkgoT().TempDir()
			solution := fixturePath(stageFixture("run-bundled"))

			dev := run(env, elsewhere, "run", "resolver", "-f", solution, "-r", "environment=dev", "-o", "json")
			utils.ExpectExitCode(dev, exitSuccess)
			prod := run(env, elsewhere, "run", "resolver", "-f", solution, "-r", "environment=prod", "-o", "json")
			utils.ExpectExitCode(prod, exitSuccess)

			devDoc, prodDoc := parseDoc(dev.Out.Contents()), parseDoc(prod.Out.Contents())
			gomega.Expect(devDoc).To(gomega.HaveKey("config"))
			gomega.Expect(string(dev.Out.Contents())).To(gomega.ContainSubstring("dev-config"))
			gomega.Expect(string(prod.Out.Contents())).To(gomega.ContainSubstring("prod-config"))
			gomega.Expect(prodDoc["config"]).NotTo(gomega.Equal(devDoc["config"]),
				"different environments must select different bundled data files")
		})
	})
})
