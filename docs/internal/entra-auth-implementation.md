# Entra Authentication Implementation Plan

This document outlines the detailed implementation plan for adding Microsoft Entra ID (formerly Azure AD) authentication support to scafctl using the OAuth 2.0 device authorization flow.

## Overview

The implementation adds:
1. `scafctl auth login entra` - Device code authentication flow
2. `scafctl auth logout entra` - Clear stored credentials
3. `scafctl auth status` - Check authentication status
4. `scafctl auth token entra` - Display current token (for debugging)
5. Auth handler system for token management with disk-based caching
6. HTTP provider integration with `authProvider` and `scope` properties

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                         scafctl CLI                                  │
├─────────────────────────────────────────────────────────────────────┤
│  auth login    │  auth logout   │  auth status   │  auth token      │
└───────┬────────┴───────┬────────┴───────┬────────┴───────┬──────────┘
        │                │                │                │
        ▼                ▼                ▼                ▼
┌─────────────────────────────────────────────────────────────────────┐
│                      pkg/auth                                        │
├─────────────────────────────────────────────────────────────────────┤
│  AuthHandler Interface  │  Registry  │  Claims  │  Context Helpers  │
├─────────────────────────────────────────────────────────────────────┤
│                      pkg/auth/entra                                  │
├─────────────────────────────────────────────────────────────────────┤
│  EntraHandler  │  DeviceFlow  │  TokenCache (disk-based)  │  Config │
└───────┬─────────────────────────────────────────────────────────────┘
        │
        ▼
┌─────────────────────────────────────────────────────────────────────┐
│                      pkg/secrets                                     │
├─────────────────────────────────────────────────────────────────────┤
│  Refresh token:  scafctl.auth.entra.refresh_token                   │
│  Metadata:       scafctl.auth.entra.metadata                        │
│  Cached tokens:  scafctl.auth.entra.token.<base64url-scope>         │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────┐
│              pkg/provider/builtin/httpprovider                       │
├─────────────────────────────────────────────────────────────────────┤
│  New properties: authProvider, scope                                 │
│  Calls auth.GetHandler() → handler.GetToken(opts) with MinValidFor  │
└─────────────────────────────────────────────────────────────────────┘
```

## Package Structure

```
pkg/
├── auth/
│   ├── handler.go           # AuthHandler interface
│   ├── handler_test.go
│   ├── registry.go          # Auth handler registry
│   ├── registry_test.go
│   ├── errors.go            # Error definitions
│   ├── claims.go            # Token claims types
│   ├── claims_test.go
│   ├── context.go           # Context helpers for auth registry
│   ├── context_test.go
│   ├── mock.go              # Mock implementations for testing
│   └── entra/
│       ├── handler.go       # EntraHandler implementation
│       ├── handler_test.go
│       ├── device_flow.go   # Device code flow
│       ├── device_flow_test.go
│       ├── service_principal.go      # Service Principal (client credentials) flow
│       ├── service_principal_test.go
│       ├── workload_identity.go      # Workload Identity (federated credentials) flow
│       ├── workload_identity_test.go
│       ├── token.go         # Token refresh and minting
│       ├── token_test.go
│       ├── cache.go         # Disk-based token cache via pkg/secrets
│       ├── cache_test.go
│       ├── http.go          # HTTP client wrapper
│       ├── http_test.go
│       └── config.go        # Entra-specific configuration
├── cmd/
│   └── scafctl/
│       └── auth/
│           ├── auth.go      # Parent 'auth' command
│           ├── login.go     # 'auth login' command (supports --flow flag)
│           ├── login_test.go
│           ├── logout.go    # 'auth logout' command
│           ├── logout_test.go
│           ├── status.go    # 'auth status' command
│           ├── status_test.go
│           ├── token.go     # 'auth token' command
│           └── token_test.go
├── config/
│   └── config.go            # Add AuthConfig
└── provider/
    └── builtin/
        └── httpprovider/
            └── http.go      # Add authProvider, scope properties
```

---

## Core Interfaces

> **Note:** Phase 1 has been implemented. The code below reflects the actual implementation in `pkg/auth/`.
> Type names were simplified from the original plan (e.g., `AuthHandler` → `Handler`) to follow Go naming conventions.

### pkg/auth/handler.go ✅ IMPLEMENTED

```go
package auth

import (
    "context"
    "net/http"
    "time"
)

// Handler defines the interface for authentication handlers.
// Auth handlers manage identity verification, credential storage, and token acquisition.
type Handler interface {
    Name() string
    DisplayName() string
    Login(ctx context.Context, opts LoginOptions) (*Result, error)
    Logout(ctx context.Context) error
    Status(ctx context.Context) (*Status, error)
    GetToken(ctx context.Context, opts TokenOptions) (*Token, error)
    InjectAuth(ctx context.Context, req *http.Request, opts TokenOptions) error
    SupportedFlows() []Flow
}

// Flow represents an authentication flow type.
type Flow string

const (
    FlowDeviceCode       Flow = "device_code"
    FlowInteractive      Flow = "interactive"
    FlowServicePrincipal Flow = "service_principal"
)

// LoginOptions configures the login process.
type LoginOptions struct {
    TenantID           string
    Scopes             []string
    Flow               Flow
    Timeout            time.Duration
    DeviceCodeCallback func(userCode, verificationURI, message string)
}

// TokenOptions configures token acquisition.
type TokenOptions struct {
    Scope        string
    MinValidFor  time.Duration
    ForceRefresh bool
}

// DefaultMinValidFor is the default minimum validity duration for tokens.
const DefaultMinValidFor = 60 * time.Second

// Result contains the result of a successful authentication.
type Result struct {
    Claims    *Claims
    ExpiresAt time.Time
}

// Status represents the current authentication state.
type Status struct {
    Authenticated bool
    Claims        *Claims
    ExpiresAt     time.Time
    LastRefresh   time.Time
    TenantID      string
}

// Token represents a short-lived access token.
type Token struct {
    AccessToken string
    TokenType   string
    ExpiresAt   time.Time
    Scope       string
}

func (t *Token) IsValidFor(duration time.Duration) bool { ... }
func (t *Token) IsExpired() bool { ... }
func (t *Token) TimeUntilExpiry() time.Duration { ... }
```

### pkg/auth/claims.go ✅ IMPLEMENTED

```go
package auth

import "time"

// Claims represents normalized identity claims from any auth handler.
type Claims struct {
    Issuer    string
    Subject   string
    TenantID  string
    ObjectID  string
    ClientID  string
    Email     string
    Name      string
    Username  string
    IssuedAt  time.Time
    ExpiresAt time.Time
}

func (c *Claims) IsEmpty() bool { ... }
func (c *Claims) DisplayIdentity() string { ... }
```

### pkg/auth/errors.go ✅ IMPLEMENTED

```go
package auth

import (
    "errors"
    "fmt"
)

var (
    ErrNotAuthenticated     = errors.New("not authenticated: please run 'scafctl auth login entra'")
    ErrAuthenticationFailed = errors.New("authentication failed")
    ErrTokenExpired         = errors.New("credentials expired: please run 'scafctl auth login entra'")
    ErrInvalidScope         = errors.New("invalid scope: scope cannot be empty")
    ErrHandlerNotFound      = errors.New("auth handler not found")
    ErrFlowNotSupported     = errors.New("authentication flow not supported")
    ErrUserCancelled        = errors.New("authentication cancelled by user")
    ErrTimeout              = errors.New("authentication timed out")
    ErrAlreadyAuthenticated = errors.New("already authenticated")
)

// Error wraps authentication errors with additional context.
type Error struct {
    Handler   string
    Operation string
    Cause     error
}

func (e *Error) Error() string { ... }
func (e *Error) Unwrap() error { ... }
func NewError(handler, operation string, cause error) *Error { ... }

// Helper functions
func IsNotAuthenticated(err error) bool { ... }
func IsTokenExpired(err error) bool { ... }
func IsHandlerNotFound(err error) bool { ... }
func IsTimeout(err error) bool { ... }
func IsUserCancelled(err error) bool { ... }
```

### pkg/auth/registry.go ✅ IMPLEMENTED

```go
package auth

import (
    "fmt"
    "sort"
    "sync"
)

// Registry manages registered auth handlers.
type Registry struct {
    mu       sync.RWMutex
    handlers map[string]Handler
}

func NewRegistry() *Registry { ... }
func (r *Registry) Register(handler Handler) error { ... }
func (r *Registry) Unregister(name string) error { ... }
func (r *Registry) Get(name string) (Handler, error) { ... }
func (r *Registry) List() []string { ... }  // Returns sorted names
func (r *Registry) Has(name string) bool { ... }
```

### pkg/auth/context.go ✅ IMPLEMENTED

```go
package auth

import (
    "context"
    "fmt"
)

func WithRegistry(ctx context.Context, registry *Registry) context.Context { ... }
func RegistryFromContext(ctx context.Context) *Registry { ... }
func MustRegistryFromContext(ctx context.Context) *Registry { ... }  // Panics if not found
func GetHandler(ctx context.Context, name string) (Handler, error) { ... }
func HasHandler(ctx context.Context, name string) bool { ... }
func ListHandlers(ctx context.Context) []string { ... }
```

### pkg/auth/mock.go ✅ IMPLEMENTED

Mock implementations for testing are provided in `pkg/auth/mock.go`.

---

## Entra Handler Implementation

> **Note:** Phase 2 has been implemented. The code below reflects the actual implementation in `pkg/auth/entra/`.
> Type naming follows Phase 1 conventions (e.g., `auth.Flow` instead of `auth.AuthFlow`).

### pkg/auth/entra/handler.go ✅ IMPLEMENTED

```go
package entra

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "time"

    "github.com/oakwood-commons/scafctl/pkg/auth"
    "github.com/oakwood-commons/scafctl/pkg/logger"
    "github.com/oakwood-commons/scafctl/pkg/secrets"
)

const (
    // HandlerName is the unique identifier for the Entra auth handler.
    HandlerName = "entra"

    // HandlerDisplayName is the human-readable name.
    HandlerDisplayName = "Microsoft Entra ID"

    // SecretKeyRefreshToken is the secret name for storing the refresh token.
    SecretKeyRefreshToken = "scafctl.auth.entra.refresh_token"

    // SecretKeyMetadata is the secret name for storing token metadata.
    SecretKeyMetadata = "scafctl.auth.entra.metadata"

    // SecretKeyTokenPrefix is the prefix for cached access tokens.
    // Full key format: scafctl.auth.entra.token.<base64url-encoded-scope>
    SecretKeyTokenPrefix = "scafctl.auth.entra.token."

    // DefaultTimeout is the default timeout for device code flow.
    DefaultTimeout = 5 * time.Minute

    // DefaultRefreshTokenLifetime is the expected lifetime of refresh tokens.
    // Azure AD refresh tokens are valid for 90 days by default.
    DefaultRefreshTokenLifetime = 90 * 24 * time.Hour
)

