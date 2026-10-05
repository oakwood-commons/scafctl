package main

// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Command my-auth-handler is a fake auth-handler plugin used by the e2e suites.
// It never talks to a network: Login always succeeds with fixed claims and
// GetToken returns a fixed, non-secret token, so specs can exercise the
// auth-handler plugin lifecycle (packaging, catalog push/pull, discovery)
// without an identity provider. State lives in memory for the life of the
// plugin process only.

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/oakwood-commons/scafctl-plugin-sdk/auth"
	sdkplugin "github.com/oakwood-commons/scafctl-plugin-sdk/plugin"
)

const (
	// handlerName is the auth handler name the plugin advertises.
	handlerName = "test-auth-handler"

	// handlerDisplayName is the human-readable handler name.
	handlerDisplayName = "Test Auth Handler"

	// fakeSubject, fakeEmail and fakeToken are fixed, obviously fake identity
	// values returned for every login and token request.
	fakeSubject = "e2e-user"
	fakeEmail   = "e2e-user@example.invalid"
	fakeToken   = "e2e-fake-access-token" //nolint:gosec // fixed fake value for tests, not a credential

	// tokenTTL is how long fake sessions and tokens remain valid.
	tokenTTL = time.Hour
)

// MyAuthHandler is an in-memory fake implementing sdkplugin.AuthHandlerPlugin.
type MyAuthHandler struct {
	mu        sync.Mutex
	loggedIn  bool
	expiresAt time.Time
}

func (h *MyAuthHandler) GetAuthHandlers(_ context.Context) ([]sdkplugin.AuthHandlerInfo, error) {
	return []sdkplugin.AuthHandlerInfo{{
		Name:         handlerName,
		DisplayName:  handlerDisplayName,
		Flows:        []auth.Flow{auth.FlowPAT},
		Capabilities: []auth.Capability{},
	}}, nil
}

func (h *MyAuthHandler) ConfigureAuthHandler(_ context.Context, name string, _ sdkplugin.ProviderConfig) error {
	return checkHandler(name)
}

func (h *MyAuthHandler) Login(_ context.Context, name string, _ sdkplugin.LoginRequest, _ func(sdkplugin.DeviceCodePrompt)) (*sdkplugin.LoginResponse, error) {
	if err := checkHandler(name); err != nil {
		return nil, err
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	h.loggedIn = true
	h.expiresAt = time.Now().Add(tokenTTL)

	return &sdkplugin.LoginResponse{
		Claims:    fakeClaims(h.expiresAt),
		ExpiresAt: h.expiresAt,
	}, nil
}

func (h *MyAuthHandler) Logout(_ context.Context, name string, _ sdkplugin.LogoutRequest) error {
	if err := checkHandler(name); err != nil {
		return err
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	h.loggedIn = false
	h.expiresAt = time.Time{}
	return nil
}

func (h *MyAuthHandler) GetStatus(_ context.Context, name string, _ sdkplugin.StatusRequest) (*auth.Status, error) {
	if err := checkHandler(name); err != nil {
		return nil, err
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.loggedIn {
		return &auth.Status{Authenticated: false, Reason: "not logged in"}, nil
	}
	return &auth.Status{
		Authenticated: true,
		Claims:        fakeClaims(h.expiresAt),
		ExpiresAt:     h.expiresAt,
		IdentityType:  auth.IdentityTypeUser,
		Flow:          auth.FlowPAT,
	}, nil
}

func (h *MyAuthHandler) GetToken(_ context.Context, name string, req sdkplugin.TokenRequest) (*sdkplugin.TokenResponse, error) {
	if err := checkHandler(name); err != nil {
		return nil, err
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.loggedIn {
		return nil, fmt.Errorf("not logged in")
	}
	return &sdkplugin.TokenResponse{
		AccessToken: fakeToken,
		TokenType:   "Bearer",
		ExpiresAt:   h.expiresAt,
		Scope:       req.Scope,
		CachedAt:    time.Now(),
		Flow:        auth.FlowPAT,
	}, nil
}

func (h *MyAuthHandler) ListCachedTokens(_ context.Context, name string) ([]*auth.CachedTokenInfo, error) {
	if err := checkHandler(name); err != nil {
		return nil, err
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.loggedIn {
		return nil, nil
	}
	return []*auth.CachedTokenInfo{{
		Handler:   handlerName,
		TokenKind: "access",
		TokenType: "Bearer",
		Flow:      auth.FlowPAT,
		ExpiresAt: h.expiresAt,
		CachedAt:  time.Now(),
	}}, nil
}

func (h *MyAuthHandler) PurgeExpiredTokens(_ context.Context, name string) (int, error) {
	if err := checkHandler(name); err != nil {
		return 0, err
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.loggedIn && !h.expiresAt.After(time.Now()) {
		h.loggedIn = false
		h.expiresAt = time.Time{}
		return 1, nil
	}
	return 0, nil
}

func (h *MyAuthHandler) DetectAvailableFlows(_ context.Context, name string) ([]sdkplugin.FlowAvailability, error) {
	if err := checkHandler(name); err != nil {
		return nil, err
	}
	return []sdkplugin.FlowAvailability{{
		Flow:      auth.FlowPAT,
		Available: true,
		Reason:    "fake handler: always available",
	}}, nil
}

func (h *MyAuthHandler) StopAuthHandler(_ context.Context, _ string) error {
	return nil
}

func checkHandler(name string) error {
	if name != handlerName {
		return fmt.Errorf("unknown auth handler: %s", name)
	}
	return nil
}

func fakeClaims(expiresAt time.Time) *auth.Claims {
	return &auth.Claims{
		Issuer:    "https://auth.example.invalid",
		Subject:   fakeSubject,
		Email:     fakeEmail,
		Name:      "E2E User",
		Username:  fakeSubject,
		IssuedAt:  time.Now(),
		ExpiresAt: expiresAt,
	}
}

func main() {
	sdkplugin.ServeAuthHandler(&MyAuthHandler{})
}

var _ sdkplugin.AuthHandlerPlugin = (*MyAuthHandler)(nil)
