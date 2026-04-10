// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package auth provides authentication handler interfaces and utilities for scafctl.
// Auth handlers manage identity verification, credential storage, and token acquisition.
// They are separate from providers - providers are stateless execution primitives,
// while auth handlers manage state (cached tokens, refresh tokens) across invocations.
package auth

import (
	"context"
	"net/http"
	"time"
)

// Handler defines the interface for authentication handlers.
// Auth handlers manage identity verification, credential storage, and token acquisition.
type Handler interface {
	// Name returns the unique identifier for this auth handler.
	Name() string

	// DisplayName returns a human-readable name for display purposes.
	DisplayName() string

	// Login initiates the authentication flow.
	// For interactive flows (like device code), this blocks until completion or timeout.
	// Returns the authenticated identity claims on success.
	Login(ctx context.Context, opts LoginOptions) (*Result, error)

	// Logout clears stored credentials for this handler.
	Logout(ctx context.Context) error

	// Status returns the current authentication status.
	// Returns a status with Authenticated=false if not logged in.
	Status(ctx context.Context) (*Status, error)

	// GetToken returns a valid access token for the specified options.
	// Uses cached tokens when available and valid for the requested duration,
	// otherwise refreshes from the identity provider.
	// Returns ErrNotAuthenticated if user is not logged in.
	// Returns ErrTokenExpired if refresh token has expired (re-login required).
	GetToken(ctx context.Context, opts TokenOptions) (*Token, error)

	// InjectAuth adds authentication to an HTTP request.
	// This is the primary method used by providers (like HTTP) to authenticate requests.
	// Automatically handles token acquisition/refresh as needed.
	InjectAuth(ctx context.Context, req *http.Request, opts TokenOptions) error

	// SupportedFlows returns the authentication flows this handler supports.
	SupportedFlows() []Flow

	// Capabilities returns the set of capabilities this handler supports.
	// Commands use capabilities to dynamically determine which flags and
	// validation rules apply for a given handler.
	Capabilities() []Capability
}

// Flow represents an authentication flow type.
type Flow string

const (
	// FlowDeviceCode is the OAuth 2.0 device authorization flow.
	FlowDeviceCode Flow = "device_code"

	// FlowInteractive is an interactive browser-based flow.
	FlowInteractive Flow = "interactive"

	// FlowServicePrincipal is authentication using service principal credentials.
	FlowServicePrincipal Flow = "service_principal"

	// FlowWorkloadIdentity is authentication using Azure Workload Identity (Kubernetes).
	FlowWorkloadIdentity Flow = "workload_identity"

	// FlowPAT is authentication using a personal access token from environment variables.
	FlowPAT Flow = "pat"

	// FlowMetadata is authentication using cloud metadata server (e.g., GCE, Cloud Run).
	FlowMetadata Flow = "metadata"

	// FlowGcloudADC is authentication using gcloud's Application Default Credentials file.
	FlowGcloudADC Flow = "gcloud_adc"

	// FlowGitHubApp is authentication using a GitHub App installation token.
	// A JWT is minted from the App's private key and exchanged for a short-lived
	// installation access token.
	FlowGitHubApp Flow = "github_app"

	// FlowClientCredentials is OAuth 2.0 client credentials grant (RFC 6749 §4.4).
	// Non-interactive, uses client_id + client_secret.
	FlowClientCredentials Flow = "client_credentials"
)

// DefaultMinValidFor is the default minimum validity duration for tokens.
const DefaultMinValidFor = 60 * time.Second

// LoginOptions configures the login process.
type LoginOptions struct {
	TenantID           string                                          `json:"tenantId,omitempty" yaml:"tenantId,omitempty" doc:"Azure AD tenant ID override" maxLength:"128"`
	Scopes             []string                                        `json:"scopes,omitempty" yaml:"scopes,omitempty" doc:"OAuth scopes to request" maxItems:"20"`
	Flow               Flow                                            `json:"flow,omitempty" yaml:"flow,omitempty" doc:"Authentication flow to use" example:"device_code" maxLength:"64"`
	Timeout            time.Duration                                   `json:"timeout,omitempty" yaml:"timeout,omitempty" doc:"Maximum time to wait for authentication"`
	CallbackPort       int                                             `json:"callbackPort,omitempty" yaml:"callbackPort,omitempty" doc:"Local port for OAuth callback server" minimum:"0" maximum:"65535"`
	DeviceCodeCallback func(userCode, verificationURI, message string) `json:"-" yaml:"-"`
}

