// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package authdelegation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	manager "github.com/oakwood-commons/go-flight/cache"
	"github.com/oakwood-commons/scafctl/pkg/api/middleware"
	"github.com/oakwood-commons/scafctl/pkg/config"
	httpc "github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	defaultCacheSize             = 1024
	defaultExpiryBuffer          = 10 * time.Minute
	defaultCleanupInterval       = 5 * time.Minute
	defaultExpiryThreshold       = 30 * time.Minute
	defaultSlowThreshold         = 500 * time.Millisecond
	defaultRetryOnFollowerErrors = true

	CredentialTypeWIF    CredentialType = "wif"
	CredentialTypeSecret CredentialType = "secret"
)

var (
	defaultHTTPClientOnce sync.Once
	defaultHTTPClientVal  *httpc.Client
)

// TokenDelegator is the top-level facade. One per server.
type TokenDelegator interface {
	Name() string
	DelegateToken(ctx context.Context, scope string) (TokenResult, error)
}

// TokenResult is the normalized output of any delegation strategy.
type TokenResult struct {
	AccessToken string
	ExpiresIn   int64
}

// CredentialType specifies which server authentication method to use.
type CredentialType string

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
	logger       logr.Logger                           // structured logger for delegation operations
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

// WithLogger overrides the logger used for delegation operations.
func WithLogger(log logr.Logger) EntraDelegatorOption {
	return func(d *EntraDelegator) { d.logger = log }
}

func defaultHTTPClient() *httpc.Client {
	defaultHTTPClientOnce.Do(func() {
		defaultHTTPClientVal = httpc.NewClient(
			&httpc.ClientConfig{
				Timeout:     5 * time.Second,
				EnableCache: false,
				RetryMax:    2,
			},
		)
	})
	return defaultHTTPClientVal
}

// NewEntraDelegator constructs an EntraDelegator from the given config.
// It validates all required fields and pre-wires the flow strategy selector.
func NewEntraDelegator(cfg EntraDelegatorConfig, opts ...EntraDelegatorOption) (*EntraDelegator, error) {
	if cfg.TenantID == "" {
		return nil, ErrEntraNoTenantID
	}
	if cfg.ClientID == "" {
		return nil, ErrEntraNoClientID
	}

	var cred ServerCredential
	switch cfg.CredentialType {
	case CredentialTypeWIF:
		if cfg.FederatedTokenFile == "" {
			return nil, ErrEntraWIFMissingTokenFile
		}
		cred = &WIFCredential{
			TokenFile:           cfg.FederatedTokenFile,
			ClientAssertionType: "urn:ietf:params:oauth:client-assertion-type:jwt-bearer",
		}
	case CredentialTypeSecret:
		if cfg.ClientSecret == "" {
			return nil, ErrEntraSecretMissing
		}
		cred = &SecretCredential{Secret: cfg.ClientSecret}
	default:
		return nil, ErrEntraInvalidCredentialType
	}

	tokenURL := fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", cfg.TenantID)

	d := &EntraDelegator{
		tokenURL:   tokenURL,
		clientID:   cfg.ClientID,
		credential: cred,
		httpClient: defaultHTTPClient(),
	}
	for _, opt := range opts {
		opt(d)
	}
	if d.logger.GetSink() == nil {
		d.logger = logr.Discard()
	}
	if d.flowRegistry == nil {
		d.flowRegistry = NewFlowRegistry()
		d.flowRegistry.Register("obo", oboFlow(tokenURL, cred, d.httpClient))
		d.flowRegistry.Register("client_credentials", clientCredentialFlow(tokenURL, cred, d.httpClient))
	}
	return d, nil
}

func (d *EntraDelegator) Name() string {
	return "entra"
}

