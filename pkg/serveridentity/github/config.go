// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	manager "github.com/oakwood-commons/go-flight/cache"
	"github.com/oakwood-commons/scafctl/pkg/api/middleware"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/serveridentity"
	"github.com/oakwood-commons/scafctl/pkg/tokenprovider"
)

const (
	defaultCacheSize             = 2
	defaultExpiryBuffer          = 5 * time.Minute
	defaultCleanupInterval       = 0 // no periodic cleanup; rely on expiry buffer and on-demand eviction
	defaultExpiryThreshold       = 10 * time.Minute
	defaultSlowThreshold         = 500 * time.Millisecond
	defaultRetryOnFollowerErrors = true
)

// NewGitHubIdentityFromConfig constructs a GitHub identity from application config.
// It resolves credentials, validates the configuration, and returns the appropriate
// TokenProvider implementation based on the credential type.
func NewGitHubIdentityFromConfig(ctx context.Context, cfg *config.APIGitHubIdentityConfig, opts ...Option) (tokenprovider.TokenProvider, error) {
	log := logr.FromContextOrDiscard(ctx)

	if cfg == nil {
		return nil, fmt.Errorf("APIGitHubIdentityConfig is required")
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	switch cfg.Credential.Type {
	case "app":
		return buildAppIdentity(ctx, log, cfg, opts...)
	case "pat":
		return buildPATIdentity(ctx, log, cfg)
	default:
		return nil, fmt.Errorf("unsupported credential type: %q", cfg.Credential.Type)
	}
}

func buildPATIdentity(_ context.Context, log logr.Logger, cfg *config.APIGitHubIdentityConfig) (*PAT, error) {
	token, err := cfg.Credential.PAT.Token.Resolve()
	if err != nil {
		return nil, fmt.Errorf("resolving PAT token: %w", err)
	}

	log.V(1).Info("credential resolved", "type", "pat", "hostname", cfg.EffectiveHostname())

	return NewPATIdentity(token)
}

func buildAppIdentity(ctx context.Context, log logr.Logger, cfg *config.APIGitHubIdentityConfig, opts ...Option) (*App, error) {
	keyPEM, err := cfg.Credential.App.PrivateKey.Resolve()
	if err != nil {
		return nil, fmt.Errorf("resolving private key: %w", err)
	}

	privateKey, err := parsePrivateKey([]byte(keyPEM))
	if err != nil {
		return nil, fmt.Errorf("parsing private key: %w", err)
	}

	hostname := cfg.EffectiveHostname()
	log.V(1).Info("credential resolved", "type", "app", "clientID", cfg.Credential.App.ClientID, "hostname", hostname)

	var managerOpts []Option
	if cfg.TokenManager != nil {
		mgr, err := buildManager(ctx, cfg.TokenManager)
		if err != nil {
			return nil, fmt.Errorf("token manager config: %w", err)
		}
		managerOpts = append(managerOpts, WithManager(mgr))
	}

	allOpts := make([]Option, 0, len(managerOpts)+len(opts))
	allOpts = append(allOpts, managerOpts...)
	allOpts = append(allOpts, opts...)

	identity, err := NewGitHubAppIdentity(
		cfg.Credential.App.ClientID,
		cfg.Credential.App.InstallationID,
		privateKey,
		hostname,
		allOpts...,
	)
	if err != nil {
		return nil, err
	}

	log.V(1).Info("github identity initialized",
		"clientID", cfg.Credential.App.ClientID,
		"installationID", cfg.Credential.App.InstallationID,
		"hostname", hostname,
	)

	return identity, nil
}

// parsePrivateKey decodes a PEM-encoded RSA private key (PKCS1 or PKCS8).
func parsePrivateKey(pemData []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in private key data")
	}

	// Try PKCS1 first (RSA PRIVATE KEY).
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}

	// Try PKCS8 (PRIVATE KEY).
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("key is neither PKCS1 nor PKCS8: %w", err)
	}

	rsaKey, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("PKCS8 key is not RSA (got %T)", parsed)
	}

	return rsaKey, nil
}

// tokenManagerSettings holds resolved token manager configuration values.
type tokenManagerSettings struct {
	CacheSize       int
	ExpiryBuffer    time.Duration
	CleanupInterval time.Duration
	ExpiryThreshold time.Duration
	SlowThreshold   time.Duration
	RetryOnError    bool
}

func buildManager(ctx context.Context, tmCfg *config.TokenManagerConfig) (*manager.Manager[string, tokenprovider.Token], error) {
	s, err := resolveTokenManagerDefaults(tmCfg)
	if err != nil {
		return nil, err
	}

	tokenCache := serveridentity.NewTokenCache[string, tokenprovider.Token](ctx, s.CacheSize, s.ExpiryBuffer, s.CleanupInterval)
	mgr := manager.NewManager(
		manager.WithStore("tokens", tokenCache),
		manager.WithExpiryThreshold[string, tokenprovider.Token](s.ExpiryThreshold),
		manager.WithSlowThreshold[string, tokenprovider.Token](s.SlowThreshold),
		manager.WithRetryFollowerOnError[string, tokenprovider.Token](s.RetryOnError),
		manager.WithRequestIDExtractor[string, tokenprovider.Token](middleware.FlightIDExtractor()),
	)
	return mgr, nil
}

func resolveTokenManagerDefaults(tm *config.TokenManagerConfig) (tokenManagerSettings, error) {
	s := tokenManagerSettings{
		CacheSize:       defaultCacheSize,
		ExpiryBuffer:    defaultExpiryBuffer,
		CleanupInterval: defaultCleanupInterval,
		ExpiryThreshold: defaultExpiryThreshold,
		SlowThreshold:   defaultSlowThreshold,
		RetryOnError:    defaultRetryOnFollowerErrors,
	}

	if tm.CacheSize != 0 {
		s.CacheSize = tm.CacheSize
	}
	if tm.ExpiryBuffer != "" {
		d, err := time.ParseDuration(tm.ExpiryBuffer)
		if err != nil {
			return tokenManagerSettings{}, fmt.Errorf("parsing expiryBuffer %q: %w", tm.ExpiryBuffer, err)
		}
		s.ExpiryBuffer = d
	}
	if tm.CleanupInterval != "" {
		d, err := time.ParseDuration(tm.CleanupInterval)
		if err != nil {
			return tokenManagerSettings{}, fmt.Errorf("parsing cleanupInterval %q: %w", tm.CleanupInterval, err)
		}
		s.CleanupInterval = d
	}
	if tm.ExpiryThreshold != "" {
		d, err := time.ParseDuration(tm.ExpiryThreshold)
		if err != nil {
			return tokenManagerSettings{}, fmt.Errorf("parsing expiryThreshold %q: %w", tm.ExpiryThreshold, err)
		}
		s.ExpiryThreshold = d
	}
	if tm.SlowThreshold != "" {
		d, err := time.ParseDuration(tm.SlowThreshold)
		if err != nil {
			return tokenManagerSettings{}, fmt.Errorf("parsing slowThreshold %q: %w", tm.SlowThreshold, err)
		}
		s.SlowThreshold = d
	}
	if tm.RetryFollowerOnError != nil {
		s.RetryOnError = *tm.RetryFollowerOnError
	}

	return s, nil
}