// TokenOptions configures token acquisition.
type TokenOptions struct {
	Scope        string        `json:"scope,omitempty" yaml:"scope,omitempty" doc:"OAuth scope for the token request" example:"https://graph.microsoft.com/.default" maxLength:"1024"`
	MinValidFor  time.Duration `json:"minValidFor,omitempty" yaml:"minValidFor,omitempty" doc:"Minimum remaining validity for cached tokens"`
	ForceRefresh bool          `json:"forceRefresh,omitempty" yaml:"forceRefresh,omitempty" doc:"Force token refresh even if cached token is valid"`
}

// Result contains the result of a successful authentication.
type Result struct {
	Claims    *Claims   `json:"claims,omitempty" yaml:"claims,omitempty" doc:"Identity claims from authentication"`
	ExpiresAt time.Time `json:"expiresAt,omitempty" yaml:"expiresAt,omitempty" doc:"Time the authentication expires"`
}

// IdentityType represents the type of authenticated identity.
type IdentityType string

const (
	// IdentityTypeUser represents a user identity (e.g., device code flow).
	IdentityTypeUser IdentityType = "user"
	// IdentityTypeServicePrincipal represents a service principal identity.
	IdentityTypeServicePrincipal IdentityType = "service-principal"
	// IdentityTypeWorkloadIdentity represents a workload identity (Kubernetes federated).
	IdentityTypeWorkloadIdentity IdentityType = "workload-identity"
)

// Status represents the current authentication state.
type Status struct {
	Authenticated bool         `json:"authenticated" yaml:"authenticated" doc:"Whether the user is currently authenticated"`
	Reason        string       `json:"reason,omitempty" yaml:"reason,omitempty" doc:"Why the handler is not authenticated (empty when authenticated)" maxLength:"256"`
	Claims        *Claims      `json:"claims,omitempty" yaml:"claims,omitempty" doc:"Identity claims from the current session"`
	ExpiresAt     time.Time    `json:"expiresAt,omitempty" yaml:"expiresAt,omitempty" doc:"Time the authentication expires"`
	LastRefresh   time.Time    `json:"lastRefresh,omitempty" yaml:"lastRefresh,omitempty" doc:"Time the token was last refreshed"`
	TenantID      string       `json:"tenantId,omitempty" yaml:"tenantId,omitempty" doc:"Azure AD tenant ID" maxLength:"128"`
	IdentityType  IdentityType `json:"identityType,omitempty" yaml:"identityType,omitempty" doc:"Type of authenticated identity" example:"user" maxLength:"64"`
	ClientID      string       `json:"clientId,omitempty" yaml:"clientId,omitempty" doc:"Application ID for service principal or workload identity" maxLength:"128"`
	TokenFile     string       `json:"tokenFile,omitempty" yaml:"tokenFile,omitempty" doc:"Path to the federated token file for workload identity" maxLength:"1024"`
	Scopes        []string     `json:"scopes,omitempty" yaml:"scopes,omitempty" doc:"Scopes granted during login" maxItems:"20"`
}

// Token represents a short-lived access token.
type Token struct {
	AccessToken string    `json:"accessToken" yaml:"accessToken" doc:"The access token value"` //nolint:gosec // G117: not a hardcoded credential, stores runtime token data
	TokenType   string    `json:"tokenType" yaml:"tokenType" doc:"Token type, typically Bearer" example:"Bearer" maxLength:"64"`
	ExpiresAt   time.Time `json:"expiresAt" yaml:"expiresAt" doc:"Time the token expires"`
	Scope       string    `json:"scope,omitempty" yaml:"scope,omitempty" doc:"OAuth scope the token was issued for" maxLength:"1024"`
	// CachedAt records when this token was written to the on-disk cache.
	// Zero value means the token was not loaded from the cache.
	CachedAt time.Time `json:"cachedAt,omitempty" yaml:"cachedAt,omitempty" doc:"Time the token was written to the on-disk cache"`
	// Flow is the authentication flow that produced this token (e.g. "device_code",
	// "service_principal", "workload_identity").  Empty string means unknown.
	Flow Flow `json:"flow,omitempty" yaml:"flow,omitempty" doc:"Authentication flow that produced this token" example:"device_code" maxLength:"64"`
	// SessionID is a stable identifier linking this access token back to the
	// authentication session (login) that produced it.  Generated once at login
	// time and preserved across refresh-token rotations.
	SessionID string `json:"sessionId,omitempty" yaml:"sessionId,omitempty" doc:"Stable identifier of the authentication session" maxLength:"128"`
}

