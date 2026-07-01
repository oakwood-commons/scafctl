// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Command authhandler is a minimal auth-handler plugin used only by unit tests
// in pkg/plugin. It exposes a single handler whose name is controlled by the
// SCAFCTL_TEST_AUTH_HANDLER_NAME environment variable (default "test-auth") so
// tests can exercise registration and de-duplication behavior.
package main

import (
	"context"
	"os"

	"github.com/oakwood-commons/scafctl-plugin-sdk/auth"
	sdkplugin "github.com/oakwood-commons/scafctl-plugin-sdk/plugin"
)

type testAuthHandler struct{}

func handlerName() string {
	if n := os.Getenv("SCAFCTL_TEST_AUTH_HANDLER_NAME"); n != "" {
		return n
	}
	return "test-auth"
}

func (h *testAuthHandler) GetAuthHandlers(_ context.Context) ([]sdkplugin.AuthHandlerInfo, error) {
	return []sdkplugin.AuthHandlerInfo{{
		Name:        handlerName(),
		DisplayName: "Test Auth Handler",
	}}, nil
}

func (h *testAuthHandler) ConfigureAuthHandler(_ context.Context, _ string, _ sdkplugin.ProviderConfig) error {
	return nil
}

func (h *testAuthHandler) Login(_ context.Context, _ string, _ sdkplugin.LoginRequest, _ func(sdkplugin.DeviceCodePrompt)) (*sdkplugin.LoginResponse, error) {
	return &sdkplugin.LoginResponse{}, nil
}

func (h *testAuthHandler) Logout(_ context.Context, _ string) error { return nil }

func (h *testAuthHandler) GetStatus(_ context.Context, _ string) (*auth.Status, error) {
	return &auth.Status{}, nil
}

func (h *testAuthHandler) GetToken(_ context.Context, _ string, _ sdkplugin.TokenRequest) (*sdkplugin.TokenResponse, error) {
	return &sdkplugin.TokenResponse{}, nil
}

func (h *testAuthHandler) ListCachedTokens(_ context.Context, _ string) ([]*auth.CachedTokenInfo, error) {
	return nil, nil
}

func (h *testAuthHandler) PurgeExpiredTokens(_ context.Context, _ string) (int, error) {
	return 0, nil
}

func (h *testAuthHandler) DetectAvailableFlows(_ context.Context, _ string) ([]sdkplugin.FlowAvailability, error) {
	return nil, nil
}

func (h *testAuthHandler) StopAuthHandler(_ context.Context, _ string) error { return nil }

func main() {
	sdkplugin.ServeAuthHandler(&testAuthHandler{})
}
