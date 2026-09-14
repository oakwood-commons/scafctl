// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package authtest

import (
	"strings"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot-import is the Ginkgo/Gomega house style.
	"github.com/onsi/gomega"

	"github.com/oakwood-commons/scafctl/e2e/internal/utils"
)

// Distinctive sentinel token values so a plaintext scan of the on-disk secret
// store cannot pass vacuously: if the store ever wrote these in cleartext, the
// scan would find them.
const (
	sentinelAccessToken    = "SENTINEL-ACCESS-TOKEN-9f8e7d6c5b4a"
	sentinelRefreshToken   = "SENTINEL-REFRESH-TOKEN-1a2b3c4d5e6f"   //nolint:gosec // test sentinel, not a real credential
	sentinelRefreshedToken = "SENTINEL-REFRESHED-TOKEN-2b3c4d5e6f7a" //nolint:gosec // test sentinel, not a real credential
)

var _ = Describe("token storage security", func() {
	var (
		env utils.Isolation
		idp *utils.FakeIDP
	)

	BeforeEach(func() {
		idp = utils.NewFakeIDP()
		DeferCleanup(idp.Close)
		idp.SetClientCredentials(sentinelAccessToken, sentinelRefreshToken, 3600)

		env = utils.NewIsolatedEnv()
		env.WriteAuthConfig(handlerName, idp)

		utils.Scafctl("auth", "login", handlerName).WithEnv(env.Env).Exec()
	})

	It("persists the access token only as an encrypted blob, never in plaintext", func() {
		// Round-trip first: the token must be retrievable, proving it really was
		// persisted (and can be decrypted) -- otherwise the plaintext scan below
		// would pass simply because nothing was stored.
		gomega.Expect(tokenFor(env, handlerName)).To(gomega.Equal(sentinelAccessToken))

		// The store must have written at least one encrypted secret file.
		gomega.Expect(env.EncSecretFiles()).NotTo(gomega.BeEmpty(),
			"expected the secret store to persist encrypted credential material")

		// Neither the access token nor the refresh token may appear in cleartext
		// anywhere under the isolated environment (secrets, cache, config, logs).
		env.ExpectNoPlaintextSecrets(sentinelAccessToken, sentinelRefreshToken)
	})

	It("still contains no plaintext token after a refresh rewrites the cache", func() {
		// Program a *distinct* refreshed access token so the assertions below
		// cannot pass by reusing the originally cached token: only a genuine
		// refresh_token grant yields this value.
		idp.SetRefreshResponse(sentinelRefreshedToken, 3600)

		// Precondition: the cached token is still the login-issued one, and no
		// refresh has happened yet.
		gomega.Expect(tokenFor(env, handlerName)).To(gomega.Equal(sentinelAccessToken))
		gomega.Expect(idp.RefreshCalls()).To(gomega.Equal(0))

		// Force the refresh path (login token lives 1h; demand 2h) so the cache
		// entry is rewritten, and prove the CLI now returns the refreshed token.
		session := utils.Scafctl("auth", "token", handlerName, "--min-valid-for", "2h").
			WithEnv(env.Env).Exec()
		gomega.Expect(strings.TrimSpace(string(session.Out.Contents()))).
			To(gomega.Equal(sentinelRefreshedToken))

		// Exactly one refresh_token grant occurred, and it presented the stored
		// refresh token -- so the cache was genuinely rewritten, not reused.
		gomega.Expect(idp.RefreshCalls()).To(gomega.Equal(1))
		var refreshReqs []utils.FakeIDPRequest
		for _, req := range idp.Requests() {
			if req.GrantType == "refresh_token" {
				refreshReqs = append(refreshReqs, req)
			}
		}
		gomega.Expect(refreshReqs).To(gomega.HaveLen(1))
		gomega.Expect(refreshReqs[0].Form.Get("refresh_token")).To(gomega.Equal(sentinelRefreshToken))

		// Neither the original tokens nor the freshly refreshed access token may
		// appear in cleartext anywhere in the isolated environment.
		env.ExpectNoPlaintextSecrets(sentinelAccessToken, sentinelRefreshToken, sentinelRefreshedToken)
	})
})