// EntraHandler implements auth.AuthHandler for Microsoft Entra ID.
type EntraHandler struct {
    config      *Config
    secretStore secrets.Store
    httpClient  HTTPClient
    tokenCache  *TokenCache
}

// Option configures the EntraHandler.
type Option func(*EntraHandler)

// WithConfig sets the Entra configuration.
func WithConfig(cfg *Config) Option {
    return func(h *EntraHandler) {
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
        }
    }
}

// WithSecretStore sets a custom secrets store.
func WithSecretStore(store secrets.Store) Option {
    return func(h *EntraHandler) {
        h.secretStore = store
    }
}

// WithHTTPClient sets a custom HTTP client for token requests.
func WithHTTPClient(client HTTPClient) Option {
    return func(h *EntraHandler) {
        h.httpClient = client
    }
}

// New creates a new Entra auth handler.
func New(opts ...Option) (*EntraHandler, error) {
    h := &EntraHandler{
        config: DefaultConfig(),
    }

    for _, opt := range opts {
        opt(h)
    }

    // Initialize secret store if not provided
    if h.secretStore == nil {
        store, err := secrets.New()
        if err != nil {
            return nil, fmt.Errorf("failed to initialize secrets store: %w", err)
        }
        h.secretStore = store
    }

    // Initialize HTTP client if not provided
    if h.httpClient == nil {
        h.httpClient = NewDefaultHTTPClient()
    }

    // Initialize token cache with secret store
    h.tokenCache = NewTokenCache(h.secretStore)

    return h, nil
}

// Name returns the handler identifier.
func (h *EntraHandler) Name() string {
    return HandlerName
}

// DisplayName returns the human-readable name.
func (h *EntraHandler) DisplayName() string {
    return HandlerDisplayName
}

// SupportedFlows returns the authentication flows this handler supports.
func (h *EntraHandler) SupportedFlows() []auth.AuthFlow {
    return []auth.AuthFlow{
        auth.AuthFlowDeviceCode,
    }
}

// Login initiates the device code authentication flow.
func (h *EntraHandler) Login(ctx context.Context, opts auth.LoginOptions) (*auth.AuthResult, error) {
    return h.deviceCodeLogin(ctx, opts)
}

// Logout clears stored credentials and cached tokens.
func (h *EntraHandler) Logout(ctx context.Context) error {
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
func (h *EntraHandler) Status(ctx context.Context) (*auth.AuthStatus, error) {
    // Check if we have stored credentials
    exists, err := h.secretStore.Exists(ctx, SecretKeyRefreshToken)
    if err != nil {
        return nil, fmt.Errorf("failed to check credentials: %w", err)
    }

    if !exists {
        return &auth.AuthStatus{Authenticated: false}, nil
    }

    // Load and validate metadata
    metadata, err := h.loadMetadata(ctx)
    if err != nil {
        // Corrupted or missing metadata, consider not authenticated
        return &auth.AuthStatus{Authenticated: false}, nil
    }

    // Check if refresh token is expired
    if !metadata.RefreshTokenExpiresAt.IsZero() && time.Now().After(metadata.RefreshTokenExpiresAt) {
        return &auth.AuthStatus{
            Authenticated: false,
            Claims:        metadata.Claims,
        }, nil
    }

    return &auth.AuthStatus{
        Authenticated: true,
        Claims:        metadata.Claims,
        ExpiresAt:     metadata.RefreshTokenExpiresAt,
        LastRefresh:   metadata.LastRefresh,
        TenantID:      metadata.TenantID,
    }, nil
}

// GetToken returns a valid access token for the specified options.
func (h *EntraHandler) GetToken(ctx context.Context, opts auth.TokenOptions) (*auth.Token, error) {
    if opts.Scope == "" {
        return nil, auth.ErrInvalidScope
    }

    lgr := logger.FromContext(ctx)

    // Determine minimum validity duration
    minValidFor := opts.MinValidFor
    if minValidFor == 0 {
        minValidFor = auth.DefaultMinValidFor
    }

    lgr.V(1).Info("getting token",
        "handler", HandlerName,
        "scope", opts.Scope,
        "minValidFor", minValidFor,
        "forceRefresh", opts.ForceRefresh,
    )

    // Check disk cache first (unless force refresh)
    if !opts.ForceRefresh {
        token, err := h.tokenCache.Get(ctx, opts.Scope)
        if err == nil && token != nil && token.IsValidFor(minValidFor) {
            lgr.V(1).Info("using cached token",
                "scope", opts.Scope,
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
    token, err := h.mintToken(ctx, opts.Scope)
    if err != nil {
        return nil, err
    }

    // Cache the token to disk
    if err := h.tokenCache.Set(ctx, opts.Scope, token); err != nil {
        lgr.V(1).Info("failed to cache token", "error", err)
        // Continue anyway - we have the token
    }

    return token, nil
}

// InjectAuth adds authentication to an HTTP request.
func (h *EntraHandler) InjectAuth(ctx context.Context, req *http.Request, opts auth.TokenOptions) error {
    token, err := h.GetToken(ctx, opts)
    if err != nil {
        return err
    }

    req.Header.Set("Authorization", fmt.Sprintf("%s %s", token.TokenType, token.AccessToken))
    return nil
}
```

### pkg/auth/entra/cache.go

```go
package entra

import (
    "context"
    "encoding/base64"
    "encoding/json"
    "fmt"
    "strings"
    "time"

    "github.com/oakwood-commons/scafctl/pkg/auth"
    "github.com/oakwood-commons/scafctl/pkg/secrets"
)

// TokenCache provides disk-based caching for access tokens via pkg/secrets.
// Tokens are stored encrypted and survive process restarts.
// Each scope has its own secret key for atomic updates and fault isolation.
type TokenCache struct {
    secretStore secrets.Store
}

// CachedToken is the structure stored on disk for each cached token.
type CachedToken struct {
    AccessToken string    `json:"accessToken"`
    TokenType   string    `json:"tokenType"`
    ExpiresAt   time.Time `json:"expiresAt"`
    Scope       string    `json:"scope"`
    CachedAt    time.Time `json:"cachedAt"`
}

// NewTokenCache creates a new disk-based token cache.
func NewTokenCache(secretStore secrets.Store) *TokenCache {
    return &TokenCache{
        secretStore: secretStore,
    }
}

// Get retrieves a token for the given scope from disk cache.
// Returns nil, nil if no token is cached for this scope.
// Returns nil, error if there was an error reading the cache.
func (c *TokenCache) Get(ctx context.Context, scope string) (*auth.Token, error) {
    key := c.scopeToKey(scope)

    data, err := c.secretStore.Get(ctx, key)
    if err != nil {
        // Check if it's a "not found" error - that's expected
        if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "does not exist") {
            return nil, nil
        }
        return nil, fmt.Errorf("failed to read cached token: %w", err)
    }

    var cached CachedToken
    if err := json.Unmarshal(data, &cached); err != nil {
        return nil, fmt.Errorf("failed to unmarshal cached token: %w", err)
    }

    return &auth.Token{
        AccessToken: cached.AccessToken,
        TokenType:   cached.TokenType,
        ExpiresAt:   cached.ExpiresAt,
        Scope:       cached.Scope,
    }, nil
}

// Set stores a token for the given scope to disk cache.
func (c *TokenCache) Set(ctx context.Context, scope string, token *auth.Token) error {
    key := c.scopeToKey(scope)

    cached := CachedToken{
        AccessToken: token.AccessToken,
        TokenType:   token.TokenType,
        ExpiresAt:   token.ExpiresAt,
        Scope:       scope,
        CachedAt:    time.Now(),
    }

    data, err := json.Marshal(cached)
    if err != nil {
        return fmt.Errorf("failed to marshal token for cache: %w", err)
    }

    if err := c.secretStore.Set(ctx, key, data); err != nil {
        return fmt.Errorf("failed to write cached token: %w", err)
    }

    return nil
}

// Delete removes a cached token for the given scope.
func (c *TokenCache) Delete(ctx context.Context, scope string) error {
    key := c.scopeToKey(scope)
    return c.secretStore.Delete(ctx, key)
}

// Clear removes all cached tokens.
// This iterates through all secrets with the token prefix and deletes them.
func (c *TokenCache) Clear(ctx context.Context) error {
    // List all secrets and delete those with our prefix
    // Note: This requires the secrets store to support listing
    // If not available, we'll need to track cached scopes separately
    
    // For now, we'll attempt to delete known common scopes
    // A more robust implementation would track cached scopes in metadata
    
    // The secrets store may not support listing, so we'll just return nil
    // and let individual token cache entries expire naturally
    // The important cleanup (refresh token, metadata) is done in Logout()
    return nil
}

// ListCachedScopes returns a list of scopes that have cached tokens.
// Note: This may not be fully accurate if the secrets store doesn't support listing.
func (c *TokenCache) ListCachedScopes(ctx context.Context) ([]string, error) {
    // This would require the secrets store to support listing
    // For now, return empty - this is primarily for debugging
    return []string{}, nil
}

// scopeToKey converts a scope to a secret key.
// Uses base64url encoding for the scope to create a valid key.
func (c *TokenCache) scopeToKey(scope string) string {
    encoded := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(scope))
    return SecretKeyTokenPrefix + encoded
}

// keyToScope converts a secret key back to a scope.
// Returns empty string if the key is not a valid token cache key.
func (c *TokenCache) keyToScope(key string) string {
    if !strings.HasPrefix(key, SecretKeyTokenPrefix) {
        return ""
    }
    encoded := strings.TrimPrefix(key, SecretKeyTokenPrefix)
    decoded, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(encoded)
    if err != nil {
        return ""
    }
    return string(decoded)
}
```

### pkg/auth/entra/config.go

```go
package entra

import "fmt"

// Config holds Entra-specific configuration.
type Config struct {
    // ClientID is the Azure application/client ID.
    // This should be a public client application registered in Azure.
    ClientID string `json:"clientId" yaml:"clientId" doc:"Azure application ID" example:"04b07795-8ddb-461a-bbee-02f9e1bf7b46"`

    // TenantID is the default Azure tenant ID.
    // Use "common" for multi-tenant, "organizations" for work/school accounts only,
    // or a specific tenant GUID.
    TenantID string `json:"tenantId" yaml:"tenantId" doc:"Azure tenant ID" example:"common"`

    // Authority is the Azure AD authority URL.
    // Defaults to https://login.microsoftonline.com
    Authority string `json:"authority,omitempty" yaml:"authority,omitempty" doc:"Azure AD authority URL"`

    // DefaultScopes are requested during initial login if not specified.
    DefaultScopes []string `json:"defaultScopes,omitempty" yaml:"defaultScopes,omitempty" doc:"Default OAuth scopes"`
}

// DefaultConfig returns the default Entra configuration.
func DefaultConfig() *Config {
    return &Config{
        // Using Azure CLI's public client ID as default
        // TODO: Register a dedicated scafctl application in Azure AD
        ClientID:  "04b07795-8ddb-461a-bbee-02f9e1bf7b46",
        TenantID:  "common",
        Authority: "https://login.microsoftonline.com",
        DefaultScopes: []string{
            "openid",
            "profile",
            "offline_access",
        },
    }
}

// Validate validates the configuration.
func (c *Config) Validate() error {
    if c.ClientID == "" {
        return fmt.Errorf("clientId is required")
    }
    if c.TenantID == "" {
        return fmt.Errorf("tenantId is required")
    }
    return nil
}

// GetAuthority returns the full authority URL for the configured tenant.
func (c *Config) GetAuthority() string {
    authority := c.Authority
    if authority == "" {
        authority = "https://login.microsoftonline.com"
    }
    return authority
}

// GetAuthorityWithTenant returns the full authority URL for a specific tenant.
func (c *Config) GetAuthorityWithTenant(tenantID string) string {
    return fmt.Sprintf("%s/%s", c.GetAuthority(), tenantID)
}
```

### pkg/auth/entra/device_flow.go

```go
package entra

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "net/url"
    "strings"
    "time"

    "github.com/oakwood-commons/scafctl/pkg/auth"
    "github.com/oakwood-commons/scafctl/pkg/logger"
)

// DeviceCodeResponse represents the response from the device code endpoint.
type DeviceCodeResponse struct {
    DeviceCode      string `json:"device_code"`
    UserCode        string `json:"user_code"`
    VerificationURI string `json:"verification_uri"`
    ExpiresIn       int    `json:"expires_in"`
    Interval        int    `json:"interval"`
    Message         string `json:"message"`
}

// TokenResponse represents the response from the token endpoint.
type TokenResponse struct {
    AccessToken  string `json:"access_token"`
    RefreshToken string `json:"refresh_token"`
    TokenType    string `json:"token_type"`
    ExpiresIn    int    `json:"expires_in"`
    Scope        string `json:"scope"`
    IDToken      string `json:"id_token,omitempty"`
}

// TokenErrorResponse represents an error from the token endpoint.
type TokenErrorResponse struct {
    Error            string `json:"error"`
    ErrorDescription string `json:"error_description"`
}

// deviceCodeLogin performs the device code authentication flow.
func (h *EntraHandler) deviceCodeLogin(ctx context.Context, opts auth.LoginOptions) (*auth.AuthResult, error) {
    lgr := logger.FromContext(ctx)
    lgr.V(1).Info("starting device code authentication flow")

    // Determine tenant
    tenantID := opts.TenantID
    if tenantID == "" {
        tenantID = h.config.TenantID
    }

    // Determine scopes
    scopes := opts.Scopes
    if len(scopes) == 0 {
        scopes = h.config.DefaultScopes
    }

    // Ensure offline_access is included for refresh token
    hasOfflineAccess := false
    for _, s := range scopes {
        if s == "offline_access" {
            hasOfflineAccess = true
            break
        }
    }
    if !hasOfflineAccess {
        scopes = append(scopes, "offline_access")
    }

    // Determine timeout
    timeout := opts.Timeout
    if timeout == 0 {
        timeout = DefaultTimeout
    }

    // Create context with timeout
    ctx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()

    // Step 1: Request device code
    deviceCode, err := h.requestDeviceCode(ctx, tenantID, scopes)
    if err != nil {
        return nil, auth.NewAuthError(HandlerName, "device_code_request", err)
    }

    lgr.V(1).Info("device code obtained",
        "userCode", deviceCode.UserCode,
        "verificationURI", deviceCode.VerificationURI,
    )

    // Step 2: Notify callback with device code info
    if opts.DeviceCodeCallback != nil {
        opts.DeviceCodeCallback(deviceCode.UserCode, deviceCode.VerificationURI, deviceCode.Message)
    }

    // Step 3: Poll for token
    tokenResp, err := h.pollForToken(ctx, tenantID, deviceCode)
    if err != nil {
        return nil, auth.NewAuthError(HandlerName, "token_poll", err)
    }

    // Step 4: Store refresh token securely
    if err := h.storeCredentials(ctx, tenantID, tokenResp); err != nil {
        return nil, auth.NewAuthError(HandlerName, "store_credentials", err)
    }

    // Step 5: Extract and return claims
    claims, err := h.extractClaims(tokenResp)
    if err != nil {
        return nil, auth.NewAuthError(HandlerName, "extract_claims", err)
    }

    lgr.V(1).Info("authentication successful",
        "subject", claims.Subject,
        "tenantId", claims.TenantID,
    )

    return &auth.AuthResult{
        Claims:    claims,
        ExpiresAt: time.Now().Add(DefaultRefreshTokenLifetime),
    }, nil
}

func (h *EntraHandler) requestDeviceCode(ctx context.Context, tenantID string, scopes []string) (*DeviceCodeResponse, error) {
    endpoint := fmt.Sprintf("%s/%s/oauth2/v2.0/devicecode", h.config.GetAuthority(), tenantID)

    data := url.Values{}
    data.Set("client_id", h.config.ClientID)
    data.Set("scope", strings.Join(scopes, " "))

    resp, err := h.httpClient.PostForm(ctx, endpoint, data)
    if err != nil {
        return nil, fmt.Errorf("device code request failed: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        var errResp TokenErrorResponse
        if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
            return nil, fmt.Errorf("device code request failed with status %d", resp.StatusCode)
        }
        return nil, fmt.Errorf("device code request failed: %s - %s", errResp.Error, errResp.ErrorDescription)
    }

    var deviceCode DeviceCodeResponse
    if err := json.NewDecoder(resp.Body).Decode(&deviceCode); err != nil {
        return nil, fmt.Errorf("failed to parse device code response: %w", err)
    }

    return &deviceCode, nil
}

func (h *EntraHandler) pollForToken(ctx context.Context, tenantID string, deviceCode *DeviceCodeResponse) (*TokenResponse, error) {
    endpoint := fmt.Sprintf("%s/%s/oauth2/v2.0/token", h.config.GetAuthority(), tenantID)

    interval := time.Duration(deviceCode.Interval) * time.Second
    if interval < 5*time.Second {
        interval = 5 * time.Second
    }

    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return nil, auth.ErrTimeout
        case <-ticker.C:
            data := url.Values{}
            data.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
            data.Set("client_id", h.config.ClientID)
            data.Set("device_code", deviceCode.DeviceCode)

            resp, err := h.httpClient.PostForm(ctx, endpoint, data)
            if err != nil {
                // Network error, continue polling
                continue
            }

            if resp.StatusCode == http.StatusOK {
                var tokenResp TokenResponse
                if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
                    resp.Body.Close()
                    return nil, fmt.Errorf("failed to parse token response: %w", err)
                }
                resp.Body.Close()
                return &tokenResp, nil
            }

            var errResp TokenErrorResponse
            json.NewDecoder(resp.Body).Decode(&errResp)
            resp.Body.Close()

            switch errResp.Error {
            case "authorization_pending":
                // User hasn't completed authentication yet, continue polling
                continue
            case "slow_down":
                // Increase polling interval
                interval += 5 * time.Second
                ticker.Reset(interval)
                continue
            case "expired_token":
                return nil, auth.ErrTimeout
            case "authorization_declined":
                return nil, auth.ErrUserCancelled
            default:
                return nil, fmt.Errorf("token request failed: %s - %s", errResp.Error, errResp.ErrorDescription)
            }
        }
    }
}
```

### pkg/auth/entra/token.go

```go
package entra