// DelegateToken exchanges the caller's inbound token for a downstream-scoped token.
// It reads the caller token and type from context (set by the auth middleware).
// User callers get OBO delegation, machine callers get client credentials.
// If no Manager was injected, the flow is called directly without caching.
func (d *EntraDelegator) DelegateToken(ctx context.Context, scope string) (TokenResult, error) {
	log := logger.FromContext(ctx)

	ctx, span := telemetry.Tracer(telemetry.TracerAuthDelegation).Start(ctx, "authdelegation.DelegateToken",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attribute.String("delegation.scope", scope)),
	)
	defer span.End()

	callerToken := middleware.AccessTokenFromContext(ctx)
	if callerToken == "" {
		return TokenResult{}, ErrNoCallerToken
	}

	if scope == "" {
		return TokenResult{}, ErrNoScope
	}

	var callerType string
	if claims := middleware.ClaimsFromContext(ctx); claims != nil {
		callerType = claims.CallerType()
	}

	flowName := FlowNameForCaller(callerType)
	span.SetAttributes(
		attribute.String("delegation.callerType", callerType),
		attribute.String("delegation.flow", flowName),
	)
	if log.V(1).Enabled() {
		log.V(1).Info("delegating token", "callerType", callerType, "flow", flowName, "scope", scope)
	}

	params := FlowParams{
		CallerToken: callerToken,
		Scope:       scope,
		ClientID:    d.clientID,
	}
	flow, err := d.flowRegistry.Select(callerType)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		log.Info("flow selection failed", "callerType", callerType, "error", err)
		return TokenResult{}, err
	}

	if d.manager == nil {
		if log.V(2).Enabled() {
			log.V(2).Info("executing flow directly", "flow", flowName)
		}
		return flow(ctx, params)
	}

	cacheKey, ok := GenerateKey(callerType, params)
	if !ok {
		if log.V(2).Enabled() {
			log.V(2).Info("cache key generation failed, bypassing cache", "flow", flowName)
		}
		return flow(ctx, params)
	}
	hooks := &manager.Hooks{
		OnCacheHit: func(source string) {
			span.AddEvent("cache.hit", trace.WithAttributes(attribute.String("cache.source", source)))
			if log.V(2).Enabled() {
				log.V(2).Info("cache hit", "source", source, "flow", flowName)
			}
		},
		OnSuccess: func() {
			span.SetAttributes(attribute.Bool("delegation.cacheHit", false))
			if log.V(2).Enabled() {
				log.V(2).Info("flow execution succeeded", "flow", flowName)
			}
		},
		OnFetchError: func(err error) {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			log.Info("flow execution failed", "flow", flowName, "error", err)
		},
	}
	return d.manager.Do(ctx, cacheKey, func(ctx context.Context) (manager.FetchResult[TokenResult], error) {
		if log.V(2).Enabled() {
			log.V(2).Info("cache miss, executing flow", "flow", flowName, "cacheKey", cacheKey)
		}
		token, err := flow(ctx, params)
		if err != nil {
			return manager.FetchResult[TokenResult]{}, err
		}
		return manager.FetchResult[TokenResult]{
			Value:  token,
			TTL:    time.Duration(token.ExpiresIn) * time.Second, //nolint:gosec // ExpiresIn is a positive int from OAuth response
			Policy: manager.CacheWithTTL,
		}, nil
	}, hooks)
}

// NewEntraDelegatorFromConfig constructs an EntraDelegator from application config.
// It resolves secrets, validates flows, wires a cache manager with defaults, and
// registers only permitted flows in the FlowRegistry.
// The ctx controls the lifetime of the cache cleanup goroutine.
func NewEntraDelegatorFromConfig(ctx context.Context, cfg *config.APIEntraIdentityConfig, opts ...EntraDelegatorOption) (*EntraDelegator, error) {
	log := logger.FromContext(ctx)

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
		log.V(1).Info("credential resolved", "type", "secret")
	case "wif":
		delegatorCfg.CredentialType = CredentialTypeWIF
		delegatorCfg.FederatedTokenFile = cfg.Credential.WIFTokenPath
		log.V(1).Info("credential resolved", "type", "wif", "tokenFile", cfg.Credential.WIFTokenPath)
	}

	// Conditionally wire cache manager when TokenManager is configured.
	var managerOpts []EntraDelegatorOption
	if cfg.TokenManager != nil {
		s, err := resolveTokenManagerDefaults(cfg.TokenManager)
		if err != nil {
			return nil, fmt.Errorf("token manager config: %w", err)
		}
		log.V(1).Info("token manager configured",
			"cacheSize", s.CacheSize,
			"expiryBuffer", s.ExpiryBuffer,
			"cleanupInterval", s.CleanupInterval,
			"expiryThreshold", s.ExpiryThreshold,
			"slowThreshold", s.SlowThreshold,
			"retryOnError", s.RetryOnError,
		)
		tokenCache := NewTokenCache[string, TokenResult](ctx, s.CacheSize, s.ExpiryBuffer, s.CleanupInterval)
		mgr := manager.NewManager(
			manager.WithStore("tokens", tokenCache),
			manager.WithExpiryThreshold[string, TokenResult](s.ExpiryThreshold),
			manager.WithSlowThreshold[string, TokenResult](s.SlowThreshold),
			manager.WithRetryFollowerOnError[string, TokenResult](s.RetryOnError),
			manager.WithRequestIDExtractor[string, TokenResult](middleware.FlightIDExtractor()),
		)
		managerOpts = append(managerOpts, WithManager(mgr))
	}

	// Build delegator with an empty registry to suppress default flow registration.
	// We set the filtered registry below using the delegator's canonical credential.
	baseOpts := make([]EntraDelegatorOption, 0, 1+len(managerOpts)+len(opts))
	baseOpts = append(baseOpts, WithFlowRegistry(NewFlowRegistry()))
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

	var permittedFlows []string
	if cfg.AllowedFlows != nil {
		permittedFlows = cfg.AllowedFlows.Flows
	}
	log.V(1).Info("entra delegator initialized",
		"tenantID", cfg.TenantID,
		"clientID", cfg.ClientID,
		"permittedFlows", permittedFlows,
	)

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
// Returns an error if any duration string is non-empty but unparseable.
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
