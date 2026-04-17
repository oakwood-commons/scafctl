// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package entra

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
)

const (
	// HandlerName is the unique identifier for the Entra auth handler.
	HandlerName = "entra"

	// HandlerDisplayName is the human-readable name.
	HandlerDisplayName = "Microsoft Entra ID"

	// SecretKeyRefreshToken is the secret name for storing the refresh token.
	SecretKeyRefreshToken = "scafctl.auth.entra.refresh_token" //nolint:gosec // This is a key name, not a credential

	// SecretKeyMetadata is the secret name for storing token metadata.
	SecretKeyMetadata = "scafctl.auth.entra.metadata" //nolint:gosec // This is a key name, not a credential

	// SecretKeyTokenPrefix is the prefix for cached access tokens.
	// Full key format: scafctl.auth.entra.token.<base64url-encoded-scope>
	SecretKeyTokenPrefix = "scafctl.auth.entra.token." //nolint:gosec // This is a key prefix, not a credential

	// DefaultTimeout is the default timeout for device code flow.
	DefaultTimeout = 5 * time.Minute

	// DefaultRefreshTokenLifetime is the expected lifetime of refresh tokens.
	// Azure AD refresh tokens are valid for 90 days by default.
	DefaultRefreshTokenLifetime = 90 * 24 * time.Hour
)

// authHTTPLogLevel is the verbosity offset applied to the base logger
// before it is handed to the HTTP transport used by auth handlers.
// This ensures auth HTTP calls only appear at high verbosity levels
// (e.g. base V(0) + offset 5 → effective V(5)).
const authHTTPLogLevel = 5

// Handler implements auth.Handler for Microsoft Entra ID.
type Handler struct {
	config           *Config
	secretStore      secrets.Store
	secretErr        error // deferred error from secrets initialization
	httpClient       HTTPClient
	httpClientConfig *config.HTTPClientConfig
	graphClient      GraphClient
	tokenCache       *auth.TokenCache
	oboCache         *oboCache
	logger           logr.Logger
}

// Option configures the Handler.
type Option func(*Handler)

// WithConfig sets the Entra configuration.
func WithConfig(cfg *Config) Option {
	return func(h *Handler) {
		if cfg != nil {
			// Merge with defaults
			if cfg.ClientID != "" {
				h.config.ClientID = cfg.ClientID
			}
			if cfg.TenantID != "" {
				h.config.TenantID = cfg.TenantID
			}
			if cfg.Authority != "" {
				h.config.Authority = cfg.Authority
			}
			if len(cfg.DefaultScopes) > 0 {
				h.config.DefaultScopes = cfg.DefaultScopes
			}
			if cfg.MinPollInterval > 0 {
				h.config.MinPollInterval = cfg.MinPollInterval
			}
			if cfg.SlowDownIncrement > 0 {
				h.config.SlowDownIncrement = cfg.SlowDownIncrement
			}
		}
	}
}

// WithSecretStore sets a custom secrets store.
func WithSecretStore(store secrets.Store) Option {
	return func(h *Handler) {
		h.secretStore = store
	}
}

// WithHTTPClient sets a custom HTTP client for token requests.
func WithHTTPClient(client HTTPClient) Option {
	return func(h *Handler) {
		h.httpClient = client
	}
}

// WithHTTPClientConfig configures the handler's HTTP client from application config.
// The config is merged: global HTTPClientConfig → auth-level HTTPClient → handler-level HTTPClient.
func WithHTTPClientConfig(cfg *config.HTTPClientConfig) Option {
	return func(h *Handler) {
		h.httpClientConfig = cfg
	}
}

// WithGraphClient sets a custom client for Microsoft Graph API requests.
// Used primarily for testing.
func WithGraphClient(client GraphClient) Option {
	return func(h *Handler) {
		h.graphClient = client
	}
}

// WithLogger sets the logger for the handler.
// The logger is offset by authHTTPLogLevel before being passed to the HTTP
// transport so that auth HTTP traffic only appears at high verbosity.
func WithLogger(lgr logr.Logger) Option {
	return func(h *Handler) {
		h.logger = lgr
	}
}

