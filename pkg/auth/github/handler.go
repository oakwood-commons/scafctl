// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package github

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
	// HandlerName is the unique identifier for the GitHub auth handler.
	HandlerName = "github"

	// HandlerDisplayName is the human-readable name for the handler.
	HandlerDisplayName = "GitHub"

	// SecretKeyRefreshToken is the secret key for storing the refresh token.
	SecretKeyRefreshToken = "scafctl.auth.github.refresh_token" //nolint:gosec // This is a key name, not a credential

	// SecretKeyAccessToken is the secret key for storing the access token.
	SecretKeyAccessToken = "scafctl.auth.github.access_token" //nolint:gosec // This is a key name, not a credential

	// SecretKeyMetadata is the secret key for storing token metadata.
	SecretKeyMetadata = "scafctl.auth.github.metadata" //nolint:gosec // This is a key name, not a credential

	// SecretKeyTokenPrefix is the prefix for cached access tokens.
	// Full key format: scafctl.auth.github.token.<base64url-encoded-scope>
	SecretKeyTokenPrefix = "scafctl.auth.github.token." //nolint:gosec // This is a key prefix, not a credential

	// DefaultTimeout is the default timeout for device code flow.
	DefaultTimeout = 5 * time.Minute

	// DefaultMinPollInterval is the minimum polling interval for device code flow.
	DefaultMinPollInterval = 5 * time.Second
)

// authHTTPLogLevel is the verbosity offset applied to the base logger
// before it is handed to the HTTP transport used by auth handlers.
const authHTTPLogLevel = 5

// Handler implements auth.Handler for GitHub.
type Handler struct {
	config           *Config
	secretStore      secrets.Store
	secretErr        error // deferred error from secrets initialization
	httpClient       HTTPClient
	httpClientConfig *config.HTTPClientConfig
	tokenCache       *auth.TokenCache
	logger           logr.Logger
}

// Option configures the Handler.
type Option func(*Handler)