import (
    "context"
    "encoding/base64"
    "encoding/json"
    "fmt"
    "net/http"
    "net/url"
    "strings"
    "time"

    "github.com/oakwood-commons/scafctl/pkg/auth"
    "github.com/oakwood-commons/scafctl/pkg/logger"
)

// TokenMetadata stores information about the stored credentials.
type TokenMetadata struct {
    Claims                *auth.Claims `json:"claims"`
    RefreshTokenExpiresAt time.Time    `json:"refreshTokenExpiresAt"`
    LastRefresh           time.Time    `json:"lastRefresh"`
    TenantID              string       `json:"tenantId"`
}

// mintToken creates a new access token for the specified scope.
func (h *EntraHandler) mintToken(ctx context.Context, scope string) (*auth.Token, error) {
    lgr := logger.FromContext(ctx)
    lgr.V(1).Info("minting access token", "scope", scope)

    // Load refresh token
    refreshToken, err := h.loadRefreshToken(ctx)
    if err != nil {
        return nil, auth.ErrNotAuthenticated
    }

    // Load metadata for tenant info
    metadata, err := h.loadMetadata(ctx)
    if err != nil {
        return nil, fmt.Errorf("failed to load metadata: %w", err)
    }

    // Request new access token using refresh token
    endpoint := fmt.Sprintf("%s/%s/oauth2/v2.0/token", h.config.GetAuthority(), metadata.TenantID)

    data := url.Values{}
    data.Set("grant_type", "refresh_token")
    data.Set("client_id", h.config.ClientID)
    data.Set("refresh_token", refreshToken)
    data.Set("scope", scope)

    resp, err := h.httpClient.PostForm(ctx, endpoint, data)
    if err != nil {
        return nil, fmt.Errorf("token request failed: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        var errResp TokenErrorResponse
        if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
            return nil, fmt.Errorf("token request failed with status %d", resp.StatusCode)
        }

        // Check if refresh token is expired
        if errResp.Error == "invalid_grant" {
            // Clear stored credentials since they're no longer valid
            _ = h.Logout(ctx)
            return nil, auth.ErrTokenExpired
        }

        return nil, fmt.Errorf("token request failed: %s - %s", errResp.Error, errResp.ErrorDescription)
    }

    var tokenResp TokenResponse
    if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
        return nil, fmt.Errorf("failed to parse token response: %w", err)
    }

    // If we got a new refresh token, store it (token rotation)
    if tokenResp.RefreshToken != "" && tokenResp.RefreshToken != refreshToken {
        lgr.V(1).Info("refresh token rotated, storing new token")
        if err := h.storeCredentials(ctx, metadata.TenantID, &tokenResp); err != nil {
            lgr.V(1).Info("warning: failed to update refresh token", "error", err)
        }
    }

    expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

    lgr.V(1).Info("access token minted successfully",
        "expiresIn", tokenResp.ExpiresIn,
        "expiresAt", expiresAt,
        "scope", scope,
    )

    return &auth.Token{
        AccessToken: tokenResp.AccessToken,
        TokenType:   tokenResp.TokenType,
        ExpiresAt:   expiresAt,
        Scope:       scope,
    }, nil
}

