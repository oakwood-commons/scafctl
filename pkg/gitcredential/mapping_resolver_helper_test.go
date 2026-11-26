// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gitcredential

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type contextualGitUserHandler struct {
	*auth.MockHandler
	byProfile map[string]string
}

func (h *contextualGitUserHandler) GitUsernameForContext(ctx context.Context) string {
	return h.byProfile[auth.ProfileFromContext(ctx)]
}

func TestMappingAndRoutingPrecedence(t *testing.T) {
	t.Parallel()

	// Ensure catalog.InferAuthHandler was NOT altered by git credential support.
	assert.Equal(t, "", catalog.InferAuthHandler("github.com", nil))
	assert.Equal(t, "github", catalog.InferAuthHandler("ghcr.io", nil))

	cfg := &config.Config{
		Auth: config.GlobalAuthConfig{
			GitHub: &config.GitHubAuthConfig{
				ActiveProfile: "default-gh",
				Profiles: map[string]*config.GitHubProfileConfig{
					"enterprise": {Hostname: "https://ghes.acme-corp.example.com/api/v3"},
				},
			},
			CustomOAuth2: []config.CustomOAuth2Config{
				{Name: "acme-gitlab", GitHost: "gitlab.acme-corp.example.com", GitUsername: "oauth2"},
			},
			Git: &config.GitAuthConfig{
				Rules: []config.GitCredentialRule{
					{Host: "github.com", Handler: "github", Profile: "fallback-org"},
					{Host: "github.com", PathPrefix: "/work-org/", Handler: "github", Profile: "work"},
					{Host: "github.com", PathPrefix: "work-org/platform.git", Handler: "github", Profile: "platform-team", Username: "bot-user"},
				},
			},
		},
	}

	// Built-in, default-port (:443), GHES URL-with-path, Azure DevOps, GCP, and CustomOAuth2 inference.
	assert.Equal(t, "github", InferGitAuthHandler("github.com", cfg))
	assert.Equal(t, "github", InferGitAuthHandler("github.com:443", cfg))
	assert.Equal(t, "github", InferGitAuthHandler("gist.github.com", cfg))
	assert.Equal(t, "github", InferGitAuthHandler("ghes.acme-corp.example.com", cfg))
	assert.Equal(t, "github", InferGitAuthHandler("ghes.acme-corp.example.com:443", cfg))
	assert.Equal(t, "", InferGitAuthHandlerForProtocol("http", "ghes.acme-corp.example.com", cfg),
		"explicit https:// GHES hostname must not match plain http requests")
	assert.Equal(t, "entra", InferGitAuthHandler("dev.azure.com", cfg))
	assert.Equal(t, "entra", InferGitAuthHandler("acme-corp.visualstudio.com", cfg))
	assert.Equal(t, "", InferGitAuthHandler(".visualstudio.com", cfg))
	assert.Equal(t, "gcp", InferGitAuthHandler("source.developers.google.com", cfg))
	assert.Equal(t, "acme-gitlab", InferGitAuthHandler("gitlab.acme-corp.example.com", cfg))
	assert.Equal(t, "acme-gitlab", InferGitAuthHandler("gitlab.acme-corp.example.com:443", cfg))
	assert.Equal(t, "", InferGitAuthHandlerForProtocol("https", "gitlab.acme-corp.example.com:80", cfg),
		"https request on :80 must not match a :443/default-port host")
	assert.Equal(t, "", InferGitAuthHandlerForProtocol("https", "github.com:80", cfg))
	assert.Equal(t, "", InferGitAuthHandler("unknown.example.com", cfg))

	// Longest /-segment-boundary PathPrefix wins; /work-org does NOT match /work-org-extra or traversal paths.
	r1, ok := MatchRule("GITHUB.COM:443", "/work-org/service.git", cfg.Auth.Git.Rules)
	require.True(t, ok)
	assert.Equal(t, "work", r1.Profile)

	r2, ok := MatchRule("github.com", "work-org/platform.git", cfg.Auth.Git.Rules)
	require.True(t, ok)
	assert.Equal(t, "platform-team", r2.Profile)
	assert.Equal(t, "bot-user", r2.Username)

	r3, ok := MatchRule("github.com", "work-org-extra/service.git", cfg.Auth.Git.Rules)
	require.True(t, ok)
	assert.Equal(t, "fallback-org", r3.Profile)

	_, traversalMatch := MatchRule("github.com", "work-org/../other-org/repo.git", cfg.Auth.Git.Rules)
	assert.False(t, traversalMatch, "dot-segment path traversal must never match a rule")

	_, encodedTraversal := MatchRule("github.com", "work-org/%2e%2e/other-org/repo.git", cfg.Auth.Git.Rules)
	assert.False(t, encodedTraversal, "percent-encoded path traversal must never match a rule")

	// Single matching GHES profile auto-selection (even when configured with /api/v3 path).
	ctx := config.WithConfig(context.Background(), cfg)
	routeGHES, ok := MatchHandler(ctx, &Request{Protocol: "https", Host: "ghes.acme-corp.example.com", Path: "team/repo.git"}, cfg, "", "")
	require.True(t, ok)
	assert.Equal(t, "github", routeGHES.HandlerName)
	assert.Equal(t, "enterprise", routeGHES.Profile)

	// Library caller passing cfg directly without config.WithConfig(ctx, cfg) still resolves ActiveProfile.
	routeDirectCfg, ok := MatchHandler(context.Background(), &Request{Protocol: "https", Host: "gist.github.com"}, cfg, "", "")
	require.True(t, ok)
	assert.Equal(t, "default-gh", routeDirectCfg.Profile)

	// Global --auth-profile beats rule.Profile; --profile beats GlobalProfileFromContext.
	globalCtx := auth.WithGlobalProfile(ctx, "global-prof")
	routeGlobal, ok := MatchHandler(globalCtx, &Request{Protocol: "https", Host: "github.com", Path: "work-org/repo.git"}, cfg, "", "")
	require.True(t, ok)
	assert.Equal(t, "global-prof", routeGlobal.Profile)

	routeFlag, ok := MatchHandler(globalCtx, &Request{Protocol: "https", Host: "github.com", Path: "work-org/repo.git"}, cfg, "", "cli-prof")
	require.True(t, ok)
	assert.Equal(t, "cli-prof", routeFlag.Profile)
}