// New creates a new Entra auth handler.
// Secret store initialization is deferred — if it fails, the handler is still
// created so that metadata operations (Name, SupportedFlows, etc.) work.
// Operations requiring secrets (Login, Logout, Status, GetToken) will return
// the deferred error.
func New(opts ...Option) (*Handler, error) {
	h := &Handler{
		config:   DefaultConfig(),
		oboCache: newOBOCache(),
	}

	for _, opt := range opts {
		opt(h)
	}

	// Initialize secret store if not provided
	if h.secretStore == nil {
		store, err := secrets.New()
		if err != nil {
			// Defer the error — metadata-only operations still work
			h.secretErr = fmt.Errorf("failed to initialize secrets store: %w", err)
		} else {
			h.secretStore = store
		}
	}

	// Compute the HTTP-level logger: offset by authHTTPLogLevel so that
	// auth HTTP calls only appear at high verbosity.
	httpLogger := h.logger.V(authHTTPLogLevel)

	// Initialize HTTP client if not provided
	if h.httpClient == nil {
		if h.httpClientConfig != nil {
			h.httpClient = &DefaultHTTPClient{
				client: httpc.NewClientFromAppConfig(h.httpClientConfig, httpLogger),
			}
		} else {
			h.httpClient = NewDefaultHTTPClient(httpLogger)
		}
	}

	// Initialize Graph client if not provided
	if h.graphClient == nil {
		if h.httpClientConfig != nil {
			h.graphClient = &DefaultGraphClient{
				client: httpc.NewClientFromAppConfig(h.httpClientConfig, httpLogger),
			}
		} else {
			h.graphClient = NewDefaultGraphClient(httpLogger)
		}
	}

	// Initialize token cache with secret store (nil-safe: checked before use)
	if h.secretStore != nil {
		h.tokenCache = auth.NewTokenCache(h.secretStore, SecretKeyTokenPrefix)
	}

	return h, nil
}

// ensureSecrets returns an error if the secret store is not available.
func (h *Handler) ensureSecrets() error {
	if h.secretStore == nil {
		if h.secretErr != nil {
			return h.secretErr
		}
		return fmt.Errorf("secrets store not initialized")
	}
	return nil
}

// Name returns the handler identifier.
func (h *Handler) Name() string {
	return HandlerName
}

// DisplayName returns the human-readable name.
func (h *Handler) DisplayName() string {
	return HandlerDisplayName
}

// SupportedFlows returns the authentication flows this handler supports.
// Note: FlowOnBehalfOf is not listed here because OBO is a token-exchange
// mechanism accessed via GetOBOToken, not through the Login flow.
func (h *Handler) SupportedFlows() []auth.Flow {
	flows := []auth.Flow{
		auth.FlowInteractive,
		auth.FlowDeviceCode,
		auth.FlowServicePrincipal,
		auth.FlowWorkloadIdentity,
	}
	return flows
}

// Capabilities returns the set of capabilities this handler supports.
// Entra supports scopes at both login time and per-request (different
// resource scopes), tenant ID selection, and federated tokens for
// workload identity.
func (h *Handler) Capabilities() []auth.Capability {
	return []auth.Capability{
		auth.CapScopesOnLogin,
		auth.CapScopesOnTokenRequest,
		auth.CapTenantID,
		auth.CapFederatedToken,
		auth.CapCallbackPort,
	}
}

// Login initiates the authentication flow.
// For interactive flow, this opens a browser for authorization code + PKCE.
// For device code flow, this initiates device code authentication.
// For service principal flow, this validates the credentials.
// For workload identity flow, this validates the federated token.
func (h *Handler) Login(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
	if err := h.ensureSecrets(); err != nil {
		return nil, err
	}

	// Check if workload identity flow is requested or detected (highest priority)
	if opts.Flow == auth.FlowWorkloadIdentity || (opts.Flow == "" && HasWorkloadIdentityCredentials()) {
		return h.workloadIdentityLogin(ctx, opts)
	}

	// Check if service principal flow is requested or detected
	if opts.Flow == auth.FlowServicePrincipal || (opts.Flow == "" && HasServicePrincipalCredentials()) {
		return h.servicePrincipalLogin(ctx, opts)
	}

	// Device code flow only when explicitly requested
	if opts.Flow == auth.FlowDeviceCode {
		return h.deviceCodeLogin(ctx, opts)
	}

	// Default to interactive (browser OAuth with authorization code + PKCE)
	return h.authCodeLogin(ctx, opts)
}