// IsValidFor returns true if the token will be valid for at least the specified duration.
func (t *Token) IsValidFor(duration time.Duration) bool {
	if t == nil || t.AccessToken == "" || t.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().Add(duration).Before(t.ExpiresAt)
}

// IsExpired returns true if the token has expired.
func (t *Token) IsExpired() bool {
	return !t.IsValidFor(0)
}

// TimeUntilExpiry returns the duration until the token expires.
// Returns 0 if the token is already expired or invalid.
func (t *Token) TimeUntilExpiry() time.Duration {
	if t == nil {
		return 0
	}
	return timeUntilExpiry(t.ExpiresAt)
}

// CachedTokenInfo holds display metadata for a cached token (refresh or access).
// The actual token value is intentionally omitted — use 'auth token' to retrieve it.
type CachedTokenInfo struct {
	// Handler is the name of the auth handler that owns this token.
	Handler string `json:"handler" yaml:"handler" doc:"Name of the auth handler that owns this token" example:"entra" maxLength:"64"`
	// TokenKind is either "refresh" or "access".
	TokenKind string `json:"tokenKind" yaml:"tokenKind" doc:"Token kind: refresh or access" example:"access" maxLength:"16"`
	// Scope is the OAuth scope associated with the token.
	// Empty for refresh tokens that were not scope-specific.
	Scope string `json:"scope,omitempty" yaml:"scope,omitempty" doc:"OAuth scope associated with the token" maxLength:"1024"`
	// TokenType is the token type, e.g. "Bearer".
	TokenType string `json:"tokenType,omitempty" yaml:"tokenType,omitempty" doc:"Token type, typically Bearer" example:"Bearer" maxLength:"64"`
	// Flow is the authentication flow that produced the token.
	Flow Flow `json:"flow,omitempty" yaml:"flow,omitempty" doc:"Authentication flow that produced the token" example:"device_code" maxLength:"64"`
	// Fingerprint is the truncated SHA-256 hash of the config identity
	// (e.g., clientID+tenantID) that produced this token. The value "_"
	// means no config-specific partitioning applies.
	Fingerprint string `json:"fingerprint,omitempty" yaml:"fingerprint,omitempty" doc:"Truncated SHA-256 hash of the config identity" maxLength:"128"`
	// ExpiresAt is when the token expires.
	ExpiresAt time.Time `json:"expiresAt,omitempty" yaml:"expiresAt,omitempty" doc:"Time the token expires"`
	// CachedAt is when the token was written to the on-disk cache.
	CachedAt time.Time `json:"cachedAt,omitempty" yaml:"cachedAt,omitempty" doc:"Time the token was written to the on-disk cache"`
	// IsExpired indicates whether the token is past its expiry time.
	IsExpired bool `json:"isExpired" yaml:"isExpired" doc:"Whether the token is past its expiry time"`
	// SessionID is the stable identifier of the authentication session that
	// produced this token.  Present on both refresh and access entries, allowing
	// callers to trace which login session minted a given access token.
	SessionID string `json:"sessionId,omitempty" yaml:"sessionId,omitempty" doc:"Stable identifier of the authentication session" maxLength:"128"`
}

// TimeUntilExpiry returns the duration until this cached token expires.
// Returns 0 if the token is already expired or has no expiry set.
func (c *CachedTokenInfo) TimeUntilExpiry() time.Duration {
	return timeUntilExpiry(c.ExpiresAt)
}

// timeUntilExpiry returns the duration until the given expiry time.
// Returns 0 if the expiry is zero or already past.
func timeUntilExpiry(expiresAt time.Time) time.Duration {
	if expiresAt.IsZero() {
		return 0
	}
	remaining := time.Until(expiresAt)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// TokenLister is an optional interface implemented by auth handlers that can
// enumerate all cached tokens (both refresh and minted access tokens).
type TokenLister interface {
	ListCachedTokens(ctx context.Context) ([]*CachedTokenInfo, error)
}

// TokenPurger is an optional interface implemented by auth handlers that can
// remove expired access tokens from the cache without affecting valid tokens
// or the refresh token.  Returns the number of tokens removed.
type TokenPurger interface {
	PurgeExpiredTokens(ctx context.Context) (int, error)
}