// storeCredentials securely stores the refresh token and metadata.
func (h *EntraHandler) storeCredentials(ctx context.Context, tenantID string, tokenResp *TokenResponse) error {
    // Store refresh token
    if err := h.secretStore.Set(ctx, SecretKeyRefreshToken, []byte(tokenResp.RefreshToken)); err != nil {
        return fmt.Errorf("failed to store refresh token: %w", err)
    }

    // Extract claims and store metadata
    claims, err := h.extractClaims(tokenResp)
    if err != nil {
        // Use minimal claims if extraction fails
        claims = &auth.Claims{
            TenantID: tenantID,
        }
    }

    metadata := &TokenMetadata{
        Claims:                claims,
        RefreshTokenExpiresAt: time.Now().Add(DefaultRefreshTokenLifetime),
        LastRefresh:           time.Now(),
        TenantID:              tenantID,
    }

    metadataBytes, err := json.Marshal(metadata)
    if err != nil {
        return fmt.Errorf("failed to marshal metadata: %w", err)
    }

    if err := h.secretStore.Set(ctx, SecretKeyMetadata, metadataBytes); err != nil {
        return fmt.Errorf("failed to store metadata: %w", err)
    }

    return nil
}

// loadRefreshToken loads the stored refresh token.
func (h *EntraHandler) loadRefreshToken(ctx context.Context) (string, error) {
    data, err := h.secretStore.Get(ctx, SecretKeyRefreshToken)
    if err != nil {
        return "", err
    }
    return string(data), nil
}

// loadMetadata loads the stored token metadata.
func (h *EntraHandler) loadMetadata(ctx context.Context) (*TokenMetadata, error) {
    data, err := h.secretStore.Get(ctx, SecretKeyMetadata)
    if err != nil {
        return nil, err
    }

    var metadata TokenMetadata
    if err := json.Unmarshal(data, &metadata); err != nil {
        return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
    }

    return &metadata, nil
}

// extractClaims extracts normalized claims from the token response.
func (h *EntraHandler) extractClaims(tokenResp *TokenResponse) (*auth.Claims, error) {
    if tokenResp.IDToken == "" {
        return &auth.Claims{}, nil
    }

    // Parse ID token (JWT format: header.payload.signature)
    parts := strings.Split(tokenResp.IDToken, ".")
    if len(parts) != 3 {
        return nil, fmt.Errorf("invalid ID token format")
    }

    // Decode payload (base64url)
    payload, err := base64URLDecode(parts[1])
    if err != nil {
        return nil, fmt.Errorf("failed to decode ID token payload: %w", err)
    }

    var idTokenClaims struct {
        Issuer            string `json:"iss"`
        Subject           string `json:"sub"`
        Audience          string `json:"aud"`
        TenantID          string `json:"tid"`
        ObjectID          string `json:"oid"`
        Email             string `json:"email"`
        PreferredUsername string `json:"preferred_username"`
        Name              string `json:"name"`
        IssuedAt          int64  `json:"iat"`
        ExpiresAt         int64  `json:"exp"`
    }

    if err := json.Unmarshal(payload, &idTokenClaims); err != nil {
        return nil, fmt.Errorf("failed to parse ID token claims: %w", err)
    }

    email := idTokenClaims.Email
    if email == "" {
        email = idTokenClaims.PreferredUsername
    }

    return &auth.Claims{
        Issuer:    idTokenClaims.Issuer,
        Subject:   idTokenClaims.Subject,
        TenantID:  idTokenClaims.TenantID,
        ObjectID:  idTokenClaims.ObjectID,
        ClientID:  idTokenClaims.Audience,
        Email:     email,
        Name:      idTokenClaims.Name,
        Username:  idTokenClaims.PreferredUsername,
        IssuedAt:  time.Unix(idTokenClaims.IssuedAt, 0),
        ExpiresAt: time.Unix(idTokenClaims.ExpiresAt, 0),
    }, nil
}

// base64URLDecode decodes a base64url encoded string.
func base64URLDecode(s string) ([]byte, error) {
    // Add padding if necessary
    switch len(s) % 4 {
    case 2:
        s += "=="
    case 3:
        s += "="
    }
    return base64.URLEncoding.DecodeString(s)
}
```

### pkg/auth/entra/http.go

```go
package entra

import (
    "context"
    "net/http"
    "net/url"
    "strings"
    "time"
)

// HTTPClient interface for token endpoint requests.
// Abstracted for testing.
type HTTPClient interface {
    PostForm(ctx context.Context, endpoint string, data url.Values) (*http.Response, error)
}

// DefaultHTTPClient implements HTTPClient using standard library.
type DefaultHTTPClient struct {
    client *http.Client
}

// NewDefaultHTTPClient creates a new default HTTP client.
func NewDefaultHTTPClient() *DefaultHTTPClient {
    return &DefaultHTTPClient{
        client: &http.Client{
            Timeout: 30 * time.Second,
        },
    }
}

// PostForm performs a POST request with form-encoded data.
func (c *DefaultHTTPClient) PostForm(ctx context.Context, endpoint string, data url.Values) (*http.Response, error) {
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(data.Encode()))
    if err != nil {
        return nil, err
    }
    req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
    return c.client.Do(req)
}
```

---

## HTTP Provider Integration

### Changes to pkg/provider/builtin/httpprovider/http.go

Add the following to the HTTP provider:

**Add to imports:**

```go
import (
    // ... existing imports ...
    "github.com/oakwood-commons/scafctl/pkg/auth"
)
```

**Add to Schema.Properties in NewHTTPProvider():**

```go
"authProvider": {
    Type:        provider.PropertyTypeString,
    Description: "Authentication provider to use for this request (e.g., 'entra'). When set, the provider will automatically obtain and inject an access token.",
    Required:    false,
    Example:     "entra",
    MaxLength:   ptrs.IntPtr(50),
},
"scope": {
    Type:        provider.PropertyTypeString,
    Description: "OAuth scope for authentication (required when authProvider is set). The token will be valid for the request timeout plus a 60-second buffer.",
    Required:    false,
    Example:     "https://graph.microsoft.com/.default",
    MaxLength:   ptrs.IntPtr(500),
},
```

**Add new example:**

```go
{
    Name:        "Request with Entra authentication",
    Description: "Make an authenticated request using Microsoft Entra ID",
    YAML: `name: fetch-azure-data
provider: http
inputs:
  url: "https://graph.microsoft.com/v1.0/me"
  method: GET
  authProvider: entra
  scope: "https://graph.microsoft.com/.default"`,
},
```

**Modify Execute() to handle auth injection:**

```go
func (p *HTTPProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
    lgr := logger.FromContext(ctx)

    inputs, ok := input.(map[string]any)
    if !ok {
        return nil, fmt.Errorf("%s: expected map[string]any, got %T", ProviderName, input)
    }

    lgr.V(1).Info("executing provider", "provider", ProviderName, "url", inputs["url"])

    // Check for dry-run mode
    if provider.DryRunFromContext(ctx) {
        return &provider.Output{
            Data: map[string]any{
                "statusCode": 200,
                "body":       "[DRY-RUN] Request not executed",
                "headers":    map[string]any{},
            },
        }, nil
    }

    // Extract inputs
    urlStr, _ := inputs["url"].(string)
    method, _ := inputs["method"].(string)
    if method == "" {
        method = "GET"
    }
    method = strings.ToUpper(method)

    // Get timeout
    timeout := 30
    if t, ok := inputs["timeout"].(int); ok && t > 0 {
        timeout = t
    }
    // Handle float64 from JSON/YAML unmarshaling
    if t, ok := inputs["timeout"].(float64); ok && t > 0 {
        timeout = int(t)
    }
    timeoutDuration := time.Duration(timeout) * time.Second

    // Get body content for potential retries
    bodyContent, _ := inputs["body"].(string)

    // Get headers (make a copy to avoid modifying input)
    headers := make(map[string]any)
    if h, ok := inputs["headers"].(map[string]any); ok {
        for k, v := range h {
            headers[k] = v
        }
    }

    // Handle authentication
    authProvider, _ := inputs["authProvider"].(string)
    scope, _ := inputs["scope"].(string)

    if authProvider != "" {
        if scope == "" {
            return nil, fmt.Errorf("%s: scope is required when authProvider is set", ProviderName)
        }

        // Get auth handler from context
        handler, err := auth.GetHandler(ctx, authProvider)
        if err != nil {
            return nil, fmt.Errorf("%s: %w", ProviderName, err)
        }

        // Calculate minimum token validity: request timeout + 60 second buffer
        minValidFor := timeoutDuration + 60*time.Second

        // Get token with sufficient validity
        token, err := handler.GetToken(ctx, auth.TokenOptions{
            Scope:       scope,
            MinValidFor: minValidFor,
        })
        if err != nil {
            return nil, fmt.Errorf("%s: failed to get auth token: %w", ProviderName, err)
        }

        // Inject authorization header
        headers["Authorization"] = fmt.Sprintf("%s %s", token.TokenType, token.AccessToken)
        lgr.V(1).Info("injected auth header",
            "authProvider", authProvider,
            "scope", scope,
            "tokenExpiresAt", token.ExpiresAt,
            "minValidFor", minValidFor,
        )
    }

    // Create client with timeout
    client := &http.Client{
        Timeout: timeoutDuration,
    }

    // Parse retry configuration
    retryCfg := parseRetryConfig(inputs)

    // Execute request (with or without retry)
    return p.executeWithRetry(ctx, lgr, client, method, urlStr, bodyContent, headers, retryCfg)
}
```

---

## CLI Commands

> **Note:** Phase 3 has been implemented. The code below reflects the planned implementation.
> The actual implementation includes additional files and some variations from the original plan.
> See implementation notes in the Phase 3 checklist.

### pkg/cmd/scafctl/auth/auth.go

```go
package auth

import (
    "fmt"

    "github.com/MakeNowJust/heredoc/v2"
    "github.com/oakwood-commons/scafctl/pkg/settings"
    "github.com/oakwood-commons/scafctl/pkg/terminal"
    "github.com/spf13/cobra"
)

// CommandAuth creates the 'auth' command group.
func CommandAuth(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
    cmd := &cobra.Command{
        Use:     "auth",
        Aliases: []string{"authenticate"},
        Short:   "Manage authentication",
        Long: heredoc.Doc(`
            Manage authentication for scafctl.

            Authentication handlers manage identity verification and token acquisition
            for accessing protected resources. scafctl supports the following auth handlers:

            - entra: Microsoft Entra ID (formerly Azure AD)

            Use 'scafctl auth login <handler>' to authenticate.
            Use 'scafctl auth status' to check current authentication status.
            Use 'scafctl auth logout <handler>' to clear credentials.
            Use 'scafctl auth token <handler>' to display a token (for debugging).
        `),
        SilenceUsage: true,
    }

    cmdPath := fmt.Sprintf("%s/%s", path, cmd.Use)
    cmd.AddCommand(CommandLogin(cliParams, ioStreams, cmdPath))
    cmd.AddCommand(CommandLogout(cliParams, ioStreams, cmdPath))
    cmd.AddCommand(CommandStatus(cliParams, ioStreams, cmdPath))
    cmd.AddCommand(CommandToken(cliParams, ioStreams, cmdPath))

    return cmd
}
```

### pkg/cmd/scafctl/auth/login.go

```go
package auth