// WithConfig sets the GitHub configuration.
func WithConfig(cfg *Config) Option {
	return func(h *Handler) {
		if cfg != nil {
			if cfg.ClientID != "" {
				h.config.ClientID = cfg.ClientID
			}
			if cfg.ClientSecret != "" {
				h.config.ClientSecret = cfg.ClientSecret
			}
			if cfg.Hostname != "" {
				h.config.Hostname = cfg.Hostname
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
			if cfg.AppID != 0 {
				h.config.AppID = cfg.AppID
			}
			if cfg.InstallationID != 0 {
				h.config.InstallationID = cfg.InstallationID
			}
			if cfg.PrivateKey != "" {
				h.config.PrivateKey = cfg.PrivateKey
			}
			if cfg.PrivateKeyPath != "" {
				h.config.PrivateKeyPath = cfg.PrivateKeyPath
			}
			if cfg.PrivateKeySecretName != "" {
				h.config.PrivateKeySecretName = cfg.PrivateKeySecretName
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

// WithHTTPClient sets a custom HTTP client for API requests.
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

// WithLogger sets the logger for the handler.
// The logger is offset by authHTTPLogLevel before being passed to the HTTP
// transport so that auth HTTP traffic only appears at high verbosity.
func WithLogger(lgr logr.Logger) Option {
	return func(h *Handler) {
		h.logger = lgr
	}
}

// New creates a new GitHub auth handler.
// Secret store initialization is deferred — if it fails, the handler is still
// created so that metadata operations (Name, SupportedFlows, etc.) work.
// Operations requiring secrets (Login, Logout, Status, GetToken) will return
// the deferred error.
func New(opts ...Option) (*Handler, error) {
	h := &Handler{
		config: DefaultConfig(),
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
func (h *Handler) SupportedFlows() []auth.Flow {
	return []auth.Flow{
		auth.FlowInteractive,
		auth.FlowDeviceCode,
		auth.FlowPAT,
		auth.FlowGitHubApp,
	}
}

// Capabilities returns the set of capabilities this handler supports.
// GitHub supports scopes at login time (device code / interactive flows) and hostname
// for GHES, but does NOT support per-request scopes (scopes are fixed
// at login time and cannot be changed on token refresh).
// CallbackPort is supported for the interactive (browser/PKCE) flow.
func (h *Handler) Capabilities() []auth.Capability {
	return []auth.Capability{
		auth.CapScopesOnLogin,
		auth.CapHostname,
		auth.CapCallbackPort,
	}
}

// Login initiates the authentication flow.
// Default interactive flow behaviour:
//   - With client_secret configured → OAuth authorization code + PKCE (browser redirect)
//   - Without client_secret → device code flow with browser auto-open (same as 'gh auth login')
//
// Use --flow device-code to force the headless code-prompt (no browser).
// Use --flow github-app for service-to-service installation tokens.
func (h *Handler) Login(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
	if err := h.ensureSecrets(); err != nil {
		return nil, err
	}

	// Check if PAT flow is requested or detected.
	// Skip PAT auto-detection when scopes are explicitly provided, since
	// PAT scopes are fixed at creation time and can't be changed at login.
	if opts.Flow == auth.FlowPAT || (opts.Flow == "" && HasPATCredentials() && len(opts.Scopes) == 0) {
		return h.patLogin(ctx, opts)
	}

	switch opts.Flow { //nolint:exhaustive // Only GitHub-supported flows are handled; others fall through to default
	case auth.FlowDeviceCode:
		// Explicit device-code: show code, do not open browser
		return h.deviceCodeLogin(ctx, opts)
	case auth.FlowGitHubApp:
		return h.appLogin(ctx, opts)
	case auth.FlowInteractive, "":
		// Interactive default: use browser auth code + PKCE when a client_secret
		// is configured. Without a secret, GitHub OAuth Apps reject the exchange,
		// so fall back to device code with browser auto-open (matching 'gh auth login').
		if h.config.ClientSecret != "" {
			return h.authCodeLogin(ctx, opts)
		}
		return h.interactiveDeviceCodeLogin(ctx, opts)
	default:
		return nil, fmt.Errorf("%w: %s", auth.ErrFlowNotSupported, opts.Flow)
	}
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

	// Delete access token
	if err := h.secretStore.Delete(ctx, SecretKeyAccessToken); err != nil {
		lgr.V(1).Info("failed to delete access token (may not exist)", "error", err)
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

	// Check for PAT credentials first (highest priority)
	if HasPATCredentials() {
		return h.patStatus(ctx)
	}

	// Check if we have stored credentials (refresh token or access token)
	hasRefresh, err := h.secretStore.Exists(ctx, SecretKeyRefreshToken)
	if err != nil {
		return nil, fmt.Errorf("failed to check credentials: %w", err)
	}

	hasAccess, err := h.secretStore.Exists(ctx, SecretKeyAccessToken)
	if err != nil {
		return nil, fmt.Errorf("failed to check credentials: %w", err)
	}

	if !hasRefresh && !hasAccess {
		return &auth.Status{Authenticated: false}, nil
	}

	// Load and validate metadata
	metadata, err := h.loadMetadata(ctx)
	if err != nil {
		return &auth.Status{Authenticated: false}, nil //nolint:nilerr // Treat corrupted metadata as not authenticated
	}

	// Check if refresh token is expired (if applicable)
	if !metadata.RefreshTokenExpiresAt.IsZero() && time.Now().After(metadata.RefreshTokenExpiresAt) {
		return &auth.Status{
			Authenticated: false,
			Claims:        metadata.Claims,
		}, nil
	}

	return &auth.Status{
		Authenticated: true,
		Claims:        metadata.Claims,
		ExpiresAt:     metadata.RefreshTokenExpiresAt,
		LastRefresh:   metadata.LastRefresh,
		IdentityType:  auth.IdentityTypeUser,
		ClientID:      metadata.ClientID,
		Scopes:        metadata.Scopes,
	}, nil
}

// defaultCacheKey is the fixed cache key for GitHub tokens.
// GitHub scopes are fixed at login time and cannot be changed per-request,
// so every token request uses the same cache entry.
const defaultCacheKey = "_github"

// GetToken returns a valid access token for the specified options.
// Unlike Entra, GitHub does not support per-request scopes — the scope field
// in opts is ignored. Scopes are fixed at login time.
func (h *Handler) GetToken(ctx context.Context, opts auth.TokenOptions) (*auth.Token, error) {
	if err := h.ensureSecrets(); err != nil {
		return nil, err
	}

	lgr := logger.FromContext(ctx)

	// Use PAT flow if credentials are present (highest priority)
	if HasPATCredentials() {
		return h.getPATToken(ctx, opts)
	}

	// Determine minimum validity duration
	minValidFor := opts.MinValidFor
	if minValidFor == 0 {
		minValidFor = auth.DefaultMinValidFor
	}

	lgr.V(1).Info("getting token",
		"handler", HandlerName,
		"minValidFor", minValidFor,
		"forceRefresh", opts.ForceRefresh,
	)

	fingerprint := auth.FingerprintHash(h.config.Hostname)

	// Check disk cache first (unless force refresh)
	if !opts.ForceRefresh {
		token, err := h.tokenCache.Get(ctx, auth.FlowDeviceCode, fingerprint, defaultCacheKey)
		if err == nil && token != nil && token.IsValidFor(minValidFor) {
			lgr.V(1).Info("using cached token",
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

	// Check if we have a stored access token (non-expiring OAuth App)
	accessToken, err := h.loadAccessToken(ctx)
	if err == nil && accessToken != "" {
		// For non-expiring tokens, just use the stored access token
		token := &auth.Token{
			AccessToken: accessToken,
			TokenType:   "Bearer",
			ExpiresAt:   farFuture(),
		}
		if cacheErr := h.tokenCache.Set(ctx, auth.FlowDeviceCode, fingerprint, defaultCacheKey, token); cacheErr != nil {
			lgr.V(1).Info("failed to cache token", "error", cacheErr)
		}
		return token, nil
	}

	// Try to mint new token using refresh token
	token, err := h.mintToken(ctx, defaultCacheKey)
	if err != nil {
		return nil, err
	}

	// Cache the token to disk
	if err := h.tokenCache.Set(ctx, auth.FlowDeviceCode, fingerprint, defaultCacheKey, token); err != nil {
		lgr.V(1).Info("failed to cache token", "error", err)
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

// farFuture returns a time far in the future for tokens with no defined expiry.
func farFuture() time.Time {
	return time.Now().Add(365 * 24 * time.Hour)
}

// Compile-time check that Handler implements auth.Handler, auth.TokenLister, and auth.TokenPurger.
var (
	_ auth.Handler     = (*Handler)(nil)
	_ auth.TokenLister = (*Handler)(nil)
	_ auth.TokenPurger = (*Handler)(nil)
)

// ListCachedTokens returns metadata for all tokens stored by the GitHub handler.
// It includes the OAuth refresh token (device code flow), any directly stored
// access token (PAT or non-expiring OAuth App), and all minted access tokens
// from the on-disk cache.  Actual token values are intentionally excluded.
func (h *Handler) ListCachedTokens(ctx context.Context) ([]*auth.CachedTokenInfo, error) {
	if err := h.ensureSecrets(); err != nil {
		return nil, err
	}

	var results []*auth.CachedTokenInfo

	// Refresh token (device code flow with token expiry enabled)
	hasRefresh, _ := h.secretStore.Exists(ctx, SecretKeyRefreshToken)
	if hasRefresh {
		info := &auth.CachedTokenInfo{
			Handler:   HandlerName,
			TokenKind: "refresh",
			Flow:      auth.FlowDeviceCode,
		}
		if metadata, err := h.loadMetadata(ctx); err == nil && metadata != nil {
			info.ExpiresAt = metadata.RefreshTokenExpiresAt
			info.CachedAt = metadata.LastRefresh
			info.SessionID = metadata.SessionID
		}
		if !info.ExpiresAt.IsZero() {
			info.IsExpired = time.Now().After(info.ExpiresAt)
		}
		results = append(results, info)
	}

	// Minted access tokens (short-lived and direct-stored)
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
				TokenType: token.TokenType,
				Flow:      token.Flow,
				ExpiresAt: token.ExpiresAt,
				CachedAt:  token.CachedAt,
				IsExpired: token.IsExpired(),
				SessionID: token.SessionID,
			})
		}
	}

	// If no tokenCache entry for the default key but a direct access token exists
	// (e.g. the user just logged in with PAT and has not yet called GetToken),
	// show a basic entry so the token is visible.
	if h.tokenCache != nil {
		hasAccess, _ := h.secretStore.Exists(ctx, SecretKeyAccessToken)
		if hasAccess {
			cached, cacheErr := h.tokenCache.Get(ctx, auth.FlowDeviceCode, auth.FingerprintHash(h.config.Hostname), defaultCacheKey)
			if cacheErr != nil || cached == nil {
				info := &auth.CachedTokenInfo{
					Handler:   HandlerName,
					TokenKind: "access",
					TokenType: "Bearer",
				}
				if metadata, err := h.loadMetadata(ctx); err == nil && metadata != nil {
					info.CachedAt = metadata.LastRefresh
					info.SessionID = metadata.SessionID
				}
				results = append(results, info)
			}
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
