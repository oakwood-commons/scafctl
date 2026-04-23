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

	sdkauth "github.com/oakwood-commons/scafctl-plugin-sdk/auth"
)

// Handler defines the interface for authentication handlers.
// Auth handlers manage identity verification, credential storage, and token acquisition.
// This interface is host-only and not part of the SDK because it references net/http.
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

// --- SDK type aliases ---

// Flow represents an authentication flow type.
type Flow = sdkauth.Flow

const (
	FlowDeviceCode        = sdkauth.FlowDeviceCode
	FlowInteractive       = sdkauth.FlowInteractive
	FlowServicePrincipal  = sdkauth.FlowServicePrincipal
	FlowWorkloadIdentity  = sdkauth.FlowWorkloadIdentity
	FlowPAT               = sdkauth.FlowPAT
	FlowMetadata          = sdkauth.FlowMetadata
	FlowGcloudADC         = sdkauth.FlowGcloudADC
	FlowGitHubApp         = sdkauth.FlowGitHubApp
	FlowClientCredentials = sdkauth.FlowClientCredentials
)

// DefaultMinValidFor is the default minimum validity duration for tokens.
const DefaultMinValidFor = sdkauth.DefaultMinValidFor

// LoginOptions configures the login process.
type LoginOptions = sdkauth.LoginOptions

// TokenOptions configures token acquisition.
type TokenOptions = sdkauth.TokenOptions

// Result contains the result of a successful authentication.
type Result = sdkauth.Result

// IdentityType represents the type of authenticated identity.
type IdentityType = sdkauth.IdentityType

const (
	IdentityTypeUser             = sdkauth.IdentityTypeUser
	IdentityTypeServicePrincipal = sdkauth.IdentityTypeServicePrincipal
	IdentityTypeWorkloadIdentity = sdkauth.IdentityTypeWorkloadIdentity
)

// Status represents the current authentication state.
type Status = sdkauth.Status

// Token represents a short-lived access token.
type Token = sdkauth.Token

// CachedTokenInfo holds display metadata for a cached token.
type CachedTokenInfo = sdkauth.CachedTokenInfo

// TokenLister is an optional interface for auth handlers that can enumerate cached tokens.
type TokenLister = sdkauth.TokenLister

// TokenPurger is an optional interface for auth handlers that can remove expired tokens.
type TokenPurger = sdkauth.TokenPurger

// FlowReporter is an optional interface for auth handlers that can report
// the credential source currently being used (e.g. device-code, gcloud-adc,
// workload-identity). The status command uses this to display the active flow.
type FlowReporter interface {
	// ActiveFlow returns the authentication flow currently in use.
	// Returns an empty string if the flow cannot be determined.
	ActiveFlow(ctx context.Context) Flow
}
