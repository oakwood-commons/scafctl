// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProfilePropagation_Login verifies that the profile reaches the handler
// when using --profile flag and --auth-profile global flag.
func TestProfilePropagation_Login(t *testing.T) {
	tests := []struct {
		name            string
		profileFlag     string // per-command --profile
		globalProfile   string // simulated --auth-profile (via context)
		expectedProfile string // what the handler should see
	}{
		{
			name:            "per-command --profile flag",
			profileFlag:     "work",
			expectedProfile: "work",
		},
		{
			name:            "global --auth-profile via context",
			globalProfile:   "staging",
			expectedProfile: "staging",
		},
		{
			name:            "per-command overrides global",
			profileFlag:     "personal",
			globalProfile:   "staging",
			expectedProfile: "personal",
		},
		{
			name:            "no profile set",
			expectedProfile: "",
		},
		{
			name:            "--profile default normalizes to empty",
			profileFlag:     "default",
			expectedProfile: "",
		},
		{
			name:            "global default normalizes to empty",
			globalProfile:   "default",
			expectedProfile: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, buf := newTestContext(t)

			mock := auth.NewMockHandler("github")
			mock.SetNotAuthenticated()
			mock.LoginResult = &auth.Result{
				Claims: &auth.Claims{Email: "test@example.com"},
			}

			ctx = withTestHandler(ctx, mock)

			if tc.globalProfile != "" {
				ctx = auth.WithGlobalProfile(ctx, tc.globalProfile)
			}

			cliParams := settings.NewCliParams()
			ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
			cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")
			cmd.SetContext(ctx)

			args := []string{"github"}
			if tc.profileFlag != "" {
				args = append(args, "--profile", tc.profileFlag)
			}
			cmd.SetArgs(args)

			err := cmd.Execute()
			require.NoError(t, err)

			assert.Equal(t, tc.expectedProfile, mock.LastContextProfile,
				"handler should see profile %q but got %q", tc.expectedProfile, mock.LastContextProfile)
		})
	}
}

// TestProfilePropagation_Logout verifies profile reaches handler on logout.
func TestProfilePropagation_Logout(t *testing.T) {
	tests := []struct {
		name            string
		profileFlag     string
		globalProfile   string
		expectedProfile string
	}{
		{
			name:            "per-command --profile flag",
			profileFlag:     "work",
			expectedProfile: "work",
		},
		{
			name:            "global --auth-profile via context",
			globalProfile:   "staging",
			expectedProfile: "staging",
		},
		{
			name:            "per-command overrides global",
			profileFlag:     "personal",
			globalProfile:   "staging",
			expectedProfile: "personal",
		},
		{
			name:            "no profile set",
			expectedProfile: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, buf := newTestContext(t)

			mock := auth.NewMockHandler("github")
			mock.SetAuthenticated(&auth.Claims{Email: "test@example.com"})

			ctx = withTestHandler(ctx, mock)

			if tc.globalProfile != "" {
				ctx = auth.WithGlobalProfile(ctx, tc.globalProfile)
			}

			cliParams := settings.NewCliParams()
			ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
			cmd := CommandLogout(cliParams, ioStreams, "scafctl/auth")
			cmd.SetContext(ctx)

			args := []string{"github"}
			if tc.profileFlag != "" {
				args = append(args, "--profile", tc.profileFlag)
			}
			cmd.SetArgs(args)

			err := cmd.Execute()
			require.NoError(t, err)

			assert.Equal(t, tc.expectedProfile, mock.LastContextProfile,
				"handler should see profile %q but got %q", tc.expectedProfile, mock.LastContextProfile)
		})
	}
}

