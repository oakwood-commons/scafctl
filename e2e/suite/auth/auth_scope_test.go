// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package authtest

import (
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot-import is the Ginkgo/Gomega house style.
	"github.com/onsi/gomega"

	"github.com/oakwood-commons/scafctl/e2e/internal/utils"
)

// Two custom OAuth2 handlers share one fake IdP but request different fixed
// scopes, so their tokens must remain isolated: neither the cache nor a
// downstream consumer may substitute one scope's token for the other.
const (
	readHandler  = "e2e-scope-read"
	writeHandler = "e2e-scope-write"

	scopeRead  = "api:read"
	scopeWrite = "api:write"
)

// tokenFor logs in (idempotent for client_credentials) and returns the raw
// access token the CLI prints for the given handler.
func tokenFor(env utils.Isolation, handler string) string {
	session := utils.Scafctl("auth", "token", handler).WithEnv(env.Env).Exec()
	return strings.TrimSpace(string(session.Out.Contents()))
}

var _ = Describe("token scoping and authorization", func() {
	var (
		env      utils.Isolation
		idp      *utils.FakeIDP
		resource *utils.ProtectedResource
	)

	BeforeEach(func() {
		idp = utils.NewFakeIDP()
		DeferCleanup(idp.Close)
		idp.EnableScopedTokens()

		resource = idp.NewProtectedResource()
		DeferCleanup(resource.Close)

		env = utils.NewIsolatedEnv()
		env.WriteAuthConfigHandlers(idp,
			utils.OAuth2HandlerSpec{Name: readHandler, Scopes: []string{scopeRead}},
			utils.OAuth2HandlerSpec{Name: writeHandler, Scopes: []string{scopeWrite}},
		)

		utils.Scafctl("auth", "login", readHandler).WithEnv(env.Env).Exec()
		utils.Scafctl("auth", "login", writeHandler).WithEnv(env.Env).Exec()
	})

	It("issues a distinct token per requested scope", func() {
		readTok := tokenFor(env, readHandler)
		writeTok := tokenFor(env, writeHandler)

		gomega.Expect(readTok).NotTo(gomega.BeEmpty())
		gomega.Expect(writeTok).NotTo(gomega.BeEmpty())
		gomega.Expect(readTok).NotTo(gomega.Equal(writeTok),
			"tokens acquired for different scopes must not be identical")

		gotScope, ok := idp.GrantedScope(readTok)
		gomega.Expect(ok).To(gomega.BeTrue())
		gomega.Expect(gotScope).To(gomega.Equal(scopeRead))
	})

	It("selects the cached token by scope without re-acquiring", func() {
		// Both handlers logged in during setup -> exactly two token acquisitions.
		gomega.Expect(idp.TokenCalls()).To(gomega.Equal(2))

		// A subsequent token request must be served from the per-scope cache
		// entry, not trigger a fresh acquisition.
		_ = tokenFor(env, readHandler)
		gomega.Expect(idp.TokenCalls()).To(gomega.Equal(2),
			"a cached token for the requested scope must be reused")
	})

	It("is accepted only by a resource requiring its own scope", func() {
		readTok := tokenFor(env, readHandler)
		writeTok := tokenFor(env, writeHandler)

		gomega.Expect(resource.Get(readTok, scopeRead)).To(gomega.Equal(http.StatusOK))
		gomega.Expect(resource.Get(writeTok, scopeWrite)).To(gomega.Equal(http.StatusOK))
	})

	It("is rejected by a resource requiring a different scope", func() {
		readTok := tokenFor(env, readHandler)
		writeTok := tokenFor(env, writeHandler)

		// A read-scoped token must not satisfy a write-scoped resource, and
		// vice versa: no fallback to a broader or unrelated token.
		gomega.Expect(resource.Get(readTok, scopeWrite)).To(gomega.Equal(http.StatusForbidden))
		gomega.Expect(resource.Get(writeTok, scopeRead)).To(gomega.Equal(http.StatusForbidden))
	})

	It("rejects an unknown token that this IdP never issued", func() {
		gomega.Expect(resource.Get("not-a-real-token", scopeRead)).
			To(gomega.Equal(http.StatusUnauthorized))
	})

	It("records the presented credential without requiring it in output", func() {
		readTok := tokenFor(env, readHandler)
		gomega.Expect(resource.Get(readTok, scopeRead)).To(gomega.Equal(http.StatusOK))
		gomega.Expect(resource.LastAuthHeader()).To(gomega.Equal("Bearer "+readTok),
			"the resource must actually receive the scoped bearer token")
	})

	It("does not fall back to another handler's token after logout", func() {
		utils.Scafctl("auth", "logout", readHandler).WithEnv(env.Env).Exec()

		// The read handler is now unauthenticated and must fail rather than
		// silently returning the still-valid write handler's token.
		utils.Scafctl("auth", "token", readHandler).WithEnv(env.Env).ExpectFailure().Exec()

		// The write handler's session is unaffected.
		writeTok := tokenFor(env, writeHandler)
		gomega.Expect(resource.Get(writeTok, scopeWrite)).To(gomega.Equal(http.StatusOK))
	})
})