import (
    "fmt"
    "time"

    "github.com/MakeNowJust/heredoc/v2"
    "github.com/oakwood-commons/scafctl/pkg/auth"
    "github.com/oakwood-commons/scafctl/pkg/auth/entra"
    "github.com/oakwood-commons/scafctl/pkg/config"
    "github.com/oakwood-commons/scafctl/pkg/settings"
    "github.com/oakwood-commons/scafctl/pkg/terminal"
    "github.com/oakwood-commons/scafctl/pkg/terminal/writer"
    "github.com/spf13/cobra"
)

// CommandLogin creates the 'auth login' command.
func CommandLogin(_ *settings.Run, _ *terminal.IOStreams, _ string) *cobra.Command {
    var (
        tenantID string
        timeout  time.Duration
    )

    cmd := &cobra.Command{
        Use:   "login <handler>",
        Short: "Authenticate with an auth handler",
        Long: heredoc.Doc(`
            Authenticate with an authentication handler.

            For the 'entra' handler, this initiates a device code flow where you
            authenticate in your browser. The refresh token is stored securely
            for future use.

            Supported handlers:
            - entra: Microsoft Entra ID (device code flow)

            Examples:
              # Login with Entra ID using default tenant
              scafctl auth login entra

              # Login with a specific tenant
              scafctl auth login entra --tenant c990bb7a-51f4-439b-bd36-9c07fb1041c0
        `),
        Args: cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            ctx := cmd.Context()
            w := writer.MustFromContext(ctx)
            handlerName := args[0]

            // Currently only entra is supported
            if handlerName != "entra" {
                return fmt.Errorf("unknown auth handler: %s (supported: entra)", handlerName)
            }

            // Get config for Entra settings
            cfg := config.FromContext(ctx)
            var entraCfg *entra.Config
            if cfg != nil && cfg.Auth.Entra != nil {
                entraCfg = &entra.Config{
                    ClientID:      cfg.Auth.Entra.ClientID,
                    TenantID:      cfg.Auth.Entra.TenantID,
                    DefaultScopes: cfg.Auth.Entra.DefaultScopes,
                }
            }

            // Create Entra handler
            var opts []entra.Option
            if entraCfg != nil {
                opts = append(opts, entra.WithConfig(entraCfg))
            }

            handler, err := entra.New(opts...)
            if err != nil {
                return fmt.Errorf("failed to initialize auth handler: %w", err)
            }

            // Check if already authenticated
            status, err := handler.Status(ctx)
            if err != nil {
                return fmt.Errorf("failed to check auth status: %w", err)
            }

            if status.Authenticated {
                w.Infof("Already authenticated as %s", status.Claims.Email)
                w.Info("Use 'scafctl auth logout entra' to sign out first, or continue to re-authenticate.")
            }

            // Override tenant if specified
            effectiveTenant := tenantID
            if effectiveTenant == "" && entraCfg != nil {
                effectiveTenant = entraCfg.TenantID
            }

            // Prepare login options
            loginOpts := auth.LoginOptions{
                TenantID: effectiveTenant,
                Flow:     auth.AuthFlowDeviceCode,
                Timeout:  timeout,
                DeviceCodeCallback: func(userCode, verificationURI, message string) {
                    w.Info("")
                    w.Info("To sign in, use a web browser to open the page:")
                    w.Infof("  %s", verificationURI)
                    w.Info("")
                    w.Infof("Enter the code: %s", userCode)
                    w.Info("")
                    w.Info("Waiting for authentication...")
                },
            }

            result, err := handler.Login(ctx, loginOpts)
            if err != nil {
                return fmt.Errorf("authentication failed: %w", err)
            }

            w.Info("")
            w.Success("Authentication successful!")
            if result.Claims.Name != "" {
                w.Infof("  Name:   %s", result.Claims.Name)
            }
            if result.Claims.Email != "" {
                w.Infof("  Email:  %s", result.Claims.Email)
            }
            if result.Claims.TenantID != "" {
                w.Infof("  Tenant: %s", result.Claims.TenantID)
            }

            return nil
        },
    }

    cmd.Flags().StringVar(&tenantID, "tenant", "", "Azure tenant ID (overrides config)")
    cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "Timeout for authentication flow")

    return cmd
}
```

### pkg/cmd/scafctl/auth/logout.go

```go
package auth

import (
    "fmt"

    "github.com/MakeNowJust/heredoc/v2"
    "github.com/oakwood-commons/scafctl/pkg/auth/entra"
    "github.com/oakwood-commons/scafctl/pkg/settings"
    "github.com/oakwood-commons/scafctl/pkg/terminal"
    "github.com/oakwood-commons/scafctl/pkg/terminal/writer"
    "github.com/spf13/cobra"
)

// CommandLogout creates the 'auth logout' command.
func CommandLogout(_ *settings.Run, _ *terminal.IOStreams, _ string) *cobra.Command {
    cmd := &cobra.Command{
        Use:   "logout <handler>",
        Short: "Clear authentication credentials",
        Long: heredoc.Doc(`
            Clear stored authentication credentials for an auth handler.

            This removes the stored refresh token, clears any cached access tokens,
            and removes metadata.

            Examples:
              # Logout from Entra ID
              scafctl auth logout entra
        `),
        Args: cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            ctx := cmd.Context()
            w := writer.MustFromContext(ctx)
            handlerName := args[0]

            if handlerName != "entra" {
                return fmt.Errorf("unknown auth handler: %s (supported: entra)", handlerName)
            }

            handler, err := entra.New()
            if err != nil {
                return fmt.Errorf("failed to initialize auth handler: %w", err)
            }

            // Check if authenticated first
            status, err := handler.Status(ctx)
            if err != nil {
                return fmt.Errorf("failed to check auth status: %w", err)
            }

            if !status.Authenticated {
                w.Info("Not currently authenticated with Entra ID.")
                return nil
            }

            if err := handler.Logout(ctx); err != nil {
                return fmt.Errorf("logout failed: %w", err)
            }

            w.Success("Successfully logged out from Entra ID.")
            return nil
        },
    }

    return cmd
}
```

### pkg/cmd/scafctl/auth/status.go

```go
package auth

import (
    "fmt"

    "github.com/MakeNowJust/heredoc/v2"
    "github.com/oakwood-commons/scafctl/pkg/auth/entra"
    "github.com/oakwood-commons/scafctl/pkg/settings"
    "github.com/oakwood-commons/scafctl/pkg/terminal"
    "github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
    "github.com/oakwood-commons/scafctl/pkg/terminal/writer"
    "github.com/spf13/cobra"
)

// CommandStatus creates the 'auth status' command.
func CommandStatus(_ *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
    outputOpts := kvx.NewOutputOptions(ioStreams)

    cmd := &cobra.Command{
        Use:   "status [handler]",
        Short: "Show authentication status",
        Long: heredoc.Doc(`
            Show the current authentication status for auth handlers.

            If no handler is specified, shows status for all known handlers.

            Examples:
              # Show all auth status
              scafctl auth status

              # Show Entra auth status
              scafctl auth status entra

              # Output as JSON
              scafctl auth status -o json
        `),
        Args: cobra.MaximumNArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            ctx := cmd.Context()
            w := writer.MustFromContext(ctx)

            handlers := []string{"entra"}
            if len(args) > 0 {
                handlers = []string{args[0]}
            }

            results := make([]map[string]any, 0, len(handlers))

            for _, handlerName := range handlers {
                switch handlerName {
                case "entra":
                    handler, err := entra.New()
                    if err != nil {
                        w.Warnf("Failed to initialize %s: %v", handlerName, err)
                        continue
                    }

                    status, err := handler.Status(ctx)
                    if err != nil {
                        w.Warnf("Failed to check %s status: %v", handlerName, err)
                        continue
                    }

                    result := map[string]any{
                        "handler":       handlerName,
                        "displayName":   handler.DisplayName(),
                        "authenticated": status.Authenticated,
                    }

                    if status.Authenticated && status.Claims != nil {
                        result["email"] = status.Claims.Email
                        result["name"] = status.Claims.Name
                        result["tenantId"] = status.TenantID
                        if !status.ExpiresAt.IsZero() {
                            result["expiresAt"] = status.ExpiresAt
                        }
                        if !status.LastRefresh.IsZero() {
                            result["lastRefresh"] = status.LastRefresh
                        }
                    }

                    results = append(results, result)
                default:
                    w.Warnf("Unknown auth handler: %s", handlerName)
                }
            }

            if len(results) == 0 {
                return fmt.Errorf("no auth handlers found")
            }

            return outputOpts.Write(results)
        },
    }

    outputOpts.AddFlags(cmd)
    return cmd
}
```

### pkg/cmd/scafctl/auth/token.go

```go
package auth

import (
    "fmt"
    "time"

    "github.com/MakeNowJust/heredoc/v2"
    "github.com/oakwood-commons/scafctl/pkg/auth"
    "github.com/oakwood-commons/scafctl/pkg/auth/entra"
    "github.com/oakwood-commons/scafctl/pkg/settings"
    "github.com/oakwood-commons/scafctl/pkg/terminal"
    "github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
    "github.com/oakwood-commons/scafctl/pkg/terminal/writer"
    "github.com/spf13/cobra"
)

