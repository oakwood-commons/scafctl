// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	authofficial "github.com/oakwood-commons/scafctl/pkg/auth/official"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/logger"
)

// Compile-time interface checks.
var (
	_ auth.Handler      = (*AuthHandlerWrapper)(nil)
	_ auth.TokenLister  = (*AuthHandlerWrapper)(nil)
	_ auth.TokenPurger  = (*AuthHandlerWrapper)(nil)
	_ auth.FlowDetector = (*AuthHandlerWrapper)(nil)
)

// AuthHandlerWrapper wraps a plugin auth handler to implement the auth.Handler
// (and optionally auth.TokenLister / auth.TokenPurger) interfaces.
type AuthHandlerWrapper struct {
	client         *AuthHandlerClient
	handlerName    string
	info           AuthHandlerInfo
	trustedDomains []string
	mu             sync.RWMutex
}

// NewAuthHandlerWrapper creates a new wrapper for a plugin auth handler.
func NewAuthHandlerWrapper(client *AuthHandlerClient, info AuthHandlerInfo) *AuthHandlerWrapper {
	return &AuthHandlerWrapper{
		client:      client,
		handlerName: info.Name,
		info:        info,
	}
}

// SetTrustedDomains sets the trusted verification URI domains for device code
// URL validation. When non-empty, device code prompts from this handler are
// validated against these domains (exact match or subdomain).
func (w *AuthHandlerWrapper) SetTrustedDomains(domains []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if domains == nil {
		w.trustedDomains = nil
		return
	}
	w.trustedDomains = make([]string, len(domains))
	copy(w.trustedDomains, domains)
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
	// Includes host-side verification URI validation to prevent phishing via
	// malicious plugins sending fake device code URLs.
	loginCtx, cancelCause := context.WithCancelCause(ctx)
	defer cancelCause(nil)

	// Snapshot trusted domains under lock to avoid races with SetTrustedDomains.
	w.mu.RLock()
	trustedSnapshot := make([]string, len(w.trustedDomains))
	copy(trustedSnapshot, w.trustedDomains)
	w.mu.RUnlock()

	var deviceCodeCb func(DeviceCodePrompt)
	if opts.DeviceCodeCallback != nil {
		deviceCodeCb = func(prompt DeviceCodePrompt) {
			if err := ValidateVerificationURI(prompt.VerificationURI, trustedSnapshot); err != nil {
				lgr.Error(err, "device code prompt blocked -- terminating login",
					"handler", w.handlerName,
					"uri", prompt.VerificationURI)
				cancelCause(fmt.Errorf("device code URL validation failed for handler %q: %w",
					w.handlerName, err))
				return
			}
			opts.DeviceCodeCallback(prompt.UserCode, prompt.VerificationURI, prompt.Message)
		}
	}

	resp, err := w.client.plugin.Login(loginCtx, w.handlerName, req, deviceCodeCb)

	// Enforce the security check host-side: if the verification URI was
	// rejected, return that error even if the plugin returned success.
	if cause := context.Cause(loginCtx); cause != nil {
		return nil, cause
	}

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

// DetectAvailableFlows implements auth.FlowDetector.
func (w *AuthHandlerWrapper) DetectAvailableFlows(ctx context.Context) ([]auth.FlowAvailability, error) {
	return w.client.plugin.DetectAvailableFlows(ctx, w.handlerName)
}

// Client returns the underlying plugin client.
func (w *AuthHandlerWrapper) Client() *AuthHandlerClient {
	return w.client
}

// configureAndRegisterAuthHandlers configures each handler with host-side
// settings (when cfg is non-nil) and registers it in the auth registry.
// It also sets trusted verification domains on each wrapper from the official
// auth handler registry (per-handler) plus config.Auth.TrustedVerificationDomains.
func configureAndRegisterAuthHandlers(ctx context.Context, registry *auth.Registry, client *AuthHandlerClient, handlers []AuthHandlerInfo, cfg *ProviderConfig) {
	lgr := logger.FromContext(ctx)

	// Resolve trusted verification domains from context sources.
	officialReg := authofficial.RegistryFromContext(ctx)
	var cfgDomains []string
	if appCfg := config.FromContext(ctx); appCfg != nil {
		cfgDomains = appCfg.Auth.TrustedVerificationDomains
	}

	for _, info := range handlers {
		if cfg != nil {
			hostCfg := *cfg
			hostCfg.HostServiceID = client.HostServiceID()
			if cfgErr := client.ConfigureAuthHandler(ctx, info.Name, hostCfg); cfgErr != nil {
				lgr.Info("failed to configure auth handler plugin",
					"handler", info.Name,
					"error", cfgErr)
			}
		}

		wrapper := NewAuthHandlerWrapper(client, info)

		// Set trusted domains: merge per-handler official domains + global config domains.
		trusted := buildTrustedDomains(info.Name, officialReg, cfgDomains)
		if len(trusted) > 0 {
			wrapper.SetTrustedDomains(trusted)
		}

		if err := registry.Register(wrapper); err != nil {
			lgr.V(1).Info("failed to register auth handler", "handler", info.Name, "error", err)
			continue
		}
	}
}

// buildTrustedDomains merges per-handler official domains with global config domains.
func buildTrustedDomains(handlerName string, officialReg *authofficial.Registry, cfgDomains []string) []string {
	var domains []string

	if officialReg != nil {
		if h, ok := officialReg.Get(handlerName); ok {
			domains = append(domains, h.TrustedVerificationDomains...)
		}
	}

	domains = append(domains, cfgDomains...)
	return domains
}

// RegisterAuthHandlerPlugins discovers auth handler plugins and registers them
// with the auth registry. Returns the created clients (caller should Kill() them
// on cleanup).
func RegisterAuthHandlerPlugins(ctx context.Context, registry *auth.Registry, pluginDirs []string, cfg *ProviderConfig, clientOpts ...ClientOption) ([]*AuthHandlerClient, error) {
	clients, err := DiscoverAuthHandlers(pluginDirs, clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to discover auth handler plugins: %w", err)
	}

	var allClients []*AuthHandlerClient

	for _, client := range clients {
		handlers, err := client.GetAuthHandlers(ctx)
		if err != nil {
			client.Kill()
			continue
		}

		configureAndRegisterAuthHandlers(ctx, registry, client, handlers, cfg)
		allClients = append(allClients, client)
	}

	return allClients, nil
}

// KillAllAuthHandlers terminates all auth handler plugin processes in the given client list.
// This is safe to call with a nil or empty slice.
func KillAllAuthHandlers(clients []*AuthHandlerClient) {
	for _, c := range clients {
		if c != nil {
			c.Kill()
		}
	}
}
