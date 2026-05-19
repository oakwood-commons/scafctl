// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package authdelegation

import (
	"context"
	"fmt"
	"time"

	manager "github.com/oakwood-commons/go-flight/cache"
	"github.com/oakwood-commons/scafctl/pkg/api/middleware"
	"github.com/oakwood-commons/scafctl/pkg/config"
	httpc "github.com/oakwood-commons/scafctl/pkg/httpc"
)

// TokenDelegator is the top-level facade. One per server.
type TokenDelegator interface {
	DelegateToken(ctx context.Context, scope string) (TokenResult, error)
}

// TokenResult is the normalized output of any delegation strategy.
type TokenResult struct {
	AccessToken string
	ExpiresIn   int64
}

// CredentialType specifies which server authentication method to use.
type CredentialType string

const (
	CredentialTypeWIF    CredentialType = "wif"
	CredentialTypeSecret CredentialType = "secret"
)

// EntraDelegatorConfig holds the static configuration for the Entra delegator.
type EntraDelegatorConfig struct {
	TenantID           string
	ClientID           string
	CredentialType     CredentialType // required: "wif" or "secret"
	FederatedTokenFile string         // required when CredentialType is "wif"
	ClientSecret       string         // required when CredentialType is "secret"
}

// EntraDelegator implements TokenDelegator for Azure/Entra.
// Constructed once at server startup.
type EntraDelegator struct {
	tokenURL     string                                // pre-built: https://login.microsoftonline.com/{tenant}/oauth2/v2.0/token
	clientID     string                                // pre-set from config
	credential   ServerCredential                      // WIF or secret
	flowRegistry *FlowRegistry                         // permitted flows keyed by name
	manager      *manager.Manager[string, TokenResult] // optional; nil disables caching
	httpClient   *httpc.Client                         // HTTP client for token endpoint requests
}

// EntraDelegatorOption configures optional behaviour on an EntraDelegator.
type EntraDelegatorOption func(*EntraDelegator)

// WithManager injects a cache manager for token deduplication and caching.
// If not provided, DelegateToken calls the flow function directly without caching.
func WithManager(mgr *manager.Manager[string, TokenResult]) EntraDelegatorOption {
	return func(d *EntraDelegator) { d.manager = mgr }
}

// WithHTTPClient overrides the default HTTP client used for token endpoint requests.
func WithHTTPClient(client *httpc.Client) EntraDelegatorOption {
	return func(d *EntraDelegator) { d.httpClient = client }
}

// WithFlowRegistry overrides the default flow registry.
func WithFlowRegistry(fr *FlowRegistry) EntraDelegatorOption {
	return func(d *EntraDelegator) { d.flowRegistry = fr }
}

var defaultHTTPClient = httpc.NewClient(
	&httpc.ClientConfig{
		Timeout:     5 * time.Second,
		EnableCache: false,
		RetryMax:    2,
	},
)

// NewEntraDelegator constructs an EntraDelegator from the given config.
// It validates all required fields and pre-wires the flow strategy selector.
func NewEntraDelegator(cfg EntraDelegatorConfig, opts ...EntraDelegatorOption) (*EntraDelegator, error) {
	if cfg.TenantID == "" {
		return nil, EntraNoTenantID
	}
	if cfg.ClientID == "" {
		return nil, EntraNoClientID
	}

	var cred ServerCredential
	switch cfg.CredentialType {
	case CredentialTypeWIF:
		if cfg.FederatedTokenFile == "" {
			return nil, EntraWIFMissingTokenFile
		}
		cred = &WIFCredential{
			TokenFile:           cfg.FederatedTokenFile,
			ClientAssertionType: "urn:ietf:params:oauth:client-assertion-type:jwt-bearer",
		}
	case CredentialTypeSecret:
		if cfg.ClientSecret == "" {
			return nil, EntraSecretMissing
		}
		cred = &SecretCredential{Secret: cfg.ClientSecret}
	default:
		return nil, EntraInvalidCredentialType
	}

	tokenURL := fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", cfg.TenantID)

	d := &EntraDelegator{
		tokenURL:   tokenURL,
		clientID:   cfg.ClientID,
		credential: cred,
		httpClient: defaultHTTPClient,
	}
	for _, opt := range opts {
		opt(d)
	}
	if d.flowRegistry == nil {
		d.flowRegistry = NewFlowRegistry()
		d.flowRegistry.Register("obo", oboFlow(tokenURL, cred, d.httpClient))
		d.flowRegistry.Register("client_credentials", clientCredentialFlow(tokenURL, cred, d.httpClient))
	}
	return d, nil
}

// DelegateToken exchanges the caller's inbound token for a downstream-scoped token.
// It reads the caller token and type from context (set by the auth middleware).
// User callers get OBO delegation, machine callers get client credentials.
// If no Manager was injected, the flow is called directly without caching.
func (d *EntraDelegator) DelegateToken(ctx context.Context, scope string) (TokenResult, error) {
	callerToken := middleware.AccessTokenFromContext(ctx)
	if callerToken == "" {
		return TokenResult{}, fmt.Errorf("no caller token in context")
	}

	var callerType string
	if claims := middleware.ClaimsFromContext(ctx); claims != nil {
		callerType = claims.CallerType()
	}

	params := FlowParams{
		CallerToken: callerToken,
		Scope:       scope,
		ClientID:    d.clientID,
	}
	flow, err := d.flowRegistry.Select(callerType)
	if err != nil {
		return TokenResult{}, err
	}

	if d.manager == nil {
		return flow(ctx, params)
	}

	cacheKey, ok := GenerateKey(callerType, params)
	if !ok {
		return flow(ctx, params)
	}

	return d.manager.Do(ctx, cacheKey, func(ctx context.Context) (manager.FetchResult[TokenResult], error) {
		token, err := flow(ctx, params)
		if err != nil {
			return manager.FetchResult[TokenResult]{}, err
		}
		return manager.FetchResult[TokenResult]{
			Value:  token,
			TTL:    time.Duration(token.ExpiresIn) * time.Second, //nolint:gosec // ExpiresIn is a positive int from OAuth response
			Policy: manager.CacheWithTTL,
		}, nil
	}, nil)
}