// Logout clears stored credentials and cached tokens.
func (h *Handler) Logout(ctx context.Context) error {
	if err := h.ensureSecrets(); err != nil {
		return err
	}

	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("logging out", "handler", HandlerName)

	// Clear all cached tokens
	if err := h.tokenCache.Clear(ctx); err != nil {
		lgr.V(1).Info("failed to clear token cache (may be empty)", "error", err)
	}

	// Delete refresh token
	if err := h.secretStore.Delete(ctx, SecretKeyRefreshToken); err != nil {
		lgr.V(1).Info("failed to delete refresh token (may not exist)", "error", err)
	}

	// Delete metadata
	if err := h.secretStore.Delete(ctx, SecretKeyMetadata); err != nil {
		lgr.V(1).Info("failed to delete metadata (may not exist)", "error", err)
	}

	return nil
}

// Status returns the current authentication status.
func (h *Handler) Status(ctx context.Context) (*auth.Status, error) {
	if err := h.ensureSecrets(); err != nil {
		return nil, err
	}

	// Check for workload identity credentials first (highest priority)
	if HasWorkloadIdentityCredentials() {
		return h.workloadIdentityStatus(ctx)
	}

	// Check for service principal credentials
	if HasServicePrincipalCredentials() {
		return h.servicePrincipalStatus(ctx)
	}

	// Check if we have stored credentials
	exists, err := h.secretStore.Exists(ctx, SecretKeyRefreshToken)
	if err != nil {
		return nil, fmt.Errorf("failed to check credentials: %w", err)
	}

	if !exists {
		return &auth.Status{Authenticated: false}, nil
	}

	// Load and validate metadata
	metadata, err := h.loadMetadata(ctx)
	if err != nil {
		// Corrupted or missing metadata, consider not authenticated.
		// We intentionally don't return the error because this is a recoverable state -
		// the user just needs to re-authenticate.
		return &auth.Status{Authenticated: false}, nil //nolint:nilerr // Intentional: treat corrupted metadata as not authenticated
	}

	// Check if refresh token is expired
	if !metadata.RefreshTokenExpiresAt.IsZero() && time.Now().After(metadata.RefreshTokenExpiresAt) {
		return &auth.Status{
			Authenticated: false,
			Reason:        "session expired",
			Claims:        metadata.Claims,
		}, nil
	}

	return &auth.Status{
		Authenticated: true,
		Claims:        metadata.Claims,
		ExpiresAt:     metadata.RefreshTokenExpiresAt,
		LastRefresh:   metadata.LastRefresh,
		TenantID:      metadata.TenantID,
		IdentityType:  auth.IdentityTypeUser,
		ClientID:      metadata.ClientID,
		Scopes:        metadata.Scopes,
	}, nil
}

// GetToken returns a valid access token for the specified options.
func (h *Handler) GetToken(ctx context.Context, opts auth.TokenOptions) (*auth.Token, error) {
	if err := h.ensureSecrets(); err != nil {
		return nil, err
	}

	// Use workload identity flow if credentials are present (highest priority)
	if HasWorkloadIdentityCredentials() {
		return h.getWorkloadIdentityToken(ctx, opts)
	}

	// Use service principal flow if credentials are present
	if HasServicePrincipalCredentials() {
		return h.getServicePrincipalToken(ctx, opts)
	}

	if opts.Scope == "" {
		return nil, auth.ErrInvalidScope
	}

	// Qualify bare permission names (e.g. "Group.Read.All" → full Graph URI).
	qualifiedScope := QualifyScope(opts.Scope)

	lgr := logger.FromContext(ctx)

	// Determine the flow from stored metadata so we can partition the cache.
	var userFlow auth.Flow
	if metadata, err := h.loadMetadata(ctx); err == nil && metadata != nil {
		userFlow = metadata.LoginFlow
	}

	// Determine minimum validity duration
	minValidFor := opts.MinValidFor
	if minValidFor == 0 {
		minValidFor = auth.DefaultMinValidFor
	}

	lgr.V(1).Info("getting token",
		"handler", HandlerName,
		"scope", qualifiedScope,
		"flow", userFlow,
		"minValidFor", minValidFor,
		"forceRefresh", opts.ForceRefresh,
	)

	// Compute identity fingerprint for cache partitioning.
	fingerprint := auth.FingerprintHash(h.config.ClientID + ":" + h.config.TenantID)

	// Check disk cache first (unless force refresh)
	if !opts.ForceRefresh {
		token, err := h.tokenCache.Get(ctx, userFlow, fingerprint, qualifiedScope)
		if err == nil && token != nil && token.IsValidFor(minValidFor) {
			lgr.V(1).Info("using cached token",
				"scope", qualifiedScope,
				"expiresAt", token.ExpiresAt,
				"remainingValidity", token.TimeUntilExpiry(),
			)
			return token, nil
		}
		if err != nil {
			lgr.V(1).Info("cache lookup failed, will mint new token", "error", err)
		} else if token != nil {
			lgr.V(1).Info("cached token insufficient validity",
				"expiresAt", token.ExpiresAt,
				"remainingValidity", token.TimeUntilExpiry(),
				"requiredValidity", minValidFor,
			)
		}
	}

	// Mint new token
	token, err := h.mintToken(ctx, qualifiedScope)
	if err != nil {
		return nil, err
	}

	// Cache the token to disk
	if err := h.tokenCache.Set(ctx, userFlow, fingerprint, qualifiedScope, token); err != nil {
		lgr.V(1).Info("failed to cache token", "error", err)
		// Continue anyway - we have the token
	}

	return token, nil
}

