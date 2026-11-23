// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"bytes"
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl-plugin-sdk/plugin/proto"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthCallbackPages_GRPCRoundTripAndLimits(t *testing.T) {
	oversized := bytes.Repeat([]byte("a"), MaxAuthCallbackPageBytes+1)

	t.Run("valid pages survive AuthHandlerGRPCClient to AuthHandlerGRPCServer ConfigureAuthHandler", func(t *testing.T) {
		svcMock := &mockAuthHandlerServiceClient{
			configureAuthHandlerResp: &proto.ConfigureAuthHandlerResponse{
				ProtocolVersion: PluginProtocolVersion,
			},
		}
		grpcClient := &AuthHandlerGRPCClient{client: svcMock, hostServiceID: 7}

		inPages := &AuthCallbackPages{
			SuccessHTML: []byte("<!doctype html><html><body>HOST-SUCCESS</body></html>"),
			FailureHTML: []byte("<!doctype html><html><body>HOST-FAILURE</body></html>"),
		}
		err := grpcClient.ConfigureAuthHandler(context.Background(), "entra", ProviderConfig{
			BinaryName:        "mycli",
			Profile:           "work",
			AuthCallbackPages: inPages,
		})
		require.NoError(t, err)
		require.NotNil(t, svcMock.lastConfigureReq)
		require.NotNil(t, svcMock.lastConfigureReq.AuthCallbackPages)

		mockImpl := &MockAuthHandlerPlugin{}
		srv := &AuthHandlerGRPCServer{Impl: mockImpl}
		resp, err := srv.ConfigureAuthHandler(context.Background(), svcMock.lastConfigureReq)
		require.NoError(t, err)
		assert.Empty(t, resp.Error)
		require.NotNil(t, mockImpl.lastConfig)
		require.NotNil(t, mockImpl.lastConfig.AuthCallbackPages)
		assert.Equal(t, inPages.SuccessHTML, mockImpl.lastConfig.AuthCallbackPages.SuccessHTML)
		assert.Equal(t, inPages.FailureHTML, mockImpl.lastConfig.AuthCallbackPages.FailureHTML)
	})

	t.Run("oversized documents are dropped at both client and server boundaries", func(t *testing.T) {
		assert.Nil(t, authCallbackPagesToProto(nil))
		assert.Nil(t, authCallbackPagesToProto(&AuthCallbackPages{}))
		assert.Nil(t, authCallbackPagesToProto(&AuthCallbackPages{
			SuccessHTML: oversized,
			FailureHTML: oversized,
		}))

		assert.Nil(t, authCallbackPagesFromProto(nil))
		assert.Nil(t, authCallbackPagesFromProto(&proto.AuthCallbackPages{
			SuccessHtml: oversized,
			FailureHtml: oversized,
		}))

		got := authCallbackPagesFromProto(&proto.AuthCallbackPages{
			SuccessHtml: oversized,
			FailureHtml: []byte("<html>ok</html>"),
		})
		require.NotNil(t, got)
		assert.Empty(t, got.SuccessHTML)
		assert.Equal(t, []byte("<html>ok</html>"), got.FailureHTML)
	})
}

func TestAuthHandlerWrapper_ApplyOverrides_PreservesAuthCallbackPages(t *testing.T) {
	mockImpl := &MockAuthHandlerPlugin{}
	ahClient := &AuthHandlerClient{plugin: mockImpl}
	registry := auth.NewRegistry()

	pages := &AuthCallbackPages{
		SuccessHTML: []byte("<!doctype html><html><body>PRESERVED-SUCCESS</body></html>"),
		FailureHTML: []byte("<!doctype html><html><body>PRESERVED-FAILURE</body></html>"),
	}
	initialCfg := &ProviderConfig{
		BinaryName:        "mycli",
		Profile:           "work",
		AuthCallbackPages: pages,
	}

	registered := configureAndRegisterAuthHandlers(
		context.Background(),
		registry,
		ahClient,
		[]AuthHandlerInfo{{Name: "entra", DisplayName: "Entra"}},
		initialCfg,
	)
	require.Equal(t, []string{"entra"}, registered)

	h, ok := registry.GetRegistered("entra")
	require.True(t, ok)
	cfgr, ok := h.(auth.Configurer)
	require.True(t, ok)

	// Passing --tenant / --client-id triggers ApplyOverrides, which re-runs ConfigureAuthHandler.
	err := cfgr.ApplyOverrides(context.Background(), map[string]string{
		"tenantId": "11111111-2222-3333-4444-555555555555",
	})
	require.NoError(t, err)

	require.NotNil(t, mockImpl.lastConfig)
	require.NotNil(t, mockImpl.lastConfig.AuthCallbackPages)
	assert.Equal(t, pages.SuccessHTML, mockImpl.lastConfig.AuthCallbackPages.SuccessHTML)
	assert.Equal(t, pages.FailureHTML, mockImpl.lastConfig.AuthCallbackPages.FailureHTML)
	assert.Equal(t, "mycli", mockImpl.lastConfig.BinaryName)
	assert.Equal(t, "work", mockImpl.lastConfig.Profile)
}
