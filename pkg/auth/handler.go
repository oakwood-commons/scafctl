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
)

// DefaultMinValidFor is the default minimum validity duration for tokens.
const DefaultMinValidFor = 60 * time.Second

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
