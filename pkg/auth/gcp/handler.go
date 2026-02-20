// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gcp

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
	// HandlerName is the unique identifier for the GCP auth handler.
	HandlerName = "gcp"

	// HandlerDisplayName is the human-readable name.
	HandlerDisplayName = "Google Cloud Platform"

	// SecretKeyRefreshToken is the secret name for storing the refresh token.
	SecretKeyRefreshToken = "scafctl.auth.gcp.refresh_token" //nolint:gosec // This is a key name, not a credential

	// SecretKeyMetadata is the secret name for storing token metadata.
	SecretKeyMetadata = "scafctl.auth.gcp.metadata" //nolint:gosec // This is a key name, not a credential

	// SecretKeyTokenPrefix is the prefix for cached access tokens.
	SecretKeyTokenPrefix = "scafctl.auth.gcp.token." //nolint:gosec // This is a key prefix, not a credential

	// DefaultTimeout is the default timeout for browser OAuth flow.
	DefaultTimeout = 5 * time.Minute
)

// Handler implements auth.Handler for Google Cloud Platform.
type Handler struct {
	config      *Config
	secretStore secrets.Store
	secretErr   error // deferred error from secrets initialization
	httpClient  HTTPClient
	tokenCache  *TokenCache
}

// Option configures the Handler.
type Option func(*Handler)

// WithConfig sets the GCP configuration.
func WithConfig(cfg *Config) Option {
	return func(h *Handler) {
		if cfg != nil {
			if cfg.ClientID != "" {
				h.config.ClientID = cfg.ClientID
			}
			if cfg.ClientSecret != "" {
				h.config.ClientSecret = cfg.ClientSecret
			}
			if len(cfg.DefaultScopes) > 0 {
				h.config.DefaultScopes = cfg.DefaultScopes
			}
			if cfg.ImpersonateServiceAccount != "" {
				h.config.ImpersonateServiceAccount = cfg.ImpersonateServiceAccount
			}
			if cfg.Project != "" {
				h.config.Project = cfg.Project
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

// New creates a new GCP auth handler.
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
			h.secretErr = fmt.Errorf("failed to initialize secrets store: %w", err)
		} else {
			h.secretStore = store
		}
	}

	// Initialize HTTP client if not provided
	if h.httpClient == nil {
		h.httpClient = NewDefaultHTTPClient()
	}

	// Initialize token cache with secret store
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
		auth.FlowInteractive,
		auth.FlowServicePrincipal,
		auth.FlowWorkloadIdentity,
		auth.FlowMetadata,
		auth.FlowGcloudADC,
	}
}

// Capabilities returns the set of capabilities this handler supports.
func (h *Handler) Capabilities() []auth.Capability {
	return []auth.Capability{
		auth.CapScopesOnLogin,
		auth.CapScopesOnTokenRequest,
		auth.CapFederatedToken,
	}
}

// Login initiates the authentication flow.
func (h *Handler) Login(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
	if err := h.ensureSecrets(); err != nil {
		return nil, err
	}

	// Check if workload identity flow is requested or detected (highest priority)
	if opts.Flow == auth.FlowWorkloadIdentity || (opts.Flow == "" && HasWorkloadIdentityCredentials()) {
		return h.workloadIdentityLogin(ctx, opts)
	}

	// Check if metadata server flow is requested or detected
	if opts.Flow == auth.FlowMetadata || (opts.Flow == "" && IsMetadataServerAvailable(ctx, h.httpClient)) {
		return h.metadataLogin(ctx, opts)
	}

	// Check if service account flow is requested or detected
	if opts.Flow == auth.FlowServicePrincipal || (opts.Flow == "" && HasServiceAccountCredentials()) {
		return h.serviceAccountLogin(ctx, opts)
	}

	// Check if gcloud ADC flow is explicitly requested
	if opts.Flow == auth.FlowGcloudADC {
		return h.gcloudADCLogin(ctx, opts)
	}

	// Default to ADC (native browser OAuth)
	return h.adcLogin(ctx, opts)
}

// Logout clears stored credentials and cached tokens.
func (h *Handler) Logout(ctx context.Context) error {
	if err := h.ensureSecrets(); err != nil {
		return err
	}

	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("logging out", "handler", HandlerName)

	// Revoke refresh token with Google (ADC flow only)
	if err := h.revokeRefreshToken(ctx); err != nil {
		lgr.V(1).Info("failed to revoke refresh token (may not exist)", "error", err)
	}

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

	// Check for metadata server
	// Note: We don't auto-probe metadata server for status to avoid network calls
	// on every status check. Users who logged in with metadata flow will have stored metadata.

	// Check for service account credentials
	if HasServiceAccountCredentials() {
		return h.serviceAccountStatus(ctx)
	}

	// Check for stored credentials (ADC flow)
	exists, err := h.secretStore.Exists(ctx, SecretKeyMetadata)
	if err != nil {
		return nil, fmt.Errorf("failed to check credentials: %w", err)
	}

	if !exists {
		// Check for gcloud ADC credentials as fallback
		if HasGcloudADCCredentials() {
			return &auth.Status{
				Authenticated: true,
				Claims: &auth.Claims{
					Issuer: "https://accounts.google.com",
					Name:   "gcloud ADC (application default credentials)",
				},
				IdentityType: auth.IdentityTypeUser,
			}, nil
		}
		return &auth.Status{Authenticated: false}, nil
	}

	// Load metadata
	metadata, err := h.loadMetadata(ctx)
	if err != nil {
		return &auth.Status{Authenticated: false}, nil //nolint:nilerr // treat corrupted metadata as not authenticated
	}

	status := &auth.Status{
		Authenticated: true,
		Claims:        metadata.Claims,
		IdentityType:  auth.IdentityTypeUser,
		Scopes:        metadata.Scopes,
	}

	switch metadata.Flow { //nolint:exhaustive // only SA and WI need override; all others use default IdentityTypeUser
	case auth.FlowServicePrincipal:
		status.IdentityType = auth.IdentityTypeServicePrincipal
	case auth.FlowWorkloadIdentity:
		status.IdentityType = auth.IdentityTypeWorkloadIdentity
	}

	if !metadata.RefreshTokenExpiresAt.IsZero() {
		status.ExpiresAt = metadata.RefreshTokenExpiresAt
	}

	if metadata.ImpersonateServiceAccount != "" {
		status.ClientID = metadata.ImpersonateServiceAccount
	}

	return status, nil
}

