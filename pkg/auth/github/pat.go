// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package github provides GitHub authentication for scafctl.
// This file implements the personal access token (PAT) flow.
package github

import (
	"context"
	"fmt"
	"os"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/logger"
)

// PAT environment variable names (following GitHub CLI conventions).
const (
	// EnvGitHubToken is the environment variable for the GitHub token.
	// Used by GitHub Actions and many CI systems.
	EnvGitHubToken = "GITHUB_TOKEN" //nolint:gosec // This is the env var name, not a credential

	// EnvGHToken is the environment variable used by the GitHub CLI (gh).
	EnvGHToken = "GH_TOKEN" //nolint:gosec // This is the env var name, not a credential

	// EnvGHHost is the environment variable for the GitHub hostname (for GHES).
	EnvGHHost = "GH_HOST"
)

// GetPATFromEnv retrieves a personal access token from environment variables.
// Returns empty string if no token is configured.
// GITHUB_TOKEN takes precedence over GH_TOKEN.
func GetPATFromEnv() string {
	if token := os.Getenv(EnvGitHubToken); token != "" {
		return token
	}
	return os.Getenv(EnvGHToken)
}

// HasPATCredentials checks if PAT credentials are configured in environment.
func HasPATCredentials() bool {
	return GetPATFromEnv() != ""
}

// GetHostnameFromEnv retrieves the GitHub hostname from environment variables.
// Returns empty string if not configured.
func GetHostnameFromEnv() string {
	return os.Getenv(EnvGHHost)
}

// patLogin validates a PAT by calling the GitHub API.
func (h *Handler) patLogin(ctx context.Context, _ auth.LoginOptions) (*auth.Result, error) {
	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("starting PAT login", "handler", HandlerName)

	token := GetPATFromEnv()
	if token == "" {
		return nil, fmt.Errorf("personal access token not configured: set %s or %s environment variable",
			EnvGitHubToken, EnvGHToken)
	}

	// Validate the token by fetching user info
	claims, err := h.fetchUserClaims(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("PAT authentication failed: %w", err)
	}

	lgr.V(1).Info("PAT authentication successful",
		"login", claims.Subject,
		"name", claims.Name,
	)

	return &auth.Result{
		Claims: claims,
		// PATs don't have a defined expiry via the API
		// They are valid until revoked
	}, nil
}

// patStatus returns auth status when PAT credentials are present.
func (h *Handler) patStatus(ctx context.Context) (*auth.Status, error) {
	token := GetPATFromEnv()
	if token == "" {
		return &auth.Status{Authenticated: false}, nil
	}

	// Validate the token
	claims, err := h.fetchUserClaims(ctx, token)
	if err != nil {
		return &auth.Status{Authenticated: false}, nil //nolint:nilerr // invalid token means not authenticated
	}

	return &auth.Status{
		Authenticated: true,
		Claims:        claims,
		IdentityType:  auth.IdentityTypeServicePrincipal, // PAT acts like a service principal
		Scopes:        h.config.DefaultScopes,
	}, nil
}

// getPATToken returns a token from the PAT environment variable.
func (h *Handler) getPATToken(ctx context.Context, opts auth.TokenOptions) (*auth.Token, error) {
	return getCachedOrAcquireToken(
		ctx,
		h,
		opts,
		GetPATFromEnv,
		func(s string) bool { return s == "" },
		func(ctx context.Context, token, _ string) (*auth.Token, error) {
			// Validate the PAT
			_, err := h.fetchUserClaims(ctx, token)
			if err != nil {
				return nil, fmt.Errorf("PAT validation failed: %w", err)
			}
			return &auth.Token{
				AccessToken: token,
				TokenType:   "Bearer",
				// PATs don't expire via API, set a long validity
				ExpiresAt: farFuture(),
				Scope:     opts.Scope,
			}, nil
		},
		"pat",
	)
}