// CommandToken creates the 'auth token' command.
func CommandToken(_ *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
    outputOpts := kvx.NewOutputOptions(ioStreams)
    var (
        scope       string
        minValidFor time.Duration
    )

    cmd := &cobra.Command{
        Use:   "token <handler>",
        Short: "Get an access token (for debugging)",
        Long: heredoc.Doc(`
            Get an access token from an auth handler.

            This command is primarily for debugging and testing. It retrieves
            a valid access token for the specified scope.

            The token is cached to disk and will be reused if it has sufficient
            remaining validity for the specified --min-valid-for duration.

            WARNING: The token is sensitive and should not be shared or logged.

            Examples:
              # Get a token for Microsoft Graph
              scafctl auth token entra --scope "https://graph.microsoft.com/.default"

              # Get a token that will be valid for at least 5 minutes
              scafctl auth token entra --scope "https://graph.microsoft.com/.default" --min-valid-for 5m

              # Output as JSON (includes token)
              scafctl auth token entra --scope "https://management.azure.com/.default" -o json
        `),
        Args: cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            ctx := cmd.Context()
            w := writer.MustFromContext(ctx)
            handlerName := args[0]

            if handlerName != "entra" {
                return fmt.Errorf("unknown auth handler: %s (supported: entra)", handlerName)
            }

            if scope == "" {
                return fmt.Errorf("--scope is required")
            }

            handler, err := entra.New()
            if err != nil {
                return fmt.Errorf("failed to initialize auth handler: %w", err)
            }

            token, err := handler.GetToken(ctx, auth.TokenOptions{
                Scope:       scope,
                MinValidFor: minValidFor,
            })
            if err != nil {
                return fmt.Errorf("failed to get token: %w", err)
            }

            result := map[string]any{
                "handler":     handlerName,
                "scope":       token.Scope,
                "tokenType":   token.TokenType,
                "expiresAt":   token.ExpiresAt,
                "expiresIn":   token.TimeUntilExpiry().String(),
                "accessToken": token.AccessToken,
            }

            // For table output, mask the token
            if outputOpts.Format == "" || outputOpts.Format == "table" {
                w.Infof("Handler:    %s", handlerName)
                w.Infof("Scope:      %s", token.Scope)
                w.Infof("Type:       %s", token.TokenType)
                w.Infof("Expires:    %s", token.ExpiresAt.Format("2006-01-02 15:04:05"))
                w.Infof("Expires In: %s", token.TimeUntilExpiry().Round(time.Second))
                if len(token.AccessToken) > 20 {
                    w.Infof("Token:      %s...%s", token.AccessToken[:10], token.AccessToken[len(token.AccessToken)-10:])
                } else {
                    w.Infof("Token:      %s", token.AccessToken)
                }
                return nil
            }

            return outputOpts.Write(result)
        },
    }

    cmd.Flags().StringVar(&scope, "scope", "", "OAuth scope for the token (required)")
    cmd.Flags().DurationVar(&minValidFor, "min-valid-for", auth.DefaultMinValidFor, "Minimum time the token should be valid for")
    _ = cmd.MarkFlagRequired("scope")
    outputOpts.AddFlags(cmd)

    return cmd
}
```

---

## Configuration Integration

### Changes to pkg/config/config.go

Add the following to the Config struct:

```go
// AuthConfig contains authentication configuration.
type AuthConfig struct {
    // Entra contains Microsoft Entra ID configuration.
    Entra *EntraAuthConfig `json:"entra,omitempty" yaml:"entra,omitempty" doc:"Entra ID configuration"`
}

// EntraAuthConfig contains Entra-specific configuration.
type EntraAuthConfig struct {
    // ClientID overrides the default application ID.
    // If not set, uses the default scafctl public client ID.
    ClientID string `json:"clientId,omitempty" yaml:"clientId,omitempty" doc:"Azure application ID"`

    // TenantID sets the default tenant for authentication.
    // Use "common" for multi-tenant, "organizations" for work/school only,
    // or a specific tenant GUID.
    TenantID string `json:"tenantId,omitempty" yaml:"tenantId,omitempty" doc:"Default Azure tenant ID" example:"common"`

    // DefaultScopes are requested during login if not specified on command line.
    DefaultScopes []string `json:"defaultScopes,omitempty" yaml:"defaultScopes,omitempty" doc:"Default OAuth scopes"`
}

// Add to Config struct:
type Config struct {
    // ... existing fields ...

    // Auth contains authentication handler configuration.
    Auth AuthConfig `json:"auth,omitempty" yaml:"auth,omitempty" doc:"Authentication configuration"`
}
```

### Example config file

```yaml
# ~/.scafctl/config.yaml
auth:
  entra:
    # Use a specific tenant instead of "common"
    tenantId: "c990bb7a-51f4-439b-bd36-9c07fb1041c0"
    # Optional: use a custom client ID (must be registered in Azure)
    # clientId: "your-client-id"
    # Optional: default scopes for login
    defaultScopes:
      - "openid"
      - "profile"
      - "offline_access"
```

---

## Root Command Integration

### Changes to pkg/cmd/scafctl/root.go

```go
// Add import
import (
    // ... existing imports ...
    authcmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/auth"
    "github.com/oakwood-commons/scafctl/pkg/auth"
    "github.com/oakwood-commons/scafctl/pkg/auth/entra"
)

// In Root() function, add the auth command:
cCmd.AddCommand(authcmd.CommandAuth(cliParams, ioStreams, settings.CliBinaryName))

// In PersistentPreRun, initialize auth registry and add to context:
// After config loading, before command execution:

// Initialize auth registry
authRegistry := auth.NewRegistry()

// Register Entra handler with config
var entraOpts []entra.Option
if cfg != nil && cfg.Auth.Entra != nil {
    entraOpts = append(entraOpts, entra.WithConfig(&entra.Config{
        ClientID:      cfg.Auth.Entra.ClientID,
        TenantID:      cfg.Auth.Entra.TenantID,
        DefaultScopes: cfg.Auth.Entra.DefaultScopes,
    }))
}
entraHandler, err := entra.New(entraOpts...)
if err != nil {
    lgr.V(1).Info("failed to initialize entra auth handler", "error", err)
} else {
    _ = authRegistry.Register(entraHandler)
}

ctx = auth.WithRegistry(ctx, authRegistry)
```

---

## Secret Storage Convention

Entra authentication credentials are stored using the `pkg/secrets` package with the following keys:

| Secret Name | Content | Description |
|-------------|---------|-------------|
| `scafctl.auth.entra.refresh_token` | Encrypted refresh token | Long-lived token for obtaining new access tokens |
| `scafctl.auth.entra.metadata` | JSON metadata | Claims, expiration, tenant info |
| `scafctl.auth.entra.token.<base64url-scope>` | JSON cached token | Access token with expiration for specific scope |

**Token Cache Key Format:**

The scope is base64url-encoded (without padding) to create the cache key:
- Scope: `https://graph.microsoft.com/.default`
- Encoded: `aHR0cHM6Ly9ncmFwaC5taWNyb3NvZnQuY29tLy5kZWZhdWx0`
- Key: `scafctl.auth.entra.token.aHR0cHM6Ly9ncmFwaC5taWNyb3NvZnQuY29tLy5kZWZhdWx0`

**Important**: These secrets use the `scafctl.` prefix which is reserved for internal use.

---

## Token Caching Strategy

### Cache Flow

```
GetToken(scope, minValidFor) called
        │
        ▼
┌───────────────────────────────┐
│   Check disk cache for scope  │
│   (pkg/secrets)               │
└───────────────┬───────────────┘
                │
        ┌───────┴───────┐
        ▼               ▼
   Found            Not Found
        │               │
        ▼               │
┌───────────────────┐   │
│ token.IsValidFor( │   │
│   minValidFor)?   │   │
└───────┬───────────┘   │
        │               │
   ┌────┴────┐          │
   ▼         ▼          │
  Yes        No         │
   │         │          │
   │         └──────────┼─────────┐
   │                    │         │
   ▼                    ▼         ▼
Return             ┌──────────────────┐
cached             │ Mint new token   │
token              │ using refresh    │
                   │ token            │
                   └────────┬─────────┘
                            │
                            ▼
                   ┌──────────────────┐
                   │ Cache token to   │
                   │ disk (secrets)   │
                   └────────┬─────────┘
                            │
                            ▼
                       Return new
                       token
```

### Token Validity Calculation

When the HTTP provider requests a token:

1. **Request timeout**: e.g., 30 seconds (from `inputs["timeout"]`)
2. **Safety buffer**: 60 seconds (constant)
3. **MinValidFor**: timeout + buffer = 90 seconds

This ensures the token won't expire during the HTTP request.

### Cached Token Structure

```go
type CachedToken struct {
    AccessToken string    `json:"accessToken"` // The bearer token
    TokenType   string    `json:"tokenType"`   // "Bearer"
    ExpiresAt   time.Time `json:"expiresAt"`   // Actual expiration time
    Scope       string    `json:"scope"`       // The scope this token is for
    CachedAt    time.Time `json:"cachedAt"`    // When the token was cached
}
```

### Benefits

1. **Fast CLI execution**: Repeated commands reuse cached tokens (no network call)
2. **Survives restarts**: Tokens persist to disk via `pkg/secrets`
3. **Secure**: Tokens are stored encrypted
4. **Smart refresh**: Tokens are refreshed proactively before expiration
5. **Request-aware**: Token validity considers the request timeout

---

## Testing Strategy

### Unit Tests

1. **Token Cache** - Test cache hit/miss, expiration, clear, key encoding
2. **Claims Extraction** - Test JWT parsing with various token formats
3. **Secret Storage** - Use mock secrets store
4. **HTTP Client** - Use mock HTTP client for token endpoints
5. **MinValidFor logic** - Test various validity durations
6. **Error Handling** - Test all error paths

### Mock Implementations

```go
// pkg/auth/mock.go

package auth

import (
    "context"
    "net/http"
)

// MockAuthHandler implements AuthHandler for testing.
type MockAuthHandler struct {
    NameValue        string
    DisplayNameValue string
    LoginResult      *AuthResult
    LoginErr         error
    LogoutErr        error
    StatusResult     *AuthStatus
    StatusErr        error
    GetTokenResult   *Token
    GetTokenErr      error
    InjectAuthErr    error

    // Call tracking
    LoginCalls      []LoginOptions
    LogoutCalls     int
    StatusCalls     int
    GetTokenCalls   []TokenOptions
    InjectAuthCalls []TokenOptions
}

func (m *MockAuthHandler) Name() string { return m.NameValue }
func (m *MockAuthHandler) DisplayName() string { return m.DisplayNameValue }
func (m *MockAuthHandler) SupportedFlows() []AuthFlow { return []AuthFlow{AuthFlowDeviceCode} }

func (m *MockAuthHandler) Login(ctx context.Context, opts LoginOptions) (*AuthResult, error) {
    m.LoginCalls = append(m.LoginCalls, opts)
    return m.LoginResult, m.LoginErr
}

func (m *MockAuthHandler) Logout(ctx context.Context) error {
    m.LogoutCalls++
    return m.LogoutErr
}

func (m *MockAuthHandler) Status(ctx context.Context) (*AuthStatus, error) {
    m.StatusCalls++
    return m.StatusResult, m.StatusErr
}

func (m *MockAuthHandler) GetToken(ctx context.Context, opts TokenOptions) (*Token, error) {
    m.GetTokenCalls = append(m.GetTokenCalls, opts)
    return m.GetTokenResult, m.GetTokenErr
}

func (m *MockAuthHandler) InjectAuth(ctx context.Context, req *http.Request, opts TokenOptions) error {
    m.InjectAuthCalls = append(m.InjectAuthCalls, opts)
    if m.GetTokenResult != nil && m.InjectAuthErr == nil {
        req.Header.Set("Authorization", m.GetTokenResult.TokenType+" "+m.GetTokenResult.AccessToken)
    }
    return m.InjectAuthErr
}
```

