// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"fmt"
	"testing"

	"github.com/oakwood-commons/scafctl-plugin-sdk/plugin/proto"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- GetAuthGroups server tests ---

func TestHostServiceServer_GetAuthGroups_Success(t *testing.T) {
	deps := HostServiceDeps{
		AuthGroupsFunc: func(_ context.Context, _ string) ([]string, error) {
			return []string{"group-a", "group-b"}, nil
		},
	}
	server := &HostServiceServer{Deps: deps}

	resp, err := server.GetAuthGroups(context.Background(), &proto.GetAuthGroupsRequest{
		HandlerName: "entra",
	})
	require.NoError(t, err)
	assert.Empty(t, resp.Error)
	assert.Equal(t, []string{"group-a", "group-b"}, resp.Groups)
}

func TestHostServiceServer_GetAuthGroups_NotAvailable(t *testing.T) {
	server := &HostServiceServer{Deps: HostServiceDeps{}}

	resp, err := server.GetAuthGroups(context.Background(), &proto.GetAuthGroupsRequest{
		HandlerName: "entra",
	})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "auth groups not available")
}

func TestHostServiceServer_GetAuthGroups_Denied(t *testing.T) {
	deps := HostServiceDeps{
		AllowedAuthHandlers: []string{"github"},
		AuthGroupsFunc: func(_ context.Context, _ string) ([]string, error) {
			t.Fatal("should not be called for denied handler")
			return nil, nil
		},
	}
	server := &HostServiceServer{Deps: deps}

	resp, err := server.GetAuthGroups(context.Background(), &proto.GetAuthGroupsRequest{
		HandlerName: "entra",
	})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "access denied")
}

func TestHostServiceServer_GetAuthGroups_FuncError(t *testing.T) {
	deps := HostServiceDeps{
		AuthGroupsFunc: func(_ context.Context, _ string) ([]string, error) {
			return nil, fmt.Errorf("graph API unavailable")
		},
	}
	server := &HostServiceServer{Deps: deps}

	resp, err := server.GetAuthGroups(context.Background(), &proto.GetAuthGroupsRequest{
		HandlerName: "entra",
	})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "graph API unavailable")
}

// TestHostServiceServer_GetAuthGroups_WiredFromRegistry exercises the full path:
// deps produced by HostDepsFromAuthRegistry driving the RPC server against a
// real auth.Registry whose handler implements GroupsProvider.
func TestHostServiceServer_GetAuthGroups_WiredFromRegistry(t *testing.T) {
	reg := auth.NewRegistry()
	require.NoError(t, reg.Register(&mockGroupsHandler{
		MockHandler: auth.NewMockHandler("entra"),
		groups:      []string{"g1", "g2", "g3"},
	}))

	deps := HostDepsFromAuthRegistry(reg)
	require.NotNil(t, deps)
	server := &HostServiceServer{Deps: *deps}

	resp, err := server.GetAuthGroups(context.Background(), &proto.GetAuthGroupsRequest{
		HandlerName: "entra",
	})
	require.NoError(t, err)
	assert.Empty(t, resp.Error)
	assert.Equal(t, []string{"g1", "g2", "g3"}, resp.Groups)
}

// TestHostServiceServer_GetAuthGroups_WiredFromRegistry_Denied verifies the
// allowlist gate still blocks a disallowed handler even when the callback is
// wired from a real registry.
func TestHostServiceServer_GetAuthGroups_WiredFromRegistry_Denied(t *testing.T) {
	reg := auth.NewRegistry()
	require.NoError(t, reg.Register(&mockGroupsHandler{
		MockHandler: auth.NewMockHandler("entra"),
		groups:      []string{"g1"},
	}))

	deps := HostDepsFromAuthRegistry(reg)
	require.NotNil(t, deps)
	deps.AllowedAuthHandlers = []string{"github"}
	server := &HostServiceServer{Deps: *deps}

	resp, err := server.GetAuthGroups(context.Background(), &proto.GetAuthGroupsRequest{
		HandlerName: "entra",
	})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "access denied")
	assert.Nil(t, resp.Groups)
}

// --- GetAuthGroups client tests ---

func TestHostServiceClient_GetAuthGroups_Success(t *testing.T) {
	mock := &mockHostServiceClient{
		getAuthGroupsResp: &proto.GetAuthGroupsResponse{
			Groups: []string{"group-x"},
		},
	}
	client := &HostServiceClient{client: mock}

	groups, err := client.GetAuthGroups(context.Background(), "entra")
	require.NoError(t, err)
	assert.Equal(t, []string{"group-x"}, groups)
}

func TestHostServiceClient_GetAuthGroups_RPCError(t *testing.T) {
	mock := &mockHostServiceClient{
		getAuthGroupsErr: fmt.Errorf("connection lost"),
	}
	client := &HostServiceClient{client: mock}

	_, err := client.GetAuthGroups(context.Background(), "entra")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection lost")
}

func TestHostServiceClient_GetAuthGroups_ResponseError(t *testing.T) {
	mock := &mockHostServiceClient{
		getAuthGroupsResp: &proto.GetAuthGroupsResponse{
			Error: "handler does not support groups",
		},
	}
	client := &HostServiceClient{client: mock}

	_, err := client.GetAuthGroups(context.Background(), "github")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "handler does not support groups")
}