// TestProfilePropagation_Status verifies profile reaches handler on status check.
func TestProfilePropagation_Status(t *testing.T) {
	tests := []struct {
		name            string
		profileFlag     string
		globalProfile   string
		expectedProfile string
	}{
		{
			name:            "per-command --profile flag",
			profileFlag:     "work",
			expectedProfile: "work",
		},
		{
			name:            "global --auth-profile via context",
			globalProfile:   "staging",
			expectedProfile: "staging",
		},
		{
			name:            "per-command overrides global",
			profileFlag:     "personal",
			globalProfile:   "staging",
			expectedProfile: "personal",
		},
		{
			name:            "no profile set",
			expectedProfile: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, buf := newTestContext(t)

			mock := auth.NewMockHandler("github")
			mock.SetAuthenticated(&auth.Claims{Email: "test@example.com"})
			mock.StatusResult.ExpiresAt = time.Now().Add(time.Hour)

			ctx = withTestHandler(ctx, mock)

			if tc.globalProfile != "" {
				ctx = auth.WithGlobalProfile(ctx, tc.globalProfile)
			}

			cliParams := settings.NewCliParams()
			ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
			cmd := CommandStatus(cliParams, ioStreams, "scafctl/auth")
			cmd.SetContext(ctx)

			args := []string{"github"}
			if tc.profileFlag != "" {
				args = append(args, "--profile", tc.profileFlag)
			}
			cmd.SetArgs(args)

			err := cmd.Execute()
			require.NoError(t, err)

			assert.Equal(t, tc.expectedProfile, mock.LastContextProfile,
				"handler should see profile %q but got %q", tc.expectedProfile, mock.LastContextProfile)
		})
	}
}

// TestProfilePropagation_Token verifies profile reaches handler on token retrieval.
func TestProfilePropagation_Token(t *testing.T) {
	tests := []struct {
		name            string
		profileFlag     string
		globalProfile   string
		expectedProfile string
	}{
		{
			name:            "per-command --profile flag",
			profileFlag:     "work",
			expectedProfile: "work",
		},
		{
			name:            "global --auth-profile via context",
			globalProfile:   "staging",
			expectedProfile: "staging",
		},
		{
			name:            "per-command overrides global",
			profileFlag:     "personal",
			globalProfile:   "staging",
			expectedProfile: "personal",
		},
		{
			name:            "no profile set",
			expectedProfile: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, buf := newTestContext(t)

			mock := auth.NewMockHandler("github")
			mock.SetToken(&auth.Token{
				AccessToken: "test-token-123",
				TokenType:   "Bearer",
				ExpiresAt:   time.Now().Add(time.Hour),
			})

			ctx = withTestHandler(ctx, mock)

			if tc.globalProfile != "" {
				ctx = auth.WithGlobalProfile(ctx, tc.globalProfile)
			}

			cliParams := settings.NewCliParams()
			ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
			cmd := CommandToken(cliParams, ioStreams, "scafctl/auth")
			cmd.SetContext(ctx)

			args := []string{"github"}
			if tc.profileFlag != "" {
				args = append(args, "--profile", tc.profileFlag)
			}
			cmd.SetArgs(args)

			err := cmd.Execute()
			require.NoError(t, err)

			assert.Equal(t, tc.expectedProfile, mock.LastContextProfile,
				"handler should see profile %q but got %q", tc.expectedProfile, mock.LastContextProfile)
		})
	}
}

