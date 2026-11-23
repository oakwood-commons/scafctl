// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package credentials implements spec.CredentialResolver backed by the host's
// secret store (pkg/secrets) and auth handler registry (pkg/auth).
package credentials

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	hostnameresolver "github.com/oakwood-commons/scafctl/pkg/auth/hostname"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// Resolver resolves scrt: and tkn: ValueRefs against the context-injected
// secrets.Store and auth.Registry.
type Resolver struct{}

// NewResolver creates a new credential Resolver.
func NewResolver() *Resolver {
	return &Resolver{}
}

// WithResolver attaches r (or a default Resolver if r is nil) to ctx as the
// active spec.CredentialResolver.
func WithResolver(ctx context.Context, r *Resolver) context.Context {
	if r == nil {
		r = NewResolver()
	}
	return spec.WithCredentialResolver(ctx, r)
}

// IsDeniedInternalSecret reports whether name falls under the default internal
// secret prefix ("scafctl.") or the configured embedder binary's internal
// prefix ("<binaryName>.").
func IsDeniedInternalSecret(ctx context.Context, name string) bool {
	if secrets.IsInternalSecret(name) {
		return true
	}
	if appPrefix := secrets.InternalSecretPrefixFor(paths.AppName()); appPrefix != "" && secrets.IsInternalSecretFor(name, appPrefix) {
		return true
	}
	binaryName := settings.BinaryNameFromContext(ctx)
	if binaryName != "" {
		sanitized := settings.SanitizeBinaryName(binaryName)
		if sanitized != "" && secrets.IsInternalSecretFor(name, secrets.InternalSecretPrefixFor(sanitized)) {
			return true
		}
		if secrets.IsInternalSecretFor(name, secrets.InternalSecretPrefixFor(binaryName)) {
			return true
		}
	}
	return false
}

// DeniedSecretPrefixesForContext returns the reserved internal secret prefixes
// ("scafctl." plus "<binaryName>." when a custom binary name is configured).
func DeniedSecretPrefixesForContext(ctx context.Context) []string {
	prefixes := []string{secrets.InternalSecretPrefix}
	candidates := []string{secrets.InternalSecretPrefixFor(paths.AppName())}
	if binaryName := settings.BinaryNameFromContext(ctx); binaryName != "" {
		candidates = append(candidates,
			secrets.InternalSecretPrefixFor(settings.SanitizeBinaryName(binaryName)),
			secrets.InternalSecretPrefixFor(binaryName),
		)
	}
	for _, candidate := range candidates {
		if candidate != "" && candidate != secrets.InternalSecretPrefix {
			already := false
			for _, existing := range prefixes {
				if existing == candidate {
					already = true
					break
				}
			}
			if !already {
				prefixes = append(prefixes, candidate)
			}
		}
	}
	return prefixes
}

// ResolveSecret validates name, enforces internal-prefix isolation, and reads
// the secret value from secrets.FromContext(ctx).
func (r *Resolver) ResolveSecret(ctx context.Context, name string) (string, error) {
	if err := secrets.ValidateName(name); err != nil {
		return "", fmt.Errorf("invalid secret reference %q: %w", name, err)
	}
	if IsDeniedInternalSecret(ctx, name) {
		return "", fmt.Errorf("access denied: secret %q is in a reserved internal namespace", name)
	}

	store := secrets.FromContext(ctx)
	if store == nil {
		return "", fmt.Errorf("secret store not available for secret %q", name)
	}

	val, err := store.Get(ctx, name)
	if err != nil {
		return "", fmt.Errorf("failed to resolve secret %q: %w", name, err)
	}
	return string(val), nil
}

// ResolveToken resolves an auth handler token using auth.RegistryFromContext(ctx),
// honoring profile overrides, per-request scope capability checks, cluster
// hostname resolution, and minimum validity durations.
func (r *Resolver) ResolveToken(ctx context.Context, req spec.TokenRef) (string, error) {
	handlerName := strings.TrimSpace(req.Handler)
	if handlerName == "" {
		return "", fmt.Errorf("invalid token reference: handler must not be empty")
	}
	if auth.RegistryFromContext(ctx) == nil {
		return "", fmt.Errorf("no auth registry available in context for handler %q", handlerName)
	}

	authCtx := ctx
	if req.Profile != "" {
		normalized := auth.NormalizeProfileName(req.Profile)
		if normalized != "" {
			if err := auth.ValidateProfileName(normalized); err != nil {
				return "", fmt.Errorf("invalid profile %q for auth handler %q: %w", req.Profile, handlerName, err)
			}
			authCtx = auth.WithProfile(authCtx, normalized)
		} else {
			authCtx = auth.WithProfile(authCtx, "")
		}
		authCtx = auth.WithProfileResolved(authCtx)
	} else if !auth.IsProfileResolved(authCtx) {
		if active := auth.ResolveActiveProfile(authCtx, handlerName); active != "" {
			authCtx = auth.WithProfile(authCtx, active)
			authCtx = auth.WithProfileResolved(authCtx)
		}
	}

	handler, err := auth.GetHandler(authCtx, handlerName)
	if err != nil {
		return "", fmt.Errorf("auth handler %q not found: %w", handlerName, err)
	}

	caps := handler.Capabilities()
	scope := strings.TrimSpace(req.Scope)
	if scope != "" && !auth.HasCapability(caps, auth.CapScopesOnTokenRequest) {
		return "", fmt.Errorf("auth handler %q does not support per-request scopes (scope %q)", handlerName, scope)
	}

	var tokenHostname string
	if rawHostname := strings.TrimSpace(req.Hostname); rawHostname != "" {
		if !auth.HasCapability(caps, auth.CapTokenHostname) {
			return "", fmt.Errorf("auth handler %q does not support per-request hostname %q (missing %s capability)", handlerName, rawHostname, auth.CapTokenHostname)
		}
		resolved, resolveErr := hostnameresolver.Resolve(authCtx, handlerName, rawHostname)
		if resolveErr != nil {
			return "", fmt.Errorf("failed to resolve hostname %q for auth handler %q: %w", rawHostname, handlerName, resolveErr)
		}
		tokenHostname = resolved
	}

	var minValidFor time.Duration
	if rawMin := strings.TrimSpace(req.MinValidFor); rawMin != "" {
		dur, parseErr := time.ParseDuration(rawMin)
		if parseErr != nil || dur <= 0 {
			return "", fmt.Errorf("invalid minValidFor %q for auth handler %q: must be a positive duration (e.g. 5m, 30s)", req.MinValidFor, handlerName)
		}
		minValidFor = dur
	}

	token, err := handler.GetToken(authCtx, auth.TokenOptions{
		Scope:       scope,
		MinValidFor: minValidFor,
		Hostname:    tokenHostname,
	})
	if err != nil {
		return "", fmt.Errorf("failed to get token from auth handler %q: %w", handlerName, err)
	}
	if token == nil || token.AccessToken == "" {
		return "", fmt.Errorf("auth handler %q returned empty token", handlerName)
	}
	return token.AccessToken, nil
}
