// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
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

// Handler implements auth.Handler for GitHub.
type Handler struct {
	config      *Config
	secretStore secrets.Store
	secretErr   error // deferred error from secrets initialization
	httpClient  HTTPClient
	tokenCache  *TokenCache
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

	// Initialize HTTP client if not provided
	if h.httpClient == nil {
		h.httpClient = NewDefaultHTTPClient()
	}

	// Initialize token cache with secret store (nil-safe: checked before use)
	if h.secretStore != nil {
		h.tokenCache = NewTokenCache(h.secretStore)
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
		auth.FlowDeviceCode,
		auth.FlowPAT,
	}
}

// Capabilities returns the set of capabilities this handler supports.
// GitHub supports scopes at login time (device code flow) and hostname
// for GHES, but does NOT support per-request scopes (scopes are fixed
// at login time and cannot be changed on token refresh).
func (h *Handler) Capabilities() []auth.Capability {
	return []auth.Capability{
		auth.CapScopesOnLogin,
		auth.CapHostname,
	}
}

// Login initiates the authentication flow.
// For device code flow, this initiates interactive authentication.
// For PAT flow, this validates the token from environment.
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

	return h.deviceCodeLogin(ctx, opts)
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

	// Check disk cache first (unless force refresh)
	if !opts.ForceRefresh {
		token, err := h.tokenCache.Get(ctx, defaultCacheKey)
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
		if cacheErr := h.tokenCache.Set(ctx, defaultCacheKey, token); cacheErr != nil {
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
	if err := h.tokenCache.Set(ctx, defaultCacheKey, token); err != nil {
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

// tokenAcquireFunc is a function that acquires a token given credentials and scope.
type tokenAcquireFunc[T any] func(ctx context.Context, creds T, scope string) (*auth.Token, error)

// getCachedOrAcquireToken is a generic helper that handles the common pattern of:
// 1. Checking if credentials exist
// 2. Checking the cache (unless ForceRefresh)
// 3. Acquiring a new token if needed
// 4. Caching the new token
func getCachedOrAcquireToken[T any](
	ctx context.Context,
	h *Handler,
	opts auth.TokenOptions,
	getCreds func() T,
	isCredsNil func(T) bool,
	acquireToken tokenAcquireFunc[T],
	logPrefix string,
) (*auth.Token, error) {
	lgr := logger.FromContext(ctx)
	creds := getCreds()

	if isCredsNil(creds) {
		return nil, auth.ErrNotAuthenticated
	}

	// Use fixed cache key — GitHub scopes are determined at login, not per-request.
	cacheKey := defaultCacheKey

	// Apply default minimum validity to match the user-flow behavior.
	minValidFor := opts.MinValidFor
	if minValidFor == 0 {
		minValidFor = auth.DefaultMinValidFor
	}

	// Check cache first (unless ForceRefresh)
	if !opts.ForceRefresh {
		cached, err := h.tokenCache.Get(ctx, cacheKey)
		if err == nil && cached != nil && cached.IsValidFor(minValidFor) {
			lgr.V(1).Info("using cached "+logPrefix+" token", "cacheKey", cacheKey)
			return cached, nil
		}
	}

	// Acquire new token
	token, err := acquireToken(ctx, creds, cacheKey)
	if err != nil {
		return nil, err
	}

	// Cache the token
	if err := h.tokenCache.Set(ctx, cacheKey, token); err != nil {
		lgr.V(1).Info("failed to cache "+logPrefix+" token", "error", err)
	}

	return token, nil
}

// farFuture returns a time far in the future for tokens with no defined expiry.
func farFuture() time.Time {
	return time.Now().Add(365 * 24 * time.Hour)
}

// Compile-time check that Handler implements auth.Handler.
var _ auth.Handler = (*Handler)(nil)