// TestProfilePropagation_List verifies profile reaches handler on token listing.
func TestProfilePropagation_List(t *testing.T) {
	tests := []struct {
		name            string
		profileFlag     string
		globalProfile   string
		expectedProfile string
	}{
		{
			name:            "per-command --profile flag",
			profileFlag:     "work",
			expectedProfile: "work",
		},
		{
			name:            "global --auth-profile via context",
			globalProfile:   "staging",
			expectedProfile: "staging",
		},
		{
			name:            "per-command overrides global",
			profileFlag:     "personal",
			globalProfile:   "staging",
			expectedProfile: "personal",
		},
		{
			name:            "no profile set",
			expectedProfile: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, buf := newTestContext(t)

			mock := auth.NewMockHandler("github")
			mock.ListCachedTokensResult = []*auth.CachedTokenInfo{
				{
					Handler:   "github",
					TokenKind: "access",
					ExpiresAt: time.Now().Add(time.Hour),
				},
			}

			registry := auth.NewRegistry()
			require.NoError(t, registry.Register(mock))
			ctx = auth.WithRegistry(ctx, registry)

			if tc.globalProfile != "" {
				ctx = auth.WithGlobalProfile(ctx, tc.globalProfile)
			}

			cliParams := settings.NewCliParams()
			ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
			cmd := CommandList(cliParams, ioStreams, "scafctl/auth")
			cmd.SetContext(ctx)

			args := []string{"github"}
			if tc.profileFlag != "" {
				args = append(args, "--profile", tc.profileFlag)
			}
			cmd.SetArgs(args)

			err := cmd.Execute()
			require.NoError(t, err)

			assert.Equal(t, tc.expectedProfile, mock.LastContextProfile,
				"handler should see profile %q but got %q", tc.expectedProfile, mock.LastContextProfile)
		})
	}
}

// TestProfilePropagation_ListPurge verifies profile reaches handler during purge.
func TestProfilePropagation_ListPurge(t *testing.T) {
	tests := []struct {
		name            string
		profileFlag     string
		globalProfile   string
		expectedProfile string
	}{
		{
			name:            "purge with --profile flag",
			profileFlag:     "work",
			expectedProfile: "work",
		},
		{
			name:            "purge with global --auth-profile",
			globalProfile:   "staging",
			expectedProfile: "staging",
		},
		{
			name:            "purge with no profile",
			expectedProfile: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, buf := newTestContext(t)

			mock := auth.NewMockHandler("github")
			mock.PurgeExpiredResult = 0

			registry := auth.NewRegistry()
			require.NoError(t, registry.Register(mock))
			ctx = auth.WithRegistry(ctx, registry)

			if tc.globalProfile != "" {
				ctx = auth.WithGlobalProfile(ctx, tc.globalProfile)
			}

			cliParams := settings.NewCliParams()
			ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
			cmd := CommandList(cliParams, ioStreams, "scafctl/auth")
			cmd.SetContext(ctx)

			args := []string{"github", "--purge-expired"}
			if tc.profileFlag != "" {
				args = append(args, "--profile", tc.profileFlag)
			}
			cmd.SetArgs(args)

			err := cmd.Execute()
			require.NoError(t, err)

			assert.Equal(t, tc.expectedProfile, mock.LastContextProfile,
				"handler should see profile %q but got %q", tc.expectedProfile, mock.LastContextProfile)
		})
	}
}