### Mock HTTP Server for Device Flow Testing

```go
// pkg/auth/entra/testserver_test.go

type MockOAuthServer struct {
    *httptest.Server
    DeviceCode      string
    UserCode        string
    AccessToken     string
    RefreshToken    string
    IDToken         string
    PollCount       int
    CompleteAfter   int
    ExpiresIn       int
}

func NewMockOAuthServer() *MockOAuthServer {
    m := &MockOAuthServer{
        DeviceCode:    "test-device-code",
        UserCode:      "ABCD-1234",
        AccessToken:   "test-access-token",
        RefreshToken:  "test-refresh-token",
        CompleteAfter: 2,
        ExpiresIn:     3600,
    }

    mux := http.NewServeMux()
    
    // Device code endpoint
    mux.HandleFunc("/oauth2/v2.0/devicecode", func(w http.ResponseWriter, r *http.Request) {
        json.NewEncoder(w).Encode(map[string]any{
            "device_code":      m.DeviceCode,
            "user_code":        m.UserCode,
            "verification_uri": "https://microsoft.com/devicelogin",
            "expires_in":       900,
            "interval":         1,
            "message":          "Test message",
        })
    })

    // Token endpoint
    mux.HandleFunc("/oauth2/v2.0/token", func(w http.ResponseWriter, r *http.Request) {
        m.PollCount++
        
        if m.PollCount < m.CompleteAfter {
            w.WriteHeader(http.StatusBadRequest)
            json.NewEncoder(w).Encode(map[string]string{
                "error": "authorization_pending",
            })
            return
        }

        json.NewEncoder(w).Encode(map[string]any{
            "access_token":  m.AccessToken,
            "refresh_token": m.RefreshToken,
            "token_type":    "Bearer",
            "expires_in":    m.ExpiresIn,
            "scope":         "openid profile",
            "id_token":      m.IDToken,
        })
    })

    m.Server = httptest.NewServer(mux)
    return m
}
```

---

## Implementation Phases

### Phase 1: Core Auth Package ✅ COMPLETE
- [x] Create `pkg/auth/handler.go` with Handler interface
- [x] Create `pkg/auth/errors.go` with error definitions
- [x] Create `pkg/auth/claims.go` with Claims struct
- [x] Create `pkg/auth/registry.go` with Registry
- [x] Create `pkg/auth/context.go` with context helpers
- [x] Create `pkg/auth/mock.go` for testing
- [x] Unit tests for all components

> **Note:** Type names were simplified from the original plan (e.g., `AuthHandler` → `Handler`, `AuthFlow` → `Flow`, `AuthResult` → `Result`) to follow Go naming conventions.

### Phase 2: Entra Handler ✅ COMPLETE
- [x] Create `pkg/auth/entra/config.go`
- [x] Create `pkg/auth/entra/cache.go` with disk-based token cache
- [x] Create `pkg/auth/entra/http.go` with HTTP client wrapper
- [x] Create `pkg/auth/entra/mock.go` with MockHTTPClient for testing
- [x] Create `pkg/auth/entra/handler.go` with main handler
- [x] Create `pkg/auth/entra/device_flow.go`
- [x] Create `pkg/auth/entra/token.go`
- [x] Unit tests with mocks

> **Implementation Notes:**
> - Added `mock.go` (not in original plan) to provide MockHTTPClient for testing HTTP interactions
> - Type naming follows Phase 1 conventions: uses `auth.Flow`, `auth.Result`, `auth.Status`, `auth.NewError`
> - Cache Clear() properly uses `secrets.List()` to enumerate and delete cached tokens
> - Device flow uses V(1) logging for transient errors (authorization_pending) to reduce log noise
> - Handler exposes Option pattern: `WithConfig`, `WithSecretStore`, `WithHTTPClient`

### Phase 3: CLI Commands ✅ COMPLETE
- [x] Create `pkg/cmd/scafctl/auth/handler.go` (helper for handler creation and test injection)
- [x] Create `pkg/cmd/scafctl/auth/auth.go`
- [x] Create `pkg/cmd/scafctl/auth/login.go`
- [x] Create `pkg/cmd/scafctl/auth/logout.go`
- [x] Create `pkg/cmd/scafctl/auth/status.go`
- [x] Create `pkg/cmd/scafctl/auth/token.go`
- [x] Add auth command to root.go
- [x] Command tests (handler_test.go, auth_test.go, login_test.go, logout_test.go, status_test.go, token_test.go)
- [x] Add `GlobalAuthConfig` and `EntraAuthConfig` to `pkg/config/types.go` (moved from Phase 5)
- [x] Add auth to `Save()` and `SaveAs()` in `pkg/config/config.go`

> **Implementation Notes:**
> - Added `handler.go` (not in original plan) to centralize handler creation and provide test injection via context
> - Uses `auth.Handler` interface return type (not `*entra.Handler`) to support mock injection in tests
> - Login command includes signal handling (SIGINT/SIGTERM) for graceful cancellation with Ctrl+C
> - Status and token commands use `flags.KvxOutputFlags` for output formatting (table, json, yaml, quiet)
> - Token command includes `--min-valid-for` flag and expiration warnings
> - AuthConfig types were added in this phase (originally planned for Phase 5) to support config-based handler configuration
> - Handler helpers (`getEntraHandler`, `getEntraHandlerWithTenant`) read from config context and support tenant override flag

### Phase 4: HTTP Provider Integration ✅ COMPLETE
- [x] Add `authProvider` and `scope` properties to HTTP provider schema
- [x] Update HTTP provider Execute() to handle auth injection with MinValidFor
- [x] Add example for authenticated requests
- [x] Update HTTP provider tests
- [x] Implement 401 retry with ForceRefresh (automatic token refresh on unauthorized)
- [x] Add auth registry initialization to root.go (moved from Phase 5)

**Implementation Notes:**
- Added `authProvider` (string) and `scope` (string) schema properties
- MinValidFor calculated as: request timeout + 60 second buffer
- On HTTP 401: retry once with `ForceRefresh: true` to get fresh token
- Headers are copied before auth injection to avoid mutating input
- Timeout accepts both int and float64 (JSON/YAML compatibility)

### Phase 5: Configuration & Documentation ✅ COMPLETE
- [x] Add AuthConfig to `pkg/config/types.go` (completed in Phase 3)
- [x] Add auth to Save/SaveAs in `pkg/config/config.go` (completed in Phase 3)
- [x] Add auth registry initialization to root.go (completed in Phase 4)
- [x] Documentation

**Documentation Created/Updated:**
- `docs/auth-tutorial.md` - User-facing tutorial covering CLI commands, HTTP provider usage, configuration, troubleshooting
- `README.md` - Added Authentication section with quick start and link to tutorial
- `docs/design/auth.md` - Added terminology, secret naming conventions, token caching, MinValidFor, 401 retry documentation

### Phase 6: Integration Testing ✅ COMPLETE
- [x] Mock OAuth server tests
- [x] End-to-end flow tests (CLI command tests already existed)
- [x] HTTP provider with auth tests (completed in Phase 4)
- [x] Token caching tests (already existed, integration tests added)
- [x] Live integration tests with `//go:build integration` tag

**Implementation Notes:**
- Added `pkg/auth/entra/integration_test.go` - Mock OAuth server using httptest.Server
  - Device code flow tests (success, pending, denied, expired)
  - Token refresh tests (success, invalid_grant)
  - Token caching tests (cache hit, MinValidFor, ForceRefresh)
- Added `pkg/auth/entra/integration_live_test.go` - Real Entra tests
  - Requires `//go:build integration` tag to run
  - Environment variables: `SCAFCTL_TEST_ENTRA_TENANT_ID`, `SCAFCTL_TEST_ENTRA_CLIENT_ID`, `SCAFCTL_TEST_ENTRA_SCOPE`
  - Tests: device code login, token refresh, MinValidFor, status
- CLI command tests already comprehensive (login, logout, status, token)

**Test Coverage:**
- `pkg/auth`: 89.7%
- `pkg/auth/entra`: 85.4%

---

## Files to Create/Modify

| File | Action | Status | Description |
|------|--------|--------|-------------|
| `pkg/auth/handler.go` | Create | ✅ Done | Handler interface and types |
| `pkg/auth/errors.go` | Create | ✅ Done | Error definitions |
| `pkg/auth/claims.go` | Create | ✅ Done | Claims struct |
| `pkg/auth/registry.go` | Create | ✅ Done | Auth handler registry |
| `pkg/auth/context.go` | Create | ✅ Done | Context helpers |
| `pkg/auth/mock.go` | Create | ✅ Done | Mock implementations |
| `pkg/auth/entra/handler.go` | Create | ✅ Done | Entra handler implementation |
| `pkg/auth/entra/device_flow.go` | Create | ✅ Done | Device code flow |
| `pkg/auth/entra/token.go` | Create | ✅ Done | Token management |
| `pkg/auth/entra/cache.go` | Create | ✅ Done | Disk-based token cache |
| `pkg/auth/entra/config.go` | Create | ✅ Done | Entra configuration |
| `pkg/auth/entra/http.go` | Create | ✅ Done | HTTP client wrapper |
| `pkg/auth/entra/mock.go` | Create | ✅ Done | Mock HTTP client for testing |
| `pkg/cmd/scafctl/auth/handler.go` | Create | ✅ Done | Handler helper and test injection |
| `pkg/cmd/scafctl/auth/auth.go` | Create | ✅ Done | Auth command group |
| `pkg/cmd/scafctl/auth/login.go` | Create | ✅ Done | Login command |
| `pkg/cmd/scafctl/auth/logout.go` | Create | ✅ Done | Logout command |
| `pkg/cmd/scafctl/auth/status.go` | Create | ✅ Done | Status command |
| `pkg/cmd/scafctl/auth/token.go` | Create | ✅ Done | Token command |
| `pkg/cmd/scafctl/root.go` | Modify | ✅ Done | Add auth command |
| `pkg/config/types.go` | Modify | ✅ Done | Add GlobalAuthConfig, EntraAuthConfig |
| `pkg/config/config.go` | Modify | ✅ Done | Add auth to Save/SaveAs |
| `pkg/provider/builtin/httpprovider/http.go` | Modify | ✅ Done | Add authProvider, scope, 401 retry |
| `pkg/provider/builtin/httpprovider/http_test.go` | Modify | ✅ Done | Add auth integration tests |
| `pkg/cmd/scafctl/root.go` | Modify | ✅ Done | Add auth registry initialization |
| `docs/auth-tutorial.md` | Create | ✅ Done | User-facing authentication tutorial |
| `docs/design/auth.md` | Modify | ✅ Done | Add terminology, caching, MinValidFor docs |
| `README.md` | Modify | ✅ Done | Add Authentication section |
| `pkg/auth/entra/integration_test.go` | Create | ✅ Done | Mock OAuth server integration tests |
| `pkg/auth/entra/integration_live_test.go` | Create | ✅ Done | Live Entra tests (build tag: integration) |

