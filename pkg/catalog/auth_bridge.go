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

// OpenShift integrated image-registry host patterns. The registry is exposed
// either through a default route (external) or the in-cluster service DNS
// (internal). Custom/renamed registry routes are not matched here; they are
// handled via config.CustomOAuth2Config.Registry exact-host mappings.
const (
	// openshiftRegistryRoutePrefix matches the default exposed route form,
	// e.g. default-route-openshift-image-registry.apps.<cluster-domain>.
	openshiftRegistryRoutePrefix = "default-route-openshift-image-registry."
	// openshiftRegistrySvcPrefix matches the in-cluster service form,
	// e.g. image-registry.openshift-image-registry.svc(.cluster.local)(:5000).
	openshiftRegistrySvcPrefix = "image-registry.openshift-image-registry.svc"
)

// RegistryUsernameProvider is an optional interface for auth handlers that declare
// a custom registry username convention. BridgeAuthToRegistry checks for this
// interface via type assertion on the default (non-built-in) path.
type RegistryUsernameProvider interface {
	RegistryUsername() string
}

// BridgeAuthToRegistry converts an auth handler's token into OCI registry credentials.
// Each registry type expects a specific username/password convention:
//   - GitHub (ghcr.io): username=<github-username>, password=<access-token>
//   - GCP (gcr.io, *.pkg.dev): username=oauth2accesstoken, password=<access-token>
//   - Entra (*.azurecr.io): username=00000000-0000-0000-0000-000000000000, password=<access-token>
//   - Generic OAuth2: username=oauth2accesstoken (or custom registryUsername), password=<access-token>
func BridgeAuthToRegistry(ctx context.Context, handler auth.Handler, registryHost, scope string) (string, string, error) {
	if handler == nil {
		return "", "", fmt.Errorf("auth handler is nil for registry %s", registryHost)
	}

	opts := auth.TokenOptions{
		Scope:         scope,
		ServerContext: auth.Server,
	}

	token, err := handler.GetToken(ctx, opts)
	if err != nil {
		return "", "", fmt.Errorf("get token from %s handler: %w", handler.Name(), err)
	}
	if token == nil || token.AccessToken == "" {
		return "", "", fmt.Errorf("auth handler %s returned empty token for registry %s", handler.Name(), registryHost)
	}

	username, err := RegistryUsername(ctx, handler, registryHost)
	if err != nil {
		return "", "", fmt.Errorf("determine registry username for %s: %w", registryHost, err)
	}

	return username, token.AccessToken, nil
}

// RegistryUsername determines the appropriate username for a registry based on the auth handler.
func RegistryUsername(ctx context.Context, handler auth.Handler, _ string) (string, error) {
	switch handler.Name() {
	case "github":
		// GHCR expects the GitHub username as the registry username, falling
		// back to the default when Status/claims are unavailable.
		return claimsUsernameOrDefault(ctx, handler), nil

	case "entra":
		// ACR expects the zero-GUID as username
		return RegistryUsernameACR, nil

	case "gcp":
		// GCR/Artifact Registry expects "oauth2accesstoken"
		return RegistryUsernameDefault, nil

	case "openshift":
		// The OpenShift integrated registry accepts the user's token as the
		// password with the OpenShift username as the docker username (like the
		// GitHub case), falling back to the default when unavailable.
		return claimsUsernameOrDefault(ctx, handler), nil

	default:
		// Check if the handler provides a custom registry username override
		// (e.g., "$oauthtoken" for Quay.io configured via CustomOAuth2Config.RegistryUsername).
		if p, ok := handler.(RegistryUsernameProvider); ok {
			if username := p.RegistryUsername(); username != "" {
				return username, nil
			}
		}
		// Generic OAuth2 handlers default to oauth2accesstoken
		return RegistryUsernameDefault, nil
	}
}

// claimsUsernameOrDefault returns the handler's authenticated username from its
// Status claims, falling back to RegistryUsernameDefault when Status is
// unavailable or the claim is empty. Used by registries (GHCR, the OpenShift
// integrated registry) that expect the user's own name as the docker username.
func claimsUsernameOrDefault(ctx context.Context, handler auth.Handler) string {
	status, err := handler.Status(ctx)
	if err != nil {
		return RegistryUsernameDefault
	}
	if status.Claims != nil && status.Claims.Username != "" {
		return status.Claims.Username
	}
	return RegistryUsernameDefault
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
	case isOpenShiftRegistryHost(registryHost):
		return "openshift"
	}

	// Check custom OAuth2 handler registry mappings
	for _, h := range customHandlers {
		if h.Registry != "" && h.Registry == registryHost {
			return h.Name
		}
	}

	return ""
}

// isOpenShiftRegistryHost reports whether host is an OpenShift integrated
// image-registry host: the exposed default route or the in-cluster service DNS.
// The service-form match enforces a boundary after the ".svc" segment (end of
// host, ":" for a port, or "." for a domain suffix) so look-alike hosts such as
// "image-registry.openshift-image-registry.svcevil.example" are not matched and
// cannot be sent an OpenShift token.
func isOpenShiftRegistryHost(host string) bool {
	if strings.HasPrefix(host, openshiftRegistryRoutePrefix) {
		return true
	}
	rest, ok := strings.CutPrefix(host, openshiftRegistrySvcPrefix)
	if !ok {
		return false
	}
	return rest == "" || rest[0] == ':' || rest[0] == '.'
}

// InferDefaultScope returns the default OAuth scope for a known registry host.
// Returns empty string if no default scope is known for the registry.
func InferDefaultScope(registryHost string) string {
	switch {
	case strings.HasSuffix(registryHost, ".pkg.dev"),
		registryHost == "gcr.io",
		strings.HasSuffix(registryHost, ".gcr.io"):
		return "https://www.googleapis.com/auth/cloud-platform"
	}
	return ""
}
