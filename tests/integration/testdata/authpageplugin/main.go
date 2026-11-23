// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Command authpageplugin is a self-contained test auth-handler plugin used by
// CLI integration tests to verify ConfigureAuthHandler callback-page bundle
// delivery, runtime flag overrides, and deferred OAuth callback completion in
// standalone single-repo CI runs without external checkouts or network access.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	oauth "github.com/oakwood-commons/oauth-helpers"
	"github.com/oakwood-commons/scafctl-plugin-sdk/auth"
	sdkplugin "github.com/oakwood-commons/scafctl-plugin-sdk/plugin"
)

const (
	handlerName     = "entra"
	shutdownTimeout = 5 * time.Second
)

type handlerSettings struct {
	Authority string `json:"authority"`
	ClientID  string `json:"clientId"`
	TenantID  string `json:"tenantId"`
}

type authPageTestPlugin struct {
	mu  sync.RWMutex
	cfg sdkplugin.ProviderConfig
}

func (p *authPageTestPlugin) GetAuthHandlers(_ context.Context) ([]sdkplugin.AuthHandlerInfo, error) {
	return []sdkplugin.AuthHandlerInfo{{
		Name:        handlerName,
		DisplayName: "Microsoft Entra ID",
		Flows:       []auth.Flow{auth.FlowInteractive, auth.FlowDeviceCode},
		Capabilities: []auth.Capability{
			auth.CapScopesOnLogin,
			auth.CapScopesOnTokenRequest,
			auth.CapTenantID,
			auth.CapCallbackPort,
			auth.CapFlowOverride,
		},
	}}, nil
}

func (p *authPageTestPlugin) ConfigureAuthHandler(_ context.Context, _ string, cfg sdkplugin.ProviderConfig) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cfg = cfg
	return nil
}

func (p *authPageTestPlugin) snapshotConfig() (sdkplugin.ProviderConfig, handlerSettings) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var hs handlerSettings
	if raw, ok := p.cfg.Settings[handlerName]; ok && len(raw) > 0 {
		_ = json.Unmarshal(raw, &hs)
	}
	return p.cfg, hs
}

func (p *authPageTestPlugin) Login(
	ctx context.Context,
	_ string,
	req sdkplugin.LoginRequest,
	_ func(sdkplugin.DeviceCodePrompt),
) (*sdkplugin.LoginResponse, error) {
	cfg, hs := p.snapshotConfig()

	authority := strings.TrimRight(hs.Authority, "/")
	if authority == "" {
		authority = "https://login.microsoftonline.com"
	}
	tenantID := req.TenantID
	if tenantID == "" {
		tenantID = hs.TenantID
	}
	if tenantID == "" {
		tenantID = "organizations"
	}
	clientID := hs.ClientID
	if clientID == "" {
		clientID = "test-client-id"
	}

	state, err := oauth.GenerateCodeVerifier()
	if err != nil {
		return nil, fmt.Errorf("generate state: %w", err)
	}

	cbOpts := []oauth.CallbackOption{oauth.WithDeferredCompletion()}
	if pages := cfg.AuthCallbackPages; pages != nil {
		cbOpts = append(cbOpts, oauth.WithCallbackPages(oauth.CallbackPages{
			SuccessHTML: pages.SuccessHTML,
			FailureHTML: pages.FailureHTML,
		}))
	}

	cs, err := oauth.StartCallbackServer(ctx, req.CallbackPort, state, cbOpts...)
	if err != nil {
		return nil, fmt.Errorf("start callback server: %w", err)
	}

	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", cs.RedirectURI)
	q.Set("state", state)
	authURL := fmt.Sprintf("%s/%s/oauth2/v2.0/authorize?%s", authority, url.PathEscape(tenantID), q.Encode())

	_ = oauth.OpenBrowserWithPreference(ctx, authURL, req.Browser)

	var code string
	select {
	case res := <-cs.ResultChan():
		if res.Err != nil {
			shutdownServer(cs)
			return nil, fmt.Errorf("callback error: %w", res.Err)
		}
		code = res.Code
	case <-ctx.Done():
		_ = cs.Close()
		return nil, ctx.Err()
	}

	tokenEndpoint := fmt.Sprintf("%s/%s/oauth2/v2.0/token", authority, url.PathEscape(tenantID))
	form := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {cs.RedirectURI},
		"client_id":    {clientID},
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		cs.Complete(false)
		shutdownServer(cs)
		return nil, fmt.Errorf("token_exchange request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		cs.Complete(false)
		shutdownServer(cs)
		return nil, fmt.Errorf("token_exchange request failed: %w", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		cs.Complete(false)
		shutdownServer(cs)
		return nil, fmt.Errorf("token_exchange failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	cs.Complete(true)
	shutdownServer(cs)

	return &sdkplugin.LoginResponse{
		Claims: &auth.Claims{
			Subject:  "sub-d2e",
			Email:    "dev@example.com",
			Username: "dev@example.com",
			TenantID: tenantID,
		},
		ExpiresAt: time.Now().Add(time.Hour),
	}, nil
}

func shutdownServer(cs *oauth.CallbackServer) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	_ = cs.Shutdown(shutdownCtx)
}

func (p *authPageTestPlugin) effectiveSettings(ctx context.Context) (sdkplugin.ProviderConfig, handlerSettings) {
	cfg, hs := p.snapshotConfig()
	if reqSettings, ok := sdkplugin.RequestSettingsFromContext(ctx); ok {
		hs = handlerSettings{}
		raw, err := json.Marshal(reqSettings)
		if err == nil {
			_ = json.Unmarshal(raw, &hs)
		}
	}
	return cfg, hs
}

func (p *authPageTestPlugin) Logout(_ context.Context, _ string, _ sdkplugin.LogoutRequest) error {
	return nil
}

func (p *authPageTestPlugin) GetStatus(ctx context.Context, _ string, _ sdkplugin.StatusRequest) (*auth.Status, error) {
	_, hs := p.effectiveSettings(ctx)
	return &auth.Status{
		Authenticated: false,
		ClientID:      hs.ClientID,
		TenantID:      hs.TenantID,
	}, nil
}

func (p *authPageTestPlugin) GetToken(ctx context.Context, _ string, req sdkplugin.TokenRequest) (*sdkplugin.TokenResponse, error) {
	_, hs := p.effectiveSettings(ctx)
	prof := auth.ProfileFromContext(ctx)
	return &sdkplugin.TokenResponse{
		AccessToken: fmt.Sprintf("subproc-tok:%s:%s:%s:%s", prof, hs.ClientID, hs.TenantID, req.Scope),
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
		Scope:       req.Scope,
		Flow:        auth.FlowInteractive,
	}, nil
}

func (p *authPageTestPlugin) ListCachedTokens(_ context.Context, _ string) ([]*auth.CachedTokenInfo, error) {
	return nil, nil
}

func (p *authPageTestPlugin) PurgeExpiredTokens(_ context.Context, _ string) (int, error) {
	return 0, nil
}

func (p *authPageTestPlugin) DetectAvailableFlows(_ context.Context, _ string) ([]sdkplugin.FlowAvailability, error) {
	return []sdkplugin.FlowAvailability{{Flow: auth.FlowInteractive, Available: true}}, nil
}

func (p *authPageTestPlugin) StopAuthHandler(_ context.Context, _ string) error { return nil }

func main() {
	sdkplugin.ServeAuthHandler(&authPageTestPlugin{})
}