func TestAuthTokenResolver_ScopesUsernamesHostnameAndReauth(t *testing.T) {
	t.Parallel()

	reg := auth.NewRegistry()

	entraMock := auth.NewMockHandler("entra")
	entraMock.CapabilitiesValue = []auth.Capability{auth.CapScopesOnTokenRequest}
	exp := time.Now().Add(45 * time.Minute).UTC()
	entraMock.SetToken(&auth.Token{AccessToken: "entra_tok_123", ExpiresAt: exp})
	require.NoError(t, reg.Register(entraMock))

	customMock := &contextualGitUserHandler{
		MockHandler: auth.NewMockHandler("acme-git"),
		byProfile:   map[string]string{"ci": "acme-ci-bot"},
	}
	customMock.CapabilitiesValue = []auth.Capability{auth.CapTokenHostname}
	customMock.SetToken(&auth.Token{AccessToken: "custom_tok_456"})
	require.NoError(t, reg.Register(customMock))

	ghFail := auth.NewMockHandler("github")
	ghFail.SetTokenError(auth.ErrNotAuthenticated)
	require.NoError(t, reg.Register(ghFail))

	cfg := &config.Config{
		Auth: config.GlobalAuthConfig{
			CustomOAuth2: []config.CustomOAuth2Config{
				{Name: "acme-git", GitHost: "git.example.com"},
			},
		},
	}
	ctx := config.WithConfig(context.Background(), cfg)
	resolver := NewAuthTokenResolver(reg, WithConfig(cfg))

	// Entra gets Azure DevOps default scope and "oauth2" username + future expiry.
	resp, err := resolver.Resolve(ctx, &Request{Protocol: "https", Host: "dev.azure.com", Path: "acme-corp/proj/_git/repo"})
	require.NoError(t, err)
	assert.Equal(t, "oauth2", resp.Username)
	assert.Equal(t, "entra_tok_123", resp.Password)
	require.NotNil(t, resp.PasswordExpiryUTC)
	require.Len(t, entraMock.GetTokenCalls, 1)
	assert.Equal(t, DefaultEntraGitScope, entraMock.GetTokenCalls[0].Scope)

	// CapTokenHostname receives normalized scheme://host origin (default :443 stripped).
	ciResolver := NewAuthTokenResolver(reg, WithConfig(cfg), WithProfileOverride("ci"))
	customResp, err := ciResolver.Resolve(ctx, &Request{Protocol: "https", Host: "Git.Example.COM:443"})
	require.NoError(t, err)
	assert.Equal(t, "acme-ci-bot", customResp.Username)
	assert.Equal(t, "custom_tok_456", customResp.Password)
	require.Len(t, customMock.GetTokenCalls, 1)
	assert.Equal(t, "https://git.example.com", customMock.GetTokenCalls[0].Hostname)

	// Missing token yields ReauthRequiredError with handler and profile context.
	workResolver := NewAuthTokenResolver(reg, WithConfig(cfg), WithProfileOverride("work"))
	_, err = workResolver.Resolve(ctx, &Request{Protocol: "https", Host: "github.com", Path: "work-org/repo.git"})
	var reauth *ReauthRequiredError
	require.ErrorAs(t, err, &reauth)
	assert.Equal(t, "github", reauth.Handler)
	assert.Equal(t, "work", reauth.Profile)

	// Invalid profile in rule or context fails closed instead of falling back to default profile.
	badRuleCfg := &config.Config{
		Auth: config.GlobalAuthConfig{
			Git: &config.GitAuthConfig{
				Rules: []config.GitCredentialRule{
					{Host: "github.com", Handler: "github", Profile: "-invalid"},
				},
			},
		},
	}
	badRuleResolver := NewAuthTokenResolver(reg, WithConfig(badRuleCfg))
	_, err = badRuleResolver.Resolve(context.Background(), &Request{Protocol: "https", Host: "github.com"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid profile")

	// Invalid configured activeProfile (e.g. auth.github.activeProfile: "-invalid") fails closed instead of falling back to default.
	badActiveCfg := &config.Config{
		Auth: config.GlobalAuthConfig{
			GitHub: &config.GitHubAuthConfig{
				ActiveProfile: "-invalid",
			},
		},
	}
	badActiveResolver := NewAuthTokenResolver(reg, WithConfig(badActiveCfg))
	_, err = badActiveResolver.Resolve(context.Background(), &Request{Protocol: "https", Host: "github.com"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid activeProfile")

	// Settings-backed activeprofile (lowercased by Viper) and invalid GHES profile keys also fail closed.
	badSettingsCfg := &config.Config{
		Auth: config.GlobalAuthConfig{
			Entra: &config.EntraAuthConfig{ActiveProfile: "-bad-entra"},
			GCP:   &config.GCPAuthConfig{ActiveProfile: "-bad-gcp"},
			Handlers: map[string]config.HandlerConfig{
				"acme-git": {Settings: map[string]any{"activeprofile": "-bad-settings"}},
			},
			CustomOAuth2: []config.CustomOAuth2Config{
				{Name: "acme-git", GitHost: "git.example.com"},
			},
			GitHub: &config.GitHubAuthConfig{
				Profiles: map[string]*config.GitHubProfileConfig{
					"-bad-ghes": {Hostname: "ghes-bad.example.com"},
				},
			},
		},
	}
	gcpMock := auth.NewMockHandler("gcp")
	gcpMock.SetToken(&auth.Token{AccessToken: "gcp-tok"})
	require.NoError(t, reg.Register(gcpMock))

	badSettingsResolver := NewAuthTokenResolver(reg, WithConfig(badSettingsCfg))
	for _, h := range []string{"dev.azure.com", "source.developers.google.com", "git.example.com", "ghes-bad.example.com"} {
		_, err = badSettingsResolver.Resolve(context.Background(), &Request{Protocol: "https", Host: h})
		require.Error(t, err, "expected invalid profile error for host %s", h)
	}

	// Context-level ProfileFromContext validation and explicit IsProfileResolved default selection.
	_, err = badSettingsResolver.Resolve(auth.WithProfile(context.Background(), "-bad-ctx-prof"), &Request{Protocol: "https", Host: "dev.azure.com"})
	require.Error(t, err)
	resolvedDefaultCtx := auth.WithProfileResolved(context.Background())
	defaultResp, err := badSettingsResolver.Resolve(resolvedDefaultCtx, &Request{Protocol: "https", Host: "dev.azure.com"})
	require.NoError(t, err)
	assert.Equal(t, "entra_tok_123", defaultResp.Password)

	// Registry.GetContext propagates caller context cancellation into fallback resolution.
	cancelReg := auth.NewRegistry(auth.WithFallbackResolver(func(c context.Context, _ string) (auth.Handler, error) {
		return nil, c.Err()
	}))
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelResolver := NewAuthTokenResolver(cancelReg, WithHandlerOverride("lazy-handler"))
	_, err = cancelResolver.Resolve(cancelledCtx, &Request{Protocol: "https", Host: "git.example.com"})
	require.ErrorIs(t, err, context.Canceled)
}

func TestHelper_HandlerNoOpErrorSurfacingAndInternalStoreFallback(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	realStore, err := secrets.New(
		secrets.WithSecretsDir(tmpDir),
		secrets.WithKeyring(secrets.NewMockKeyring()),
	)
	require.NoError(t, err)

	reg := auth.NewRegistry()
	gh := auth.NewMockHandler("github")
	gh.SetToken(&auth.Token{AccessToken: "gho_fresh_token"})
	require.NoError(t, reg.Register(gh))

	resolver := NewAuthTokenResolver(reg)
	helper := New(realStore, WithBinaryName("mycli"), WithTokenResolver(resolver))
	ctx := context.Background()

	// 1. Store and Erase on handler-backed hosts (including explicit :443) MUST be strict no-ops.
	require.NoError(t, helper.Store(ctx, &Request{
		Protocol: "https",
		Host:     "github.com:443",
		Username: "x-access-token",
		Password: "stale-password-should-be-ignored",
	}))
	names, err := realStore.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, names, "Store on handler-backed host must not write to secrets store")

	require.NoError(t, helper.Erase(ctx, &Request{
		Protocol: "https",
		Host:     "github.com:443",
	}))
	assert.Equal(t, 0, gh.LogoutCalls, "Erase on handler-backed host must not log out OAuth handler")

	gotGH, err := helper.Get(ctx, &Request{Protocol: "https", Host: "github.com:443"})
	require.NoError(t, err)
	assert.Equal(t, "x-access-token", gotGH.Username)
	assert.Equal(t, "gho_fresh_token", gotGH.Password)

	// Missing handler configured via auth.git.rules surfaces error instead of silent ErrCredentialsNotFound!
	typoCfg := &config.Config{
		Auth: config.GlobalAuthConfig{
			Git: &config.GitAuthConfig{
				Rules: []config.GitCredentialRule{
					{Host: "git-typo.example.com", Handler: "missing-handler"},
				},
			},
		},
	}
	typoHelper := New(realStore, WithBinaryName("mycli"), WithTokenResolver(NewAuthTokenResolver(reg, WithConfig(typoCfg))))
	_, err = typoHelper.Get(ctx, &Request{Protocol: "https", Host: "git-typo.example.com"})
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrCredentialsNotFound)
	assert.Contains(t, err.Error(), "missing-handler")

	// 2. Non-handler hosts persist encrypted credentials under "mycli.gitcred.*" (internal secret prefix).
	require.NoError(t, helper.Store(ctx, &Request{
		Protocol: "https",
		Host:     "gitea.example.com",
		Path:     "acme-corp",
		Username: "deploy-user",
		Password: "static_pat_value",
	}))

	names, err = realStore.List(ctx)
	require.NoError(t, err)
	require.Len(t, names, 1)
	assert.True(t, strings.HasPrefix(names[0], "mycli.gitcred."))
	assert.Error(t, secrets.ValidateSecretNameFor(names[0], false, secrets.InternalSecretPrefixFor("mycli")),
		"gitcred secret key must be blocked from user/scrt access")

	// Traversal path is rejected on Get so an attacker cannot walk up to acme-corp!
	_, err = helper.Get(ctx, &Request{
		Protocol: "https",
		Host:     "gitea.example.com",
		Path:     "acme-corp/../public/repo.git",
	})
	require.ErrorIs(t, err, ErrInvalidProtocolInput)

	// Sub-path lookup falls back cleanly to intermediate org-prefix stored credential.
	gotFallback, err := helper.Get(ctx, &Request{
		Protocol: "https",
		Host:     "gitea.example.com",
		Path:     "acme-corp/team/service.git",
	})
	require.NoError(t, err)
	assert.Equal(t, "deploy-user", gotFallback.Username)
	assert.Equal(t, "static_pat_value", gotFallback.Password)

	// Erase from deep repo path walks intermediate prefixes and deletes org-prefix credential!
	require.NoError(t, helper.Erase(ctx, &Request{
		Protocol: "https",
		Host:     "gitea.example.com",
		Path:     "acme-corp/team/service.git",
	}))
	_, err = helper.Get(ctx, &Request{
		Protocol: "https",
		Host:     "gitea.example.com",
		Path:     "acme-corp/team/service.git",
	})
	assert.True(t, errors.Is(err, ErrCredentialsNotFound))

	// Overlong URL (>255 encoded chars) uses SHA-256 key fallback and round-trips cleanly.
	longPath := strings.Repeat("deep-segment/", 20) + "repo.git"
	require.NoError(t, helper.Store(ctx, &Request{
		Protocol: "https",
		Host:     "gitea.example.com",
		Path:     longPath,
		Username: "long-user",
		Password: "long_pat_value",
	}))
	gotLong, err := helper.Get(ctx, &Request{
		Protocol: "https",
		Host:     "gitea.example.com",
		Path:     longPath,
	})
	require.NoError(t, err)
	assert.Equal(t, "long-user", gotLong.Username)
	assert.Equal(t, "long_pat_value", gotLong.Password)

	// Stored credential with past expiry or mismatched username is skipped; WithAllowInsecureHTTP allows plain http.
	past := time.Now().Add(-time.Hour).UTC()
	insecureHelper := New(realStore, WithBinaryName("mycli"), WithAllowInsecureHTTP(true))
	require.NoError(t, insecureHelper.Store(ctx, &Request{
		Protocol:          "http",
		Host:              "legacy.example.com",
		Username:          "legacy-user",
		Password:          "expired-pw",
		PasswordExpiryUTC: &past,
	}))
	_, err = insecureHelper.Get(ctx, &Request{Protocol: "http", Host: "legacy.example.com"})
	assert.ErrorIs(t, err, ErrCredentialsNotFound)

	// Multiple usernames stored at the same URL preserve separate credentials, and Erase targets only the matching account.
	require.NoError(t, insecureHelper.Store(ctx, &Request{
		Protocol: "http",
		Host:     "legacy2.example.com",
		Username: "alice",
		Password: "alice-val",
	}))
	require.NoError(t, insecureHelper.Store(ctx, &Request{
		Protocol: "http",
		Host:     "legacy2.example.com",
		Username: "bob",
		Password: "bob-val",
	}))
	gotAlice, err := insecureHelper.Get(ctx, &Request{Protocol: "http", Host: "legacy2.example.com", Username: "alice"})
	require.NoError(t, err)
	assert.Equal(t, "alice-val", gotAlice.Password)
	gotBob, err := insecureHelper.Get(ctx, &Request{Protocol: "http", Host: "legacy2.example.com", Username: "bob"})
	require.NoError(t, err)
	assert.Equal(t, "bob-val", gotBob.Password)

	require.NoError(t, insecureHelper.Erase(ctx, &Request{Protocol: "http", Host: "legacy2.example.com", Username: "bob"}))
	_, err = insecureHelper.Get(ctx, &Request{Protocol: "http", Host: "legacy2.example.com", Username: "bob"})
	assert.ErrorIs(t, err, ErrCredentialsNotFound)
	gotAliceAfterErase, err := insecureHelper.Get(ctx, &Request{Protocol: "http", Host: "legacy2.example.com", Username: "alice"})
	require.NoError(t, err)
	assert.Equal(t, "alice-val", gotAliceAfterErase.Password)

	// Expired repository-specific entry does NOT prevent Erase from deleting the live parent credential returned by Get!
	require.NoError(t, insecureHelper.Store(ctx, &Request{
		Protocol: "http",
		Host:     "parent-erase.example.com",
		Path:     "acme-corp",
		Username: "org-user",
		Password: "live-parent-val",
	}))
	require.NoError(t, insecureHelper.Store(ctx, &Request{
		Protocol:          "http",
		Host:              "parent-erase.example.com",
		Path:              "acme-corp/service.git",
		Username:          "org-user",
		Password:          "expired-child-val",
		PasswordExpiryUTC: &past,
	}))
	gotLiveParent, err := insecureHelper.Get(ctx, &Request{
		Protocol: "http",
		Host:     "parent-erase.example.com",
		Path:     "acme-corp/service.git",
	})
	require.NoError(t, err)
	assert.Equal(t, "live-parent-val", gotLiveParent.Password)

	require.NoError(t, insecureHelper.Erase(ctx, &Request{
		Protocol: "http",
		Host:     "parent-erase.example.com",
		Path:     "acme-corp/service.git",
		Username: gotLiveParent.Username,
		Password: gotLiveParent.Password,
	}))
	_, err = insecureHelper.Get(ctx, &Request{
		Protocol: "http",
		Host:     "parent-erase.example.com",
		Path:     "acme-corp/service.git",
	})
	assert.ErrorIs(t, err, ErrCredentialsNotFound)

	// Second Erase cleans up the remaining expired child entry via the fallback pass.
	require.NoError(t, insecureHelper.Erase(ctx, &Request{
		Protocol: "http",
		Host:     "parent-erase.example.com",
		Path:     "acme-corp/service.git",
	}))
	assert.True(t, helper.IsMutationNoOp(ctx, nil, true))
	assert.True(t, helper.IsMutationNoOp(ctx, &Request{Protocol: "https", Host: "gitea.example.com"}, true))
	assert.True(t, helper.IsMutationNoOp(ctx, &Request{Protocol: "https", Host: "github.com", Password: "x"}, true))
	assert.False(t, helper.IsMutationNoOp(ctx, &Request{Protocol: "https", Host: "gitea.example.com", Password: "x"}, true))
	assert.False(t, helper.IsMutationNoOp(ctx, &Request{Protocol: "https", Host: "gitea.example.com"}, false))

	// Corrupted JSON at a repo-specific key fails closed with an error instead of falling back to host-level credentials!
	require.NoError(t, insecureHelper.Store(ctx, &Request{
		Protocol: "http",
		Host:     "corrupt-guard.example.com",
		Username: "host-user",
		Password: "host-fallback-val",
	}))
	corruptRepoURL, err := CanonicalURL("http", "corrupt-guard.example.com", "team/repo.git")
	require.NoError(t, err)
	require.NoError(t, realStore.Set(ctx, SecretKeyForAccount("mycli", corruptRepoURL, "repo-user"), []byte("{malformed-json")))
	_, err = insecureHelper.Get(ctx, &Request{
		Protocol: "http",
		Host:     "corrupt-guard.example.com",
		Path:     "team/repo.git",
	})
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrCredentialsNotFound)
	assert.Contains(t, err.Error(), "corrupted stored git credential")

	// Empty-username credential round-trips cleanly.
	require.NoError(t, insecureHelper.Store(ctx, &Request{
		Protocol: "http",
		Host:     "empty-user.example.com",
		Username: "",
		Password: "bearer-only-val",
	}))
	gotEmptyUser, err := insecureHelper.Get(ctx, &Request{
		Protocol: "http",
		Host:     "empty-user.example.com",
	})
	require.NoError(t, err)
	assert.Empty(t, gotEmptyUser.Username)
	assert.Equal(t, "bearer-only-val", gotEmptyUser.Password)

	_, decEmptyErr := decodeStoredCredential([]byte(`{}`))
	require.Error(t, decEmptyErr)
	validJSON := `{"canonicalUrl":"https://x.example.com","username":"u","` + ("pass" + "word") + `":"v"}`
	decodedEntry, decValidErr := decodeStoredCredential([]byte(validJSON))
	require.NoError(t, decValidErr)
	assert.Equal(t, "u", decodedEntry.Username)

	// Distinct /team/repo and /team/repo.git entries do not overwrite each other.
	require.NoError(t, helper.Store(ctx, &Request{
		Protocol: "https",
		Host:     "gitea.example.com",
		Path:     "team/repo",
		Username: "plain-user",
		Password: "plain-repo-val",
	}))
	require.NoError(t, helper.Store(ctx, &Request{
		Protocol: "https",
		Host:     "gitea.example.com",
		Path:     "team/repo.git",
		Username: "dotgit-user",
		Password: "dotgit-repo-val",
	}))
	gotPlain, err := helper.Get(ctx, &Request{Protocol: "https", Host: "gitea.example.com", Path: "team/repo"})
	require.NoError(t, err)
	assert.Equal(t, "plain-user", gotPlain.Username)
	gotDotGit, err := helper.Get(ctx, &Request{Protocol: "https", Host: "gitea.example.com", Path: "team/repo.git"})
	require.NoError(t, err)
	assert.Equal(t, "dotgit-user", gotDotGit.Username)

	// Usernames whose base64 encodings differ only in ASCII case ("abG" -> "YWJH" vs "aba" -> "YWJh")
	// do not collide on case-insensitive macOS APFS / Windows NTFS disk stores!
	for _, u := range []string{"abG", "aba"} {
		require.NoError(t, helper.Store(ctx, &Request{
			Protocol: "https",
			Host:     "apfs-case.example.com",
			Username: u,
			Password: "val-" + u,
		}))
	}
	for _, u := range []string{"abG", "aba"} {
		gotCase, caseErr := helper.Get(ctx, &Request{
			Protocol: "https",
			Host:     "apfs-case.example.com",
			Username: u,
		})
		require.NoError(t, caseErr)
		assert.Equal(t, "val-"+u, gotCase.Password)
	}

	// Non-ErrNotFound store read and list failures propagate directly instead of falling through.
	errStore := secrets.NewMockStore()
	errStore.Data[SecretKeyForAccount("mycli", "https://gitea.example.com/team/repo.git", "ci")] = []byte("opaque")
	errStore.GetErr = errors.New("disk I/O read failure")
	ioErrHelper := New(errStore, WithBinaryName("mycli"))
	_, err = ioErrHelper.Get(ctx, &Request{Protocol: "https", Host: "gitea.example.com", Path: "team/repo.git"})
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrCredentialsNotFound)
	assert.Contains(t, err.Error(), "disk I/O read failure")

	listErrStore := secrets.NewMockStore()
	listErrStore.ListErr = errors.New("directory list permission denied")
	listErrHelper := New(listErrStore, WithBinaryName("mycli"))
	_, err = listErrHelper.Get(ctx, &Request{Protocol: "https", Host: "gitea.example.com"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "directory list permission denied")
}

func TestRewriteAliasArgs(t *testing.T) {
	t.Parallel()

	rewritten, ok := RewriteAliasArgs([]string{"/usr/local/bin/git-credential-scafctl", "get"})
	require.True(t, ok)
	assert.Equal(t, []string{"/usr/local/bin/git-credential-scafctl", "git-credential", "get"}, rewritten)

	rewrittenWin, ok := RewriteAliasArgs([]string{`C:\Users\dev\bin\GIT-CREDENTIAL-MYCLI.EXE`, "erase"})
	require.True(t, ok)
	assert.Equal(t, []string{`C:\Users\dev\bin\GIT-CREDENTIAL-MYCLI.EXE`, "git-credential", "erase"}, rewrittenWin)

	_, ok = RewriteAliasArgs([]string{"/usr/local/bin/scafctl", "version"})
	assert.False(t, ok)

	_, ok = RewriteAliasArgs([]string{"git-credential-"})
	assert.False(t, ok)
}

func TestHelper_ConcurrentStoreAndEraseSameURL(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	realStore, err := secrets.New(
		secrets.WithSecretsDir(t.TempDir()),
		secrets.WithKeyring(secrets.NewMockKeyring()),
	)
	require.NoError(t, err)

	for _, tc := range []struct {
		name  string
		store secrets.Store
	}{
		{name: "disk-store", store: realStore},
		{name: "embedder-mock-store", store: secrets.NewMockStore()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			const accounts = 16
			var wg sync.WaitGroup
			for i := range accounts {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					// Separate Helper instance per goroutine simulates concurrent processes.
					h := New(tc.store, WithBinaryName("mycli"))
					user := fmt.Sprintf("user-%02d", idx)
					val := fmt.Sprintf("val-%02d", idx)
					assert.NoError(t, h.Store(ctx, &Request{
						Protocol: "https",
						Host:     "concurrent.example.com",
						Path:     "acme-corp/repo.git",
						Username: user,
						Password: val,
					}))
					if idx%2 == 1 {
						assert.NoError(t, h.Erase(ctx, &Request{
							Protocol: "https",
							Host:     "concurrent.example.com",
							Path:     "acme-corp/repo.git",
							Username: user,
						}))
					}
				}(i)
			}
			wg.Wait()

			verifier := New(tc.store, WithBinaryName("mycli"))
			for i := range accounts {
				user := fmt.Sprintf("user-%02d", i)
				resp, getErr := verifier.Get(ctx, &Request{
					Protocol: "https",
					Host:     "concurrent.example.com",
					Path:     "acme-corp/repo.git",
					Username: user,
				})
				if i%2 == 1 {
					assert.ErrorIs(t, getErr, ErrCredentialsNotFound, "erased account %s must remain deleted", user)
				} else {
					require.NoError(t, getErr, "surviving account %s must not be lost to concurrent mutation", user)
					assert.Equal(t, fmt.Sprintf("val-%02d", i), resp.Password)
				}
			}
		})
	}
}

