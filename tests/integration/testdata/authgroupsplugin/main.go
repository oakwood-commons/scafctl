// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Command authgroupsplugin is a self-contained test auth-handler plugin used
// by CLI integration tests to verify the real hashicorp/go-plugin + SDK v0.19.0
// AuthHandlerService.GetGroups RPC (including per-RPC profile and settings
// forwarding as well as gRPC codes.Unimplemented fallback when
// SCAFCTL_TEST_GROUPS_UNIMPLEMENTED=1).
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"time"

	"github.com/oakwood-commons/scafctl-plugin-sdk/auth"
	sdkplugin "github.com/oakwood-commons/scafctl-plugin-sdk/plugin"
)

type baseAuthPlugin struct {
	configuredClientID string
}

func (p *baseAuthPlugin) GetAuthHandlers(_ context.Context) ([]sdkplugin.AuthHandlerInfo, error) {
	return []sdkplugin.AuthHandlerInfo{{
		Name:        "entra",
		DisplayName: "Microsoft Entra ID",
		Flows:       []auth.Flow{auth.FlowInteractive, auth.FlowDeviceCode},
		Capabilities: []auth.Capability{
			auth.CapScopesOnLogin,
			auth.CapScopesOnTokenRequest,
			auth.CapTenantID,
		},
	}}, nil
}

func (p *baseAuthPlugin) ConfigureAuthHandler(_ context.Context, _ string, cfg sdkplugin.ProviderConfig) error {
	if raw, ok := cfg.Settings["entra"]; ok && len(raw) > 0 {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err == nil {
			if cid, ok := m["clientId"].(string); ok {
				p.configuredClientID = cid
			}
		}
	}
	return nil
}

func (p *baseAuthPlugin) Login(_ context.Context, _ string, _ sdkplugin.LoginRequest, _ func(sdkplugin.DeviceCodePrompt)) (*sdkplugin.LoginResponse, error) {
	return &sdkplugin.LoginResponse{ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (p *baseAuthPlugin) Logout(_ context.Context, _ string, _ sdkplugin.LogoutRequest) error {
	return nil
}

func (p *baseAuthPlugin) GetStatus(_ context.Context, _ string, _ sdkplugin.StatusRequest) (*auth.Status, error) {
	return &auth.Status{Authenticated: true, ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (p *baseAuthPlugin) effectiveClientID(ctx context.Context, explicit map[string]json.RawMessage) string {
	clientID := p.configuredClientID
	settings := explicit
	hasSettings := settings != nil
	if !hasSettings {
		settings, hasSettings = sdkplugin.RequestSettingsFromContext(ctx)
	}
	if hasSettings {
		// Per-RPC settings override ConfigureAuthHandler settings (an empty
		// map resets configure-time profile settings back to defaults).
		clientID = "default-client"
		if raw, has := settings["clientId"]; has {
			_ = json.Unmarshal(raw, &clientID)
		}
	}
	return clientID
}

func (p *baseAuthPlugin) GetToken(ctx context.Context, _ string, req sdkplugin.TokenRequest) (*sdkplugin.TokenResponse, error) {
	hdr, _ := json.Marshal(map[string]any{"alg": "RS256", "typ": "JWT"})
	payload, _ := json.Marshal(map[string]any{
		"sub":              "sub-overage-e2e",
		"token_profile":    auth.ProfileFromContext(ctx),
		"token_client_id":  p.effectiveClientID(ctx, req.Settings),
		"token_ctx_client": p.effectiveClientID(ctx, nil),
		"_claim_names":     map[string]any{"groups": "src1"},
	})
	jwt := base64.RawURLEncoding.EncodeToString(hdr) + "." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
	return &sdkplugin.TokenResponse{
		AccessToken: jwt,
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
		Scope:       req.Scope,
		Flow:        auth.FlowInteractive,
	}, nil
}

func (p *baseAuthPlugin) ListCachedTokens(_ context.Context, _ string) ([]*auth.CachedTokenInfo, error) {
	return nil, nil
}

func (p *baseAuthPlugin) PurgeExpiredTokens(_ context.Context, _ string) (int, error) {
	return 0, nil
}

func (p *baseAuthPlugin) DetectAvailableFlows(_ context.Context, _ string) ([]sdkplugin.FlowAvailability, error) {
	return nil, nil
}

func (p *baseAuthPlugin) StopAuthHandler(_ context.Context, _ string) error {
	return nil
}

type groupsAuthPlugin struct {
	baseAuthPlugin
}

func (p *groupsAuthPlugin) GetGroups(ctx context.Context, _ string) ([]string, error) {
	profile := auth.ProfileFromContext(ctx)
	clientID := p.effectiveClientID(ctx, nil)
	return []string{
		"00000000-0000-0000-0000-000000000001",
		"profile:" + profile,
		"client:" + clientID,
	}, nil
}

func main() {
	if os.Getenv("SCAFCTL_TEST_GROUPS_UNIMPLEMENTED") == "1" {
		sdkplugin.ServeAuthHandler(&baseAuthPlugin{})
		return
	}
	sdkplugin.ServeAuthHandler(&groupsAuthPlugin{})
}