// GetToken returns a valid access token for the specified options.
func (h *Handler) GetToken(ctx context.Context, opts auth.TokenOptions) (*auth.Token, error) {
	if err := h.ensureSecrets(); err != nil {
		return nil, err
	}

	// Determine source token function based on priority
	sourceTokenFunc := h.resolveSourceTokenFunc(ctx)

	// If impersonation is configured, wrap with impersonation
	if h.config.ImpersonateServiceAccount != "" {
		return h.getImpersonatedToken(ctx, opts, sourceTokenFunc)
	}

	return sourceTokenFunc(ctx, opts)
}

// resolveSourceTokenFunc returns the appropriate token acquisition function
// based on available credentials, following the auto-detection priority:
// WI > Metadata > SA Key > ADC (stored) > gcloud ADC.
func (h *Handler) resolveSourceTokenFunc(_ context.Context) func(context.Context, auth.TokenOptions) (*auth.Token, error) {
	// Workload identity (highest priority)
	if HasWorkloadIdentityCredentials() {
		return h.getWorkloadIdentityToken
	}

	// Metadata server — check stored metadata to see if we logged in via metadata
	// (don't probe the network on every token request)
	metadata, err := h.loadMetadata(context.Background())
	if err == nil && metadata != nil && metadata.Flow == auth.FlowMetadata {
		return h.getMetadataToken
	}

	// Service account key
	if HasServiceAccountCredentials() {
		return h.getServiceAccountToken
	}

	// Check for stored refresh token (ADC browser flow)
	exists, _ := h.secretStore.Exists(context.Background(), SecretKeyRefreshToken)
	if exists {
		return h.getStoredRefreshToken
	}

	// Fallback to gcloud ADC
	if HasGcloudADCCredentials() {
		return h.getGcloudADCToken
	}

	// No credentials available
	return func(_ context.Context, _ auth.TokenOptions) (*auth.Token, error) {
		return nil, auth.ErrNotAuthenticated
	}
}

// getStoredRefreshToken gets a token using the stored refresh token, with caching.
func (h *Handler) getStoredRefreshToken(ctx context.Context, opts auth.TokenOptions) (*auth.Token, error) {
	if opts.Scope == "" {
		return nil, auth.ErrInvalidScope
	}

	lgr := logger.FromContext(ctx)

	minValidFor := opts.MinValidFor
	if minValidFor == 0 {
		minValidFor = auth.DefaultMinValidFor
	}

	// Check cache first
	if !opts.ForceRefresh {
		cached, err := h.tokenCache.Get(ctx, opts.Scope)
		if err == nil && cached != nil && cached.IsValidFor(minValidFor) {
			lgr.V(1).Info("using cached token", "scope", opts.Scope)
			return cached, nil
		}
	}

	// Mint new token via refresh
	token, err := h.mintToken(ctx, opts.Scope)
	if err != nil {
		return nil, err
	}

	// Cache the token
	if err := h.tokenCache.Set(ctx, opts.Scope, token); err != nil {
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
	getCreds func() (T, error),
	isCredsNil func(T, error) bool,
	acquireToken tokenAcquireFunc[T],
	logPrefix string,
) (*auth.Token, error) {
	if opts.Scope == "" {
		opts.Scope = "https://www.googleapis.com/auth/cloud-platform"
	}

	lgr := logger.FromContext(ctx)

	creds, err := getCreds()
	if isCredsNil(creds, err) {
		return nil, auth.ErrNotAuthenticated
	}

	minValidFor := opts.MinValidFor
	if minValidFor == 0 {
		minValidFor = auth.DefaultMinValidFor
	}

	// Check cache first (unless ForceRefresh)
	if !opts.ForceRefresh {
		cached, cacheErr := h.tokenCache.Get(ctx, opts.Scope)
		if cacheErr == nil && cached != nil && cached.IsValidFor(minValidFor) {
			lgr.V(1).Info("using cached "+logPrefix+" token", "scope", opts.Scope)
			return cached, nil
		}
	}

	// Acquire new token
	token, err := acquireToken(ctx, creds, opts.Scope)
	if err != nil {
		return nil, err
	}

	// Cache the token
	if cacheErr := h.tokenCache.Set(ctx, opts.Scope, token); cacheErr != nil {
		lgr.V(1).Info("failed to cache "+logPrefix+" token", "error", cacheErr)
	}

	return token, nil
}

// Compile-time check that Handler implements auth.Handler.
var _ auth.Handler = (*Handler)(nil)