// InjectAuth adds authentication to an HTTP request.
func (h *Handler) InjectAuth(ctx context.Context, req *http.Request, opts auth.TokenOptions) error {
	token, err := h.GetToken(ctx, opts)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", fmt.Sprintf("%s %s", token.TokenType, token.AccessToken))
	return nil
}

// Compile-time check that Handler implements auth.Handler, auth.TokenLister, and auth.TokenPurger.
var (
	_ auth.Handler     = (*Handler)(nil)
	_ auth.TokenLister = (*Handler)(nil)
	_ auth.TokenPurger = (*Handler)(nil)
)

// ListCachedTokens returns metadata for all tokens stored by the Entra handler.
// It includes the long-lived refresh token (if any) and all minted access tokens
// from the on-disk cache.  Actual token values are intentionally excluded.
func (h *Handler) ListCachedTokens(ctx context.Context) ([]*auth.CachedTokenInfo, error) {
	if err := h.ensureSecrets(); err != nil {
		return nil, err
	}

	var results []*auth.CachedTokenInfo

	// Refresh token
	exists, _ := h.secretStore.Exists(ctx, SecretKeyRefreshToken)
	if exists {
		info := &auth.CachedTokenInfo{
			Handler:   HandlerName,
			TokenKind: "refresh",
		}
		if metadata, err := h.loadMetadata(ctx); err == nil && metadata != nil {
			info.ExpiresAt = metadata.RefreshTokenExpiresAt
			info.CachedAt = metadata.LastRefresh
			info.Flow = metadata.LoginFlow
			info.SessionID = metadata.SessionID
		}
		if !info.ExpiresAt.IsZero() {
			info.IsExpired = time.Now().After(info.ExpiresAt)
		}
		results = append(results, info)
	}

	// Minted access tokens
	if h.tokenCache != nil {
		entries, _ := h.tokenCache.ListCachedEntries(ctx)
		for _, entry := range entries {
			token, err := h.tokenCache.Get(ctx, entry.Flow, entry.Fingerprint, entry.Scope)
			if err != nil || token == nil {
				continue
			}
			results = append(results, &auth.CachedTokenInfo{
				Handler:   HandlerName,
				TokenKind: "access",
				Scope:     entry.Scope,
				TokenType: token.TokenType,
				Flow:      token.Flow,
				ExpiresAt: token.ExpiresAt,
				CachedAt:  token.CachedAt,
				IsExpired: token.IsExpired(),
				SessionID: token.SessionID,
			})
		}
	}

	return results, nil
}

// PurgeExpiredTokens removes expired access tokens from the on-disk cache.
// The refresh token and valid access tokens are left untouched.
// Returns the number of tokens removed.
func (h *Handler) PurgeExpiredTokens(ctx context.Context) (int, error) {
	if err := h.ensureSecrets(); err != nil {
		return 0, err
	}
	if h.tokenCache == nil {
		return 0, nil
	}
	return h.tokenCache.PurgeExpired(ctx)
}
