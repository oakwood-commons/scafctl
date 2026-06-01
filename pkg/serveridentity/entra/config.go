package entra

import (
	"context"
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
	defaultCacheSize             = 1024
	defaultExpiryBuffer          = 10 * time.Minute
	defaultCleanupInterval       = 5 * time.Minute
	defaultExpiryThreshold       = 30 * time.Minute
	defaultSlowThreshold         = 500 * time.Millisecond
	defaultRetryOnFollowerErrors = true
)

// NewEntraIdentityFromConfig constructs an Entra identity from application config.
// It resolves secrets, validates credentials, and optionally wires a cache manager.
// The ctx controls the lifetime of the cache cleanup goroutine.
func NewEntraIdentityFromConfig(ctx context.Context, cfg *config.APIEntraIdentityConfig, opts ...Option) (*Entra, error) {
	log := logr.FromContextOrDiscard(ctx)

	if cfg == nil {
		return nil, fmt.Errorf("APIEntraIdentityConfig is required")
	}

	if err := cfg.Credential.Validate(); err != nil {
		return nil, fmt.Errorf("credential: %w", err)
	}

	entraCfg := Config{
		TenantID: cfg.TenantID,
		ClientID: cfg.ClientID,
	}

	switch cfg.Credential.Type {
	case "secret":
		secret, err := cfg.Credential.ClientSecret.Resolve()
		if err != nil {
			return nil, fmt.Errorf("resolving client secret: %w", err)
		}
		entraCfg.CredentialType = CredentialTypeSecret
		entraCfg.ClientSecret = secret
		log.V(1).Info("credential resolved", "type", "secret")
	case "wif":
		entraCfg.CredentialType = CredentialTypeWIF
		entraCfg.FederatedTokenFile = cfg.Credential.WIFTokenPath
		log.V(1).Info("credential resolved", "type", "wif", "tokenFile", cfg.Credential.WIFTokenPath)
	}

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

	identity, err := NewEntraIdentity(entraCfg, allOpts...)
	if err != nil {
		return nil, err
	}

	log.V(1).Info("entra identity initialized",
		"tenantID", cfg.TenantID,
		"clientID", cfg.ClientID,
	)

	return identity, nil
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
