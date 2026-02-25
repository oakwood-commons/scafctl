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
)

// DefaultMinValidFor is the default minimum validity duration for tokens.
const DefaultMinValidFor = 60 * time.Second

// LoginOptions configures the login process.
type LoginOptions struct {
	TenantID           string
	Scopes             []string
	Flow               Flow
	Timeout            time.Duration
	CallbackPort       int
	DeviceCodeCallback func(userCode, verificationURI, message string)
}

// TokenOptions configures token acquisition.
type TokenOptions struct {
	Scope        string
	MinValidFor  time.Duration
	ForceRefresh bool
}

// Result contains the result of a successful authentication.
type Result struct {
	Claims    *Claims
	ExpiresAt time.Time
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
	Authenticated bool
	Claims        *Claims
	ExpiresAt     time.Time
	LastRefresh   time.Time
	TenantID      string
	IdentityType  IdentityType // Type of identity: "user", "service-principal", or "workload-identity"
	ClientID      string       // For service principal/workload identity: the application ID
	TokenFile     string       // For workload identity: path to the federated token file
	Scopes        []string     // Scopes granted during login
}

// Token represents a short-lived access token.
type Token struct {
	AccessToken string //nolint:gosec // G117: not a hardcoded credential, stores runtime token data
	TokenType   string
	ExpiresAt   time.Time
	Scope       string
	// CachedAt records when this token was written to the on-disk cache.
	// Zero value means the token was not loaded from the cache.
	CachedAt time.Time
	// Flow is the authentication flow that produced this token (e.g. "device_code",
	// "service_principal", "workload_identity").  Empty string means unknown.
	Flow Flow
	// SessionID is a stable identifier linking this access token back to the
	// authentication session (login) that produced it.  Generated once at login
	// time and preserved across refresh-token rotations.
	SessionID string
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
	if t == nil || t.ExpiresAt.IsZero() {
		return 0
	}
	remaining := time.Until(t.ExpiresAt)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// CachedTokenInfo holds display metadata for a cached token (refresh or access).
// The actual token value is intentionally omitted — use 'auth token' to retrieve it.
type CachedTokenInfo struct {
	// Handler is the name of the auth handler that owns this token.
	Handler string `json:"handler"`
	// TokenKind is either "refresh" or "access".
	TokenKind string `json:"tokenKind"`
	// Scope is the OAuth scope associated with the token.
	// Empty for refresh tokens that were not scope-specific.
	Scope string `json:"scope,omitempty"`
	// TokenType is the token type, e.g. "Bearer".
	TokenType string `json:"tokenType,omitempty"`
	// Flow is the authentication flow that produced the token.
	Flow Flow `json:"flow,omitempty"`
	// ExpiresAt is when the token expires.
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
	// CachedAt is when the token was written to the on-disk cache.
	CachedAt time.Time `json:"cachedAt,omitempty"`
	// IsExpired indicates whether the token is past its expiry time.
	IsExpired bool `json:"isExpired"`
	// SessionID is the stable identifier of the authentication session that
	// produced this token.  Present on both refresh and access entries, allowing
	// callers to trace which login session minted a given access token.
	SessionID string `json:"sessionId,omitempty"`
}

// TimeUntilExpiry returns the duration until this cached token expires.
// Returns 0 if the token is already expired or has no expiry set.
func (c *CachedTokenInfo) TimeUntilExpiry() time.Duration {
	if c.ExpiresAt.IsZero() {
		return 0
	}
	remaining := time.Until(c.ExpiresAt)
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
