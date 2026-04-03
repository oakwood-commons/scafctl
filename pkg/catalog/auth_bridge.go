// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
)

// RegistryUsernameDefault is the default username for OAuth2 token-based registry auth.
const RegistryUsernameDefault = "oauth2accesstoken"

// RegistryUsernameACR is the Azure Container Registry username for token auth.
// ACR uses a zero-GUID as the username when authenticating with an Entra token.
const RegistryUsernameACR = "00000000-0000-0000-0000-000000000000"

// builtinHandlerNames contains the names of built-in auth handlers.
// Used for name conflict detection when registering custom OAuth2 handlers.
var builtinHandlerNames = []string{"github", "gcp", "entra"}

// BridgeAuthToRegistry converts an auth handler's token into OCI registry credentials.
// Each registry type expects a specific username/password convention:
//   - GitHub (ghcr.io): username=<github-username>, password=<access-token>
//   - GCP (gcr.io, *.pkg.dev): username=oauth2accesstoken, password=<access-token>
//   - Entra (*.azurecr.io): username=00000000-0000-0000-0000-000000000000, password=<access-token>
//   - Generic OAuth2: username=oauth2accesstoken (or custom registryUsername), password=<access-token>
func BridgeAuthToRegistry(ctx context.Context, handler auth.Handler, registryHost, scope string) (string, string, error) {
	opts := auth.TokenOptions{}
	if scope != "" {
		opts.Scope = scope
	}

	token, err := handler.GetToken(ctx, opts)
	if err != nil {
		return "", "", fmt.Errorf("get token from %s handler: %w", handler.Name(), err)
	}

	username, err := registryUsername(ctx, handler, registryHost)
	if err != nil {
		return "", "", fmt.Errorf("determine registry username for %s: %w", registryHost, err)
	}

	return username, token.AccessToken, nil
}

// registryUsername determines the appropriate username for a registry based on the auth handler.
func registryUsername(ctx context.Context, handler auth.Handler, _ string) (string, error) {
	switch handler.Name() {
	case "github":
		// GHCR expects the GitHub username as the registry username
		status, err := handler.Status(ctx)
		if err != nil {
			return "", fmt.Errorf("get auth status: %w", err)
		}
		if status.Claims != nil && status.Claims.Username != "" {
			return status.Claims.Username, nil
		}
		// Fall back to default if no username in claims
		return RegistryUsernameDefault, nil

	case "entra":
		// ACR expects the zero-GUID as username
		return RegistryUsernameACR, nil

	case "gcp":
		// GCR/Artifact Registry expects "oauth2accesstoken"
		return RegistryUsernameDefault, nil

	default:
		// Generic OAuth2 handlers use "oauth2accesstoken" by default
		return RegistryUsernameDefault, nil
	}
}

// InferAuthHandler maps a registry host to a built-in or custom auth handler name.
// Returns empty string if no handler can be inferred for the registry.
func InferAuthHandler(registryHost string, customHandlers []config.CustomOAuth2Config) string {
	// Built-in mappings
	switch {
	case registryHost == "ghcr.io":
		return "github"
	case strings.HasSuffix(registryHost, ".pkg.dev"),
		registryHost == "gcr.io",
		strings.HasSuffix(registryHost, ".gcr.io"):
		return "gcp"
	case strings.HasSuffix(registryHost, ".azurecr.io"):
		return "entra"
	}

	// Check custom OAuth2 handler registry mappings
	for _, h := range customHandlers {
		if h.Registry != "" && h.Registry == registryHost {
			return h.Name
		}
	}

	return ""
}

// IsBuiltinHandlerName returns true if the name conflicts with a built-in handler.
func IsBuiltinHandlerName(name string) bool {
	for _, builtin := range builtinHandlerNames {
		if name == builtin {
			return true
		}
	}
	return false
}
