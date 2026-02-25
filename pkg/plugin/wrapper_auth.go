// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/logger"
)

// Compile-time interface checks.
var (
	_ auth.Handler     = (*AuthHandlerWrapper)(nil)
	_ auth.TokenLister = (*AuthHandlerWrapper)(nil)
	_ auth.TokenPurger = (*AuthHandlerWrapper)(nil)
)

// AuthHandlerWrapper wraps a plugin auth handler to implement the auth.Handler
// (and optionally auth.TokenLister / auth.TokenPurger) interfaces.
type AuthHandlerWrapper struct {
	client      *AuthHandlerClient
	handlerName string
	info        AuthHandlerInfo
	mu          sync.RWMutex
}

// NewAuthHandlerWrapper creates a new wrapper for a plugin auth handler.
func NewAuthHandlerWrapper(client *AuthHandlerClient, info AuthHandlerInfo) *AuthHandlerWrapper {
	return &AuthHandlerWrapper{
		client:      client,
		handlerName: info.Name,
		info:        info,
	}
}

// Name implements auth.Handler.
func (w *AuthHandlerWrapper) Name() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.info.Name
}

// DisplayName implements auth.Handler.
func (w *AuthHandlerWrapper) DisplayName() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.info.DisplayName
}

// SupportedFlows implements auth.Handler.
func (w *AuthHandlerWrapper) SupportedFlows() []auth.Flow {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.info.Flows
}

// Capabilities implements auth.Handler.
func (w *AuthHandlerWrapper) Capabilities() []auth.Capability {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.info.Capabilities
}

// Login implements auth.Handler.
func (w *AuthHandlerWrapper) Login(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("login via plugin auth handler", "handler", w.handlerName)

	req := LoginRequest{
		TenantID: opts.TenantID,
		Scopes:   opts.Scopes,
		Flow:     opts.Flow,
		Timeout:  opts.Timeout,
	}

	// Bridge the LoginOptions.DeviceCodeCallback to the plugin's streaming callback.
	var deviceCodeCb func(DeviceCodePrompt)
	if opts.DeviceCodeCallback != nil {
		deviceCodeCb = func(prompt DeviceCodePrompt) {
			opts.DeviceCodeCallback(prompt.UserCode, prompt.VerificationURI, prompt.Message)
		}
	}

	resp, err := w.client.plugin.Login(ctx, w.handlerName, req, deviceCodeCb)
	if err != nil {
		return nil, fmt.Errorf("plugin auth handler %s login: %w", w.handlerName, err)
	}

	return &auth.Result{
		Claims:    resp.Claims,
		ExpiresAt: resp.ExpiresAt,
	}, nil
}

// Logout implements auth.Handler.
func (w *AuthHandlerWrapper) Logout(ctx context.Context) error {
	return w.client.plugin.Logout(ctx, w.handlerName)
}

// Status implements auth.Handler.
func (w *AuthHandlerWrapper) Status(ctx context.Context) (*auth.Status, error) {
	return w.client.plugin.GetStatus(ctx, w.handlerName)
}

// GetToken implements auth.Handler.
func (w *AuthHandlerWrapper) GetToken(ctx context.Context, opts auth.TokenOptions) (*auth.Token, error) {
	req := TokenRequest{
		Scope:        opts.Scope,
		MinValidFor:  opts.MinValidFor,
		ForceRefresh: opts.ForceRefresh,
	}

	resp, err := w.client.plugin.GetToken(ctx, w.handlerName, req)
	if err != nil {
		return nil, fmt.Errorf("plugin auth handler %s get-token: %w", w.handlerName, err)
	}

	return &auth.Token{
		AccessToken: resp.AccessToken,
		TokenType:   resp.TokenType,
		ExpiresAt:   resp.ExpiresAt,
		Scope:       resp.Scope,
		CachedAt:    resp.CachedAt,
		Flow:        resp.Flow,
		SessionID:   resp.SessionID,
	}, nil
}

// InjectAuth implements auth.Handler.
// Since http.Request cannot be serialized over gRPC, this method decomposes into
// GetToken (over gRPC) + local header injection.
func (w *AuthHandlerWrapper) InjectAuth(ctx context.Context, req *http.Request, opts auth.TokenOptions) error {
	token, err := w.GetToken(ctx, opts)
	if err != nil {
		return fmt.Errorf("plugin auth handler %s inject-auth: %w", w.handlerName, err)
	}

	tokenType := token.TokenType
	if tokenType == "" {
		tokenType = "Bearer"
	}
	req.Header.Set("Authorization", tokenType+" "+token.AccessToken)
	return nil
}

// ListCachedTokens implements auth.TokenLister.
func (w *AuthHandlerWrapper) ListCachedTokens(ctx context.Context) ([]*auth.CachedTokenInfo, error) {
	return w.client.plugin.ListCachedTokens(ctx, w.handlerName)
}

// PurgeExpiredTokens implements auth.TokenPurger.
func (w *AuthHandlerWrapper) PurgeExpiredTokens(ctx context.Context) (int, error) {
	return w.client.plugin.PurgeExpiredTokens(ctx, w.handlerName)
}

// Client returns the underlying plugin client.
func (w *AuthHandlerWrapper) Client() *AuthHandlerClient {
	return w.client
}

// RegisterAuthHandlerPlugins discovers auth handler plugins and registers them
// with the auth registry.
func RegisterAuthHandlerPlugins(registry *auth.Registry, pluginDirs []string) error {
	clients, err := DiscoverAuthHandlers(pluginDirs)
	if err != nil {
		return fmt.Errorf("failed to discover auth handler plugins: %w", err)
	}

	for _, client := range clients {
		handlers, err := client.GetAuthHandlers(context.Background())
		if err != nil {
			client.Kill()
			continue
		}

		for _, info := range handlers {
			wrapper := NewAuthHandlerWrapper(client, info)
			if err := registry.Register(wrapper); err != nil {
				continue
			}
		}
	}

	return nil
}