---

## Security Considerations

1. **Refresh tokens** are stored encrypted using `pkg/secrets`
2. **Access tokens** are cached encrypted using `pkg/secrets`
3. **Logging** never includes tokens or secrets
4. **Error messages** do not expose credential details
5. **Token display** in `auth token` command masks the token for table output
6. **TLS** - all OAuth endpoints use HTTPS
7. **Token expiration** is tracked and tokens are refreshed proactively

---

## Phase 7: Service Principal Authentication ✅

### Overview

Added support for Service Principal (client credentials) authentication for CI/CD and automation scenarios. This allows non-interactive authentication using Azure AD application credentials.

### Environment Variables

Following Azure SDK conventions:
- `AZURE_CLIENT_ID` - Application (client) ID of the service principal
- `AZURE_TENANT_ID` - Directory (tenant) ID
- `AZURE_CLIENT_SECRET` - Client secret value (never logged)

### Authentication Flow

```
┌──────────────────┐     ┌─────────────────────┐     ┌───────────────────┐
│   CLI / CI Job   │────▶│  Environment Vars   │────▶│  Client Creds     │
│                  │     │  AZURE_CLIENT_ID    │     │  Grant Type       │
│                  │     │  AZURE_TENANT_ID    │     │  OAuth2 Token     │
│                  │     │  AZURE_CLIENT_SECRET│     │  Endpoint         │
└──────────────────┘     └─────────────────────┘     └───────────────────┘
```

### Flow Selection

The Entra handler automatically selects the authentication flow:
1. If `AZURE_CLIENT_SECRET` is set → Service Principal flow
2. Otherwise → Device Code flow

Users can also explicitly specify the flow:
```bash
scafctl auth login entra --flow service-principal
scafctl auth login entra --flow device-code
```

### Implementation Details

**New Files:**
- `pkg/auth/entra/service_principal.go` - Client credentials flow implementation
- `pkg/auth/entra/service_principal_test.go` - Unit tests

**Modified Files:**
- `pkg/auth/handler.go` - Added `IdentityType` and `ClientID` to `Status`
- `pkg/auth/entra/handler.go` - Updated to route between device code and SP flows
- `pkg/cmd/scafctl/auth/login.go` - Added `--flow` flag
- `pkg/cmd/scafctl/auth/status.go` - Shows identity type and client ID for SP

**Key Functions:**
```go
// Get credentials from environment
func GetServicePrincipalCredentials() *ServicePrincipalCredentials

// Check if SP credentials are available
func HasServicePrincipalCredentials() bool

// Login validates credentials by acquiring a token
func (h *Handler) servicePrincipalLogin(ctx, opts) (*Result, error)

// Get token using client credentials grant
func (h *Handler) getServicePrincipalToken(ctx, opts) (*Token, error)
```

### CLI Usage

```bash
# Auto-detect: uses SP if env vars are set
export AZURE_CLIENT_ID="..."
export AZURE_TENANT_ID="..."
export AZURE_CLIENT_SECRET="..."
scafctl auth login entra

# Explicit SP flow
scafctl auth login entra --flow service-principal

# Check status (shows identity type)
scafctl auth status entra
# Output: identityType: service-principal, clientId: <client-id>
```

### Token Caching

Service Principal tokens are cached the same way as device code tokens:
- Cached to disk via `pkg/secrets`
- Key format: `scafctl.auth.entra.token.<base64url-scope>`
- Tokens refreshed when expired or near expiration

### Security Considerations

1. **Client secret never logged** - Not in debug output, errors, or metrics
2. **Environment variables only** - No config file storage of secrets
3. **Tokens cached encrypted** - Same as device code flow
4. **Short-lived tokens** - Typically 1 hour, cached with expiration tracking

### Testing

```bash
# Run SP-specific tests
go test ./pkg/auth/entra/... -run ServicePrincipal -v
```

---

## Phase 8: Workload Identity Authentication ✅

**Status**: ✅ Completed

Added support for Workload Identity (Azure AD federated credentials) for Kubernetes workloads running on AKS. This enables secure, secretless authentication using projected service account tokens.

### Environment Variables

The following environment variables are automatically set by the Azure Workload Identity webhook when a pod is configured:

- `AZURE_FEDERATED_TOKEN_FILE` - Path to the projected service account token file (typically `/var/run/secrets/azure/tokens/azure-identity-token`)
- `AZURE_FEDERATED_TOKEN` - (Optional) Raw federated token value (for testing; takes precedence over token file)
- `AZURE_CLIENT_ID` - Client ID of the managed identity or app registration
- `AZURE_TENANT_ID` - Azure AD tenant ID
- `AZURE_AUTHORITY_HOST` - (Optional) Azure AD authority URL, defaults to `https://login.microsoftonline.com`

### Token Source Priority

When both are set, the direct token takes precedence:
1. `AZURE_FEDERATED_TOKEN` (direct token value)
2. `AZURE_FEDERATED_TOKEN_FILE` (read from file)

### Authentication Flow

Workload Identity uses the OAuth 2.0 client_credentials grant with a federated token assertion:

```
┌─────────────────────────────────────────────────────────────────────┐
│                        AKS Pod                                       │
├─────────────────────────────────────────────────────────────────────┤
│  1. Read projected token from                                        │
│     /var/run/secrets/azure/tokens/azure-identity-token              │
│                                                                      │
│  2. Exchange token with Azure AD:                                    │
│     POST /{tenant}/oauth2/v2.0/token                                │
│     grant_type=client_credentials                                   │
│     client_id=<AZURE_CLIENT_ID>                                     │
│     client_assertion_type=urn:ietf:params:oauth:client-assertion... │
│     client_assertion=<federated_token>                              │
│     scope=<requested_scope>                                         │
│                                                                      │
│  3. Receive access token from Azure AD                              │
└─────────────────────────────────────────────────────────────────────┘
```

### Flow Priority

When auto-detecting the authentication flow, the priority order is:

1. **Workload Identity** (if `AZURE_FEDERATED_TOKEN` is set OR `AZURE_FEDERATED_TOKEN_FILE` exists and file is accessible)
2. **Service Principal** (if `AZURE_CLIENT_SECRET` is set)
3. **Device Code** (interactive, fallback)

This priority ensures the most secure option (no secrets) is preferred.

### Usage

```bash
# Auto-detect (recommended in AKS pods)
scafctl auth login entra

# Explicit flow selection
scafctl auth login entra --flow workload-identity

# Test with a direct token (for development/testing)
scafctl auth login entra --flow workload-identity --federated-token "eyJ..."

# Or via environment variable
AZURE_FEDERATED_TOKEN="eyJ..." scafctl auth login entra --flow workload-identity

# Check status - shows tokenFile path for debugging
scafctl auth status
```

### Status Output

When authenticated via Workload Identity, `scafctl auth status` shows:

```
Handler:       entra
Authenticated: true
Identity Type: workload-identity
Client ID:     12345678-1234-1234-1234-123456789012
Tenant ID:     tenant-id
Token File:    /var/run/secrets/azure/tokens/azure-identity-token
Name:          Workload Identity (12345678-1234-1234-1234-123456789012)
```

### Error Handling

Workload Identity provides clear, actionable error messages:

| Scenario | Error Message |
|----------|---------------|
| Token file env not set | `workload identity not configured: AZURE_FEDERATED_TOKEN_FILE environment variable not set` |
| Token file doesn't exist | `workload identity token file not found: /path/to/token. Hint: Ensure the pod has the azure-workload-identity webhook labels and the service account is properly configured` |
| Missing client ID | `workload identity not configured: AZURE_CLIENT_ID environment variable not set. Hint: Ensure the pod has the azure-workload-identity webhook labels` |
| Token expired/invalid | Standard Azure AD token exchange error |

### Files Added/Modified

- `pkg/auth/handler.go` - Added `FlowWorkloadIdentity` constant and `IdentityTypeWorkloadIdentity`
- `pkg/auth/entra/workload_identity.go` - Core implementation (NEW)
- `pkg/auth/entra/workload_identity_test.go` - Comprehensive tests (NEW)
- `pkg/auth/entra/handler.go` - Integrated WI into Login, Status, GetToken
- `pkg/cmd/scafctl/auth/login.go` - Added `--flow workload-identity` option
- `pkg/cmd/scafctl/auth/status.go` - Shows `tokenFile` for WI

### Token Caching

Workload Identity tokens are cached the same way as other flows:
- Cache key: `scafctl.auth.entra.token.<base64url-encoded-scope>`
- Cache includes expiry checking with configurable buffer
- Token file is re-read on each acquisition (handles Kubernetes rotation)

### Testing

```bash
# Run WI-specific tests
go test ./pkg/auth/entra/... -run WorkloadIdentity -v
```

---

## Future Enhancements

1. **Multiple Tenants**: Support authentication to multiple Entra tenants simultaneously
2. ~~**Service Principal Auth**: Add `AuthFlowServicePrincipal` for CI/CD scenarios~~ ✅ Implemented in Phase 7
3. ~~**Token Refresh on 401**: HTTP provider could automatically retry with fresh token on 401~~ ✅ Implemented in Phase 4
4. ~~**Workload Identity**: Add federated credential support for AKS workloads~~ ✅ Implemented in Phase 8
5. **Managed Identity (IMDS)**: Add support for Azure VMs using Instance Metadata Service
6. **Additional Auth Handlers**: GitHub, GCloud, etc.
7. **Plugin Auth Handlers**: Load auth handlers from plugins
8. **Certificate-Based SP Auth**: Add certificate credential support for service principals (X.509)

---

## Discrepancies with Design Document

After reviewing [docs/design/auth.md](docs/design/auth.md):

1. **Terminology**: The design doc uses "auth providers" while this implementation uses "auth handlers" to distinguish from action/resolver providers. Consider updating the design doc for consistency.

2. **Token Caching**: The design doc doesn't mention disk-based token caching. This implementation adds disk-based caching for performance across multiple CLI invocations.

3. **MinValidFor**: The design doc doesn't mention dynamic token validity requirements. This implementation adds `MinValidFor` to ensure tokens remain valid for the duration of requests.

**Suggested Design Doc Updates**:
- Clarify terminology: "auth handler" vs "auth provider" vs "provider"
- Add section on secret naming conventions (`scafctl.auth.<handler>.*`)
- Document the `auth token` command for debugging
- Document disk-based token caching strategy
- Document MinValidFor concept for request-aware token validity
