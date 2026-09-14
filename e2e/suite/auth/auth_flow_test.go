// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package authtest

import (
	"encoding/json"
	"strings"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot-import is the Ginkgo/Gomega house style.
	"github.com/onsi/gomega"

	"github.com/oakwood-commons/scafctl/e2e/internal/utils"
)

// handlerName is the custom OAuth2 handler registered from the isolated config.
// It must not collide with a reserved first-party handler name (github, gcp,
// entra, openshift), which config-registered handlers are forbidden to shadow.
const handlerName = "e2e-oauth"

// authStatusAuthenticated fetches `auth status <handler> -o json` and reports
// whether the handler row is authenticated. It returns false when the handler
// is present but unauthenticated.
func authStatusAuthenticated(env utils.Isolation) bool {
	session := utils.Scafctl("auth", "status", handlerName, "-o", "json").
		WithEnv(env.Env).Exec()

	var rows []map[string]any
	gomega.Expect(json.Unmarshal(session.Out.Contents(), &rows)).To(gomega.Succeed())
	gomega.Expect(rows).NotTo(gomega.BeEmpty(), "expected a status row for %s", handlerName)

	for _, row := range rows {
		if name, _ := row["handler"].(string); name == handlerName {
			authenticated, _ := row["authenticated"].(bool)
			return authenticated
		}
	}
	Fail("no status row found for handler " + handlerName)
	return false
}

var _ = Describe("auth flow coverage (client_credentials)", func() {
	var (
		env utils.Isolation
		idp *utils.FakeIDP
	)

	BeforeEach(func() {
		idp = utils.NewFakeIDP()
		DeferCleanup(idp.Close)

		env = utils.NewIsolatedEnv()
		env.WriteAuthConfig(handlerName, idp)
	})

	Describe("valid credentials", func() {
		It("logs in, reports authenticated status, and returns a token", func() {
			idp.SetClientCredentials("access-token-valid", "refresh-token-valid", 3600)

			utils.Scafctl("auth", "login", handlerName).
				WithEnv(env.Env).Exec()

			gomega.Expect(authStatusAuthenticated(env)).To(gomega.BeTrue(),
				"handler should be authenticated after a successful login")

			tokenSession := utils.Scafctl("auth", "token", handlerName).
				WithEnv(env.Env).Exec()
			gomega.Expect(strings.TrimSpace(string(tokenSession.Out.Contents()))).
				To(gomega.Equal("access-token-valid"))
		})

		It("records the client identity from the verify endpoint", func() {
			utils.Scafctl("auth", "login", handlerName).WithEnv(env.Env).Exec()

			session := utils.Scafctl("auth", "status", handlerName, "-o", "json").
				WithEnv(env.Env).Exec()
			gomega.Expect(string(session.Out.Contents())).
				To(gomega.ContainSubstring("e2e-user@example.com"))
		})
	})

	Describe("invalid credentials", func() {
		It("fails login, leaves the handler unauthenticated, and returns no token", func() {
			idp.FailToken("invalid_client", "client authentication failed")

			loginSession := utils.Scafctl("auth", "login", handlerName).
				WithEnv(env.Env).ExpectFailure().Exec()
			gomega.Expect(string(loginSession.Err.Contents())).
				To(gomega.ContainSubstring("invalid_client"))

			gomega.Expect(authStatusAuthenticated(env)).To(gomega.BeFalse(),
				"handler must not be authenticated after a failed login")

			utils.Scafctl("auth", "token", handlerName).
				WithEnv(env.Env).ExpectFailure().Exec()
		})
	})

	Describe("token refresh", func() {
		It("transparently refreshes when the cached token lacks required validity", func() {
			idp.SetClientCredentials("access-token-initial", "refresh-token-1", 3600)
			utils.Scafctl("auth", "login", handlerName).WithEnv(env.Env).Exec()

			// Program the refresh grant to mint a distinct token, then demand more
			// validity than the cached token has so GetToken must refresh.
			idp.SetRefreshResponse("access-token-refreshed", 3600)

			tokenSession := utils.Scafctl("auth", "token", handlerName, "--min-valid-for", "2h").
				WithEnv(env.Env).Exec()
			gomega.Expect(strings.TrimSpace(string(tokenSession.Out.Contents()))).
				To(gomega.Equal("access-token-refreshed"))
			gomega.Expect(idp.RefreshCalls()).To(gomega.BeNumerically(">=", 1),
				"expected the refresh_token grant to be exercised")
		})
	})

	Describe("revoked or invalid refresh token", func() {
		It("fails clearly instead of returning a stale token", func() {
			idp.SetClientCredentials("access-token-initial", "refresh-token-1", 3600)
			utils.Scafctl("auth", "login", handlerName).WithEnv(env.Env).Exec()

			idp.FailRefresh("invalid_grant", "refresh token has been revoked")

			utils.Scafctl("auth", "token", handlerName, "--min-valid-for", "2h").
				WithEnv(env.Env).ExpectFailure().Exec()
		})
	})

	Describe("logout", func() {
		It("clears the session so subsequent status and token calls fail", func() {
			utils.Scafctl("auth", "login", handlerName).WithEnv(env.Env).Exec()
			gomega.Expect(authStatusAuthenticated(env)).To(gomega.BeTrue())

			utils.Scafctl("auth", "logout", handlerName).WithEnv(env.Env).Exec()

			gomega.Expect(authStatusAuthenticated(env)).To(gomega.BeFalse(),
				"handler must be unauthenticated after logout")
			utils.Scafctl("auth", "token", handlerName).
				WithEnv(env.Env).ExpectFailure().Exec()
		})
	})
})