// TestProfileValidation verifies that invalid profile names are rejected
// across all commands.
func TestProfileValidation_AllCommands(t *testing.T) {
	invalidProfiles := []string{
		"has spaces",
		"has@separator",
		"-starts-with-dash",
		"_starts-with-underscore",
	}

	for _, badProfile := range invalidProfiles {
		t.Run("login/"+badProfile, func(t *testing.T) {
			ctx, buf := newTestContext(t)
			mock := auth.NewMockHandler("github")
			mock.SetNotAuthenticated()
			mock.LoginResult = &auth.Result{Claims: &auth.Claims{Email: "t@t.com"}}
			ctx = withTestHandler(ctx, mock)

			cmd := CommandLogin(settings.NewCliParams(), terminal.NewIOStreams(nil, buf, buf, false), "scafctl/auth")
			cmd.SetContext(ctx)
			cmd.SetArgs([]string{"github", "--profile", badProfile})

			err := cmd.Execute()
			require.Error(t, err, "login should reject invalid profile %q", badProfile)
		})

		t.Run("status/"+badProfile, func(t *testing.T) {
			ctx, buf := newTestContext(t)
			mock := auth.NewMockHandler("github")
			mock.SetAuthenticated(&auth.Claims{Email: "t@t.com"})
			ctx = withTestHandler(ctx, mock)

			cmd := CommandStatus(settings.NewCliParams(), terminal.NewIOStreams(nil, buf, buf, false), "scafctl/auth")
			cmd.SetContext(ctx)
			cmd.SetArgs([]string{"github", "--profile", badProfile})

			err := cmd.Execute()
			require.Error(t, err, "status should reject invalid profile %q", badProfile)
		})

		t.Run("token/"+badProfile, func(t *testing.T) {
			ctx, buf := newTestContext(t)
			mock := auth.NewMockHandler("github")
			mock.SetToken(&auth.Token{AccessToken: "tok", TokenType: "Bearer", ExpiresAt: time.Now().Add(time.Hour)})
			ctx = withTestHandler(ctx, mock)

			cmd := CommandToken(settings.NewCliParams(), terminal.NewIOStreams(nil, buf, buf, false), "scafctl/auth")
			cmd.SetContext(ctx)
			cmd.SetArgs([]string{"github", "--profile", badProfile})

			err := cmd.Execute()
			require.Error(t, err, "token should reject invalid profile %q", badProfile)
		})

		t.Run("logout/"+badProfile, func(t *testing.T) {
			ctx, buf := newTestContext(t)
			mock := auth.NewMockHandler("github")
			mock.SetAuthenticated(&auth.Claims{Email: "t@t.com"})
			ctx = withTestHandler(ctx, mock)

			cmd := CommandLogout(settings.NewCliParams(), terminal.NewIOStreams(nil, buf, buf, false), "scafctl/auth")
			cmd.SetContext(ctx)
			cmd.SetArgs([]string{"github", "--profile", badProfile})

			err := cmd.Execute()
			require.Error(t, err, "logout should reject invalid profile %q", badProfile)
		})

		t.Run("list/"+badProfile, func(t *testing.T) {
			ctx, buf := newTestContext(t)
			mock := auth.NewMockHandler("github")
			registry := auth.NewRegistry()
			require.NoError(t, registry.Register(mock))
			ctx = auth.WithRegistry(ctx, registry)

			cmd := CommandList(settings.NewCliParams(), terminal.NewIOStreams(nil, buf, buf, false), "scafctl/auth")
			cmd.SetContext(ctx)
			cmd.SetArgs([]string{"github", "--profile", badProfile})

			err := cmd.Execute()
			require.Error(t, err, "list should reject invalid profile %q", badProfile)
		})
	}
}

// TestGlobalProfileValidation verifies that invalid global profiles are rejected.
func TestGlobalProfileValidation_AllCommands(t *testing.T) {
	// When --auth-profile is set with an invalid value via context, the CLI
	// commands should still reject it because they validate via
	// auth.ValidateProfileName before setting.
	tests := []struct {
		name    string
		command func(*settings.Run, *terminal.IOStreams, string) *cobra.Command
		args    []string
	}{
		{"login", CommandLogin, []string{"github"}},
		{"status", CommandStatus, []string{"github"}},
		{"token", CommandToken, []string{"github"}},
		{"logout", CommandLogout, []string{"github"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, buf := newTestContext(t)
			mock := auth.NewMockHandler("github")
			mock.SetNotAuthenticated()
			mock.LoginResult = &auth.Result{Claims: &auth.Claims{Email: "t@t.com"}}
			mock.SetToken(&auth.Token{AccessToken: "tok", TokenType: "Bearer", ExpiresAt: time.Now().Add(time.Hour)})
			mock.SetAuthenticated(&auth.Claims{Email: "t@t.com"})
			ctx = withTestHandler(ctx, mock)

			// Set invalid global profile in context
			ctx = auth.WithGlobalProfile(ctx, "has spaces")

			cliParams := settings.NewCliParams()
			ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
			cmd := tc.command(cliParams, ioStreams, "scafctl/auth")
			cmd.SetContext(ctx)
			cmd.SetArgs(tc.args)

			err := cmd.Execute()
			require.Error(t, err, "%s should reject invalid global profile", tc.name)
		})
	}
}