const (
	defaultCacheSize             = 1024
	defaultExpiryBuffer          = 10 * time.Minute
	defaultCleanupInterval       = 5 * time.Minute
	defaultExpiryThreshold       = 30 * time.Minute
	defaultSlowThreshold         = 2 * time.Second
	defaultRetryOnFollowerErrors = true
)

// NewEntraDelegatorFromConfig constructs an EntraDelegator from application config.
// It resolves secrets, validates flows, wires a cache manager with defaults, and
// registers only permitted flows in the FlowRegistry.
// The ctx controls the lifetime of the cache cleanup goroutine.
func NewEntraDelegatorFromConfig(ctx context.Context, cfg *config.APIEntraIdentityConfig, opts ...EntraDelegatorOption) (*EntraDelegator, error) {
	if cfg == nil {
		return nil, fmt.Errorf("APIEntraIdentityConfig is required")
	}

	if err := cfg.Credential.Validate(); err != nil {
		return nil, fmt.Errorf("credential: %w", err)
	}

	if err := cfg.AllowedFlows.Validate(); err != nil {
		return nil, fmt.Errorf("allowedFlows: %w", err)
	}

	delegatorCfg := EntraDelegatorConfig{
		TenantID: cfg.TenantID,
		ClientID: cfg.ClientID,
	}

	switch cfg.Credential.Type {
	case "secret":
		secret, err := cfg.Credential.ClientSecret.Resolve()
		if err != nil {
			return nil, fmt.Errorf("resolving client secret: %w", err)
		}
		delegatorCfg.CredentialType = CredentialTypeSecret
		delegatorCfg.ClientSecret = secret
	case "wif":
		delegatorCfg.CredentialType = CredentialTypeWIF
		delegatorCfg.FederatedTokenFile = cfg.Credential.WIFTokenPath
	}

	// Conditionally wire cache manager when TokenManager is configured.
	var managerOpts []EntraDelegatorOption
	if cfg.TokenManager != nil {
		s := resolveTokenManagerDefaults(cfg.TokenManager)
		tokenCache := NewTokenCache[string, TokenResult](ctx, s.CacheSize, s.ExpiryBuffer, s.CleanupInterval)
		mgr := manager.NewManager(
			manager.WithStore[string, TokenResult]("tokens", tokenCache),
			manager.WithExpiryThreshold[string, TokenResult](s.ExpiryThreshold),
			manager.WithSlowThreshold[string, TokenResult](s.SlowThreshold),
			manager.WithRetryFollowerOnError[string, TokenResult](s.RetryOnError),
		)
		managerOpts = append(managerOpts, WithManager(mgr))
	}

	// Build delegator with an empty registry to suppress default flow registration.
	// We set the filtered registry below using the delegator's canonical credential.
	baseOpts := []EntraDelegatorOption{
		WithFlowRegistry(NewFlowRegistry()),
	}
	baseOpts = append(baseOpts, managerOpts...)
	baseOpts = append(baseOpts, opts...)

	delegator, err := NewEntraDelegator(delegatorCfg, baseOpts...)
	if err != nil {
		return nil, err
	}

	// Build filtered FlowRegistry using the delegator's canonical credential.
	registry := NewFlowRegistry()
	if cfg.AllowedFlows.IsFlowPermitted(config.DelegationFlowOBO) {
		registry.Register(config.DelegationFlowOBO, oboFlow(delegator.tokenURL, delegator.credential, delegator.httpClient))
	}
	if cfg.AllowedFlows.IsFlowPermitted(config.DelegationFlowClientCredentials) {
		registry.Register(config.DelegationFlowClientCredentials, clientCredentialFlow(delegator.tokenURL, delegator.credential, delegator.httpClient))
	}
	delegator.flowRegistry = registry

	return delegator, nil
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

// resolveTokenManagerDefaults applies defaults to a TokenManagerConfig.
// All zero values fall back to package-level constants.
func resolveTokenManagerDefaults(tm *config.TokenManagerConfig) tokenManagerSettings {
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
		if d, err := time.ParseDuration(tm.ExpiryBuffer); err == nil {
			s.ExpiryBuffer = d
		}
	}
	if tm.CleanupInterval != "" {
		if d, err := time.ParseDuration(tm.CleanupInterval); err == nil {
			s.CleanupInterval = d
		}
	}
	if tm.ExpiryThreshold != "" {
		if d, err := time.ParseDuration(tm.ExpiryThreshold); err == nil {
			s.ExpiryThreshold = d
		}
	}
	if tm.SlowThreshold != "" {
		if d, err := time.ParseDuration(tm.SlowThreshold); err == nil {
			s.SlowThreshold = d
		}
	}
	if tm.RetryFollowerOnError != nil {
		s.RetryOnError = *tm.RetryFollowerOnError
	}

	return s
}