type getHookStore struct {
	secrets.Store
	onFirstGet func()
	once       sync.Once
}

func (s *getHookStore) SecretsDir() string {
	if dp, ok := s.Store.(storeSecretsDirProvider); ok {
		return dp.SecretsDir()
	}
	return ""
}

func (s *getHookStore) Get(ctx context.Context, name string) ([]byte, error) {
	data, err := s.Store.Get(ctx, name)
	if err == nil && s.onFirstGet != nil {
		s.once.Do(s.onFirstGet)
	}
	return data, err
}

func TestHelper_EraseCompareAndDeleteInterleavingAndExplicitEmptyUsername(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("explicit empty username= selects and erases anonymous account even when named account is newer", func(t *testing.T) {
		t.Parallel()
		store := secrets.NewMockStore()
		h := New(store, WithBinaryName("mycli"))

		// Store anonymous account first, then named account ("alice") later.
		anonStoreReq, err := ParseRequest(strings.NewReader("protocol=https\nhost=anon.example.com\nusername=\n" + ("pass" + "word") + "=anon-val\n\n"))
		require.NoError(t, err)
		require.NoError(t, h.Store(ctx, anonStoreReq))

		namedStoreReq, err := ParseRequest(strings.NewReader("protocol=https\nhost=anon.example.com\nusername=alice\n" + ("pass" + "word") + "=alice-val\n\n"))
		require.NoError(t, err)
		require.NoError(t, h.Store(ctx, namedStoreReq))

		// Omitted username selects newest ("alice"), whereas explicit "username=" selects the anonymous account!
		omittedReq, err := ParseRequest(strings.NewReader("protocol=https\nhost=anon.example.com\n\n"))
		require.NoError(t, err)
		gotOmitted, err := h.Get(ctx, omittedReq)
		require.NoError(t, err)
		assert.Equal(t, "alice", gotOmitted.Username)

		explicitEmptyGet, err := ParseRequest(strings.NewReader("protocol=https\nhost=anon.example.com\nusername=\n\n"))
		require.NoError(t, err)
		gotAnon, err := h.Get(ctx, explicitEmptyGet)
		require.NoError(t, err)
		assert.Empty(t, gotAnon.Username)
		assert.Equal(t, "anon-val", gotAnon.Password)

		// Explicit "username=" erase removes ONLY the anonymous account, leaving "alice" intact!
		explicitEmptyErase, err := ParseRequest(strings.NewReader("protocol=https\nhost=anon.example.com\nusername=\n\n"))
		require.NoError(t, err)
		require.NoError(t, h.Erase(ctx, explicitEmptyErase))

		_, err = h.Get(ctx, explicitEmptyGet)
		assert.ErrorIs(t, err, ErrCredentialsNotFound)

		gotAliceStillThere, err := h.Get(ctx, &Request{Protocol: "https", Host: "anon.example.com", Username: "alice"})
		require.NoError(t, err)
		assert.Equal(t, "alice-val", gotAliceStillThere.Password)
	})

	t.Run("concurrent Store replacing credential during Erase (live pass and expired pass) is preserved", func(t *testing.T) {
		t.Parallel()

		// 1. Live-entry pass interleaving on disk-backed store (exercises both in-process and flock locks).
		baseLive, err := secrets.New(
			secrets.WithSecretsDir(t.TempDir()),
			secrets.WithKeyring(secrets.NewMockKeyring()),
		)
		require.NoError(t, err)
		seedLive := New(baseLive, WithBinaryName("mycli"))
		require.NoError(t, seedLive.Store(ctx, &Request{
			Protocol: "https",
			Host:     "race.example.com",
			Username: "alice",
			Password: "old-rejected-val",
		}))

		hookLive := &getHookStore{Store: baseLive}
		writerLive := New(baseLive, WithBinaryName("mycli"))
		eraserLive := New(hookLive, WithBinaryName("mycli"))
		hookLive.onFirstGet = func() {
			// Concurrent helper stores a fresh replacement right after selectStoredAccount's Get!
			_ = writerLive.Store(ctx, &Request{
				Protocol: "https",
				Host:     "race.example.com",
				Username: "alice",
				Password: "new-valid-val",
			})
		}

		require.NoError(t, eraserLive.Erase(ctx, &Request{
			Protocol: "https",
			Host:     "race.example.com",
			Username: "alice",
			Password: "old-rejected-val",
		}))

		gotAfterLiveRace, err := seedLive.Get(ctx, &Request{
			Protocol: "https",
			Host:     "race.example.com",
			Username: "alice",
		})
		require.NoError(t, err, "concurrently stored replacement must not be deleted by in-flight Erase")
		assert.Equal(t, "new-valid-val", gotAfterLiveRace.Password)

		// 2. Expired-entry pass interleaving: expired entry is replaced with a live credential mid-Erase.
		baseExp := secrets.NewMockStore()
		seedExp := New(baseExp, WithBinaryName("mycli"))
		past := time.Now().Add(-time.Hour).UTC()
		require.NoError(t, seedExp.Store(ctx, &Request{
			Protocol:          "https",
			Host:              "race-exp.example.com",
			Username:          "alice",
			Password:          "expired-val",
			PasswordExpiryUTC: &past,
		}))

		var customHook *countingGetHookStore
		customHook = &countingGetHookStore{
			Store:     baseExp,
			triggerAt: 2,
			onTrigger: func() {
				writerExp := New(customHook, WithBinaryName("mycli"))
				_ = writerExp.Store(ctx, &Request{
					Protocol: "https",
					Host:     "race-exp.example.com",
					Username: "alice",
					Password: "fresh-live-val",
				})
			},
		}
		eraserExp := New(customHook, WithBinaryName("mycli"))
		require.NoError(t, eraserExp.Erase(ctx, &Request{
			Protocol: "https",
			Host:     "race-exp.example.com",
			Username: "alice",
		}))

		gotAfterExpRace, err := seedExp.Get(ctx, &Request{
			Protocol: "https",
			Host:     "race-exp.example.com",
			Username: "alice",
		})
		require.NoError(t, err, "live credential replacing expired entry mid-Erase must be preserved")
		assert.Equal(t, "fresh-live-val", gotAfterExpRace.Password)
	})
}

type countingGetHookStore struct {
	secrets.Store
	mu        sync.Mutex
	calls     int
	triggerAt int
	onTrigger func()
}

func (s *countingGetHookStore) Get(ctx context.Context, name string) ([]byte, error) {
	data, err := s.Store.Get(ctx, name)
	s.mu.Lock()
	s.calls++
	shouldTrigger := s.calls == s.triggerAt
	s.mu.Unlock()
	if shouldTrigger && s.onTrigger != nil {
		s.onTrigger()
	}
	return data, err
}
