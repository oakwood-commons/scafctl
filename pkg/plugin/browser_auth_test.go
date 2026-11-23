// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl-plugin-sdk/plugin/proto"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestAuthHandlerWrapper_Login_ForwardsBrowserAndWarnsOnlyOnInteractiveFlow(t *testing.T) {
	t.Parallel()

	t.Run("forwards explicit --browser and warns on host when browser missing in interactive flow", func(t *testing.T) {
		t.Parallel()

		var gotReq LoginRequest
		mock := &MockAuthHandlerPlugin{
			loginFunc: func(_ context.Context, _ string, req LoginRequest, _ func(DeviceCodePrompt)) (*LoginResponse, error) {
				gotReq = req
				return &LoginResponse{
					Claims:    &auth.Claims{Email: "user@example.com"},
					ExpiresAt: time.Now().Add(time.Hour),
				}, nil
			},
		}

		wrapper := &AuthHandlerWrapper{
			client:      &AuthHandlerClient{plugin: mock, name: "test-plugin"},
			handlerName: "entra",
			info: AuthHandlerInfo{
				Name:  "entra",
				Flows: []auth.Flow{auth.FlowInteractive, auth.FlowDeviceCode},
			},
		}

		var outBuf bytes.Buffer
		ioStreams := terminal.NewIOStreams(nil, &outBuf, &outBuf, false)
		w := writer.New(ioStreams, settings.NewCliParams())
		ctx := writer.WithWriter(context.Background(), w)

		_, err := wrapper.Login(ctx, auth.LoginOptions{
			Flow:    auth.FlowInteractive,
			Browser: "'nonexistent-browser-for-plugin-test'",
		})
		require.NoError(t, err)
		assert.Equal(t, "nonexistent-browser-for-plugin-test", gotReq.Browser)
		assert.Contains(t, outBuf.String(), `configured browser "nonexistent-browser-for-plugin-test" could not be launched`)
	})

	t.Run("resolves auth.browser from config and suppresses warning on non-interactive flow", func(t *testing.T) {
		t.Parallel()

		var gotReq LoginRequest
		mock := &MockAuthHandlerPlugin{
			loginFunc: func(_ context.Context, _ string, req LoginRequest, _ func(DeviceCodePrompt)) (*LoginResponse, error) {
				gotReq = req
				return &LoginResponse{
					Claims:    &auth.Claims{Email: "user@example.com"},
					ExpiresAt: time.Now().Add(time.Hour),
				}, nil
			},
		}

		wrapper := &AuthHandlerWrapper{
			client:      &AuthHandlerClient{plugin: mock, name: "test-plugin"},
			handlerName: "entra",
			info: AuthHandlerInfo{
				Name:  "entra",
				Flows: []auth.Flow{auth.FlowInteractive, auth.FlowDeviceCode, auth.FlowServicePrincipal},
			},
		}

		var outBuf bytes.Buffer
		ioStreams := terminal.NewIOStreams(nil, &outBuf, &outBuf, false)
		w := writer.New(ioStreams, settings.NewCliParams())
		ctx := config.WithConfig(writer.WithWriter(context.Background(), w), &config.Config{
			Auth: config.GlobalAuthConfig{Browser: "nonexistent-browser-for-sp-test"},
		})

		_, err := wrapper.Login(ctx, auth.LoginOptions{
			Flow: auth.FlowServicePrincipal,
		})
		require.NoError(t, err)
		assert.Equal(t, "nonexistent-browser-for-sp-test", gotReq.Browser)
		assert.Empty(t, outBuf.String(), "non-interactive flows must not emit browser warnings")
	})

	t.Run("empty flow consults DetectAvailableFlows and Entra defaultFlow before warning", func(t *testing.T) {
		t.Parallel()

		var sawDeadline bool
		mock := &MockAuthHandlerPlugin{
			detectFlowsFunc: func(dctx context.Context, _ string) ([]FlowAvailability, error) {
				_, sawDeadline = dctx.Deadline()
				return []FlowAvailability{
					{Flow: auth.FlowDeviceCode, Available: true, Reason: "headless session"},
				}, nil
			},
			loginFunc: func(_ context.Context, _ string, _ LoginRequest, _ func(DeviceCodePrompt)) (*LoginResponse, error) {
				return &LoginResponse{
					Claims:    &auth.Claims{Email: "user@example.com"},
					ExpiresAt: time.Now().Add(time.Hour),
				}, nil
			},
		}

		wrapper := &AuthHandlerWrapper{
			client:      &AuthHandlerClient{plugin: mock, name: "test-plugin"},
			handlerName: "entra",
			info: AuthHandlerInfo{
				Name:  "entra",
				Flows: []auth.Flow{auth.FlowInteractive, auth.FlowDeviceCode},
			},
		}

		var outBuf bytes.Buffer
		ioStreams := terminal.NewIOStreams(nil, &outBuf, &outBuf, false)
		w := writer.New(ioStreams, settings.NewCliParams())
		ctx := config.WithConfig(writer.WithWriter(context.Background(), w), &config.Config{
			Auth: config.GlobalAuthConfig{Browser: "nonexistent-browser-for-headless-kube-login"},
		})

		_, err := wrapper.Login(ctx, auth.LoginOptions{})
		require.NoError(t, err)
		assert.True(t, sawDeadline, "preflight DetectAvailableFlows RPC must always carry a deadline")
		assert.Empty(t, outBuf.String(), "empty flow auto-resolving to device_code must not emit browser warning")
	})

	t.Run("stalled DetectAvailableFlows preflight honors LoginOptions.Timeout and aborts before Login RPC", func(t *testing.T) {
		t.Parallel()

		loginCalled := false
		mock := &MockAuthHandlerPlugin{
			detectFlowsFunc: func(dctx context.Context, _ string) ([]FlowAvailability, error) {
				<-dctx.Done()
				return nil, dctx.Err()
			},
			loginFunc: func(_ context.Context, _ string, _ LoginRequest, _ func(DeviceCodePrompt)) (*LoginResponse, error) {
				loginCalled = true
				return &LoginResponse{}, nil
			},
		}

		wrapper := &AuthHandlerWrapper{
			client:      &AuthHandlerClient{plugin: mock, name: "test-plugin"},
			handlerName: "entra",
			info: AuthHandlerInfo{
				Name:  "entra",
				Flows: []auth.Flow{auth.FlowInteractive, auth.FlowDeviceCode},
			},
		}

		var outBuf bytes.Buffer
		ioStreams := terminal.NewIOStreams(nil, &outBuf, &outBuf, false)
		w := writer.New(ioStreams, settings.NewCliParams())
		ctx := writer.WithWriter(context.Background(), w)

		_, err := wrapper.Login(ctx, auth.LoginOptions{
			Browser: "nonexistent-browser-stalled-preflight",
			Timeout: 40 * time.Millisecond,
		})
		require.Error(t, err)
		require.ErrorIs(t, err, auth.ErrTimeout)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.False(t, loginCalled, "stalled preflight exceeding LoginOptions.Timeout must not proceed to Login RPC")
		assert.Empty(t, outBuf.String(), "timed-out preflight must not emit browser warning")
	})

	t.Run("caller context cancellation during DetectAvailableFlows preflight aborts before Login RPC", func(t *testing.T) {
		t.Parallel()

		started := make(chan struct{}, 1)
		loginCalled := false
		mock := &MockAuthHandlerPlugin{
			detectFlowsFunc: func(dctx context.Context, _ string) ([]FlowAvailability, error) {
				started <- struct{}{}
				<-dctx.Done()
				// Simulate a plugin that swallows the cancellation error and returns a flow anyway.
				return []FlowAvailability{
					{Flow: auth.FlowInteractive, Available: true},
				}, nil
			},
			loginFunc: func(_ context.Context, _ string, _ LoginRequest, _ func(DeviceCodePrompt)) (*LoginResponse, error) {
				loginCalled = true
				return &LoginResponse{}, nil
			},
		}

		wrapper := &AuthHandlerWrapper{
			client:      &AuthHandlerClient{plugin: mock, name: "test-plugin"},
			handlerName: "entra",
			info: AuthHandlerInfo{
				Name:  "entra",
				Flows: []auth.Flow{auth.FlowInteractive, auth.FlowDeviceCode},
			},
		}

		var outBuf bytes.Buffer
		ioStreams := terminal.NewIOStreams(nil, &outBuf, &outBuf, false)
		w := writer.New(ioStreams, settings.NewCliParams())
		ctx, cancel := context.WithCancel(writer.WithWriter(context.Background(), w))
		go func() {
			<-started
			cancel()
		}()

		_, err := wrapper.Login(ctx, auth.LoginOptions{
			Browser: "nonexistent-browser-cancelled-preflight",
		})
		require.Error(t, err)
		require.ErrorIs(t, err, context.Canceled)
		assert.False(t, loginCalled, "cancelled preflight must not proceed to Login RPC")
		assert.Empty(t, outBuf.String(), "cancelled preflight must not emit browser warning")
	})

	t.Run("invalid browser preference fails closed before normalization or plugin RPC", func(t *testing.T) {
		t.Parallel()

		for _, bad := range []string{
			"--headless",
			"msedge\n",
			"   ",
			"  " + strings.Repeat("a", config.MaxAuthBrowserLength),
		} {
			rpcCalled := false
			mock := &MockAuthHandlerPlugin{
				loginFunc: func(_ context.Context, _ string, _ LoginRequest, _ func(DeviceCodePrompt)) (*LoginResponse, error) {
					rpcCalled = true
					return &LoginResponse{}, nil
				},
			}
			wrapper := &AuthHandlerWrapper{
				client:      &AuthHandlerClient{plugin: mock, name: "test-plugin"},
				handlerName: "entra",
				info:        AuthHandlerInfo{Name: "entra"},
			}

			cfgCtx := config.WithConfig(context.Background(), &config.Config{
				Auth: config.GlobalAuthConfig{Browser: "msedge"},
			})
			_, err := wrapper.Login(cfgCtx, auth.LoginOptions{
				Flow:    auth.FlowInteractive,
				Browser: bad,
			})
			require.Error(t, err, "expected wrapper.Login to reject %q", bad)
			assert.Contains(t, err.Error(), "invalid browser preference")
			assert.False(t, rpcCalled)
		}
	})
}

func TestAuthHandlerGRPCClient_Login_ForwardsBrowser(t *testing.T) {
	t.Parallel()

	now := time.Now().Truncate(time.Second)
	var gotReq *proto.LoginRequest
	mock := &mockAuthHandlerServiceClient{
		loginFunc: func(_ context.Context, req *proto.LoginRequest) (grpc.ServerStreamingClient[proto.LoginStreamMessage], error) {
			gotReq = req
			return &mockLoginStreamClient{
				messages: []*proto.LoginStreamMessage{
					{
						Payload: &proto.LoginStreamMessage_Result{
							Result: &proto.LoginResult{
								Claims:        &proto.Claims{Email: "user@example.com"},
								ExpiresAtUnix: now.Add(time.Hour).Unix(),
							},
						},
					},
				},
			}, nil
		},
	}
	client := &AuthHandlerGRPCClient{client: mock}

	_, err := client.Login(context.Background(), "entra", LoginRequest{
		Flow:    auth.FlowInteractive,
		Browser: "msedge",
	}, nil)
	require.NoError(t, err)
	require.NotNil(t, gotReq)
	assert.Equal(t, "msedge", gotReq.Browser, "browser must be mapped onto the proto LoginRequest")
}

func TestAuthHandlerGRPCServer_Login_ForwardsAndValidatesBrowser(t *testing.T) {
	t.Parallel()

	var captured LoginRequest
	implCalled := false
	mockImpl := &MockAuthHandlerPlugin{
		loginFunc: func(_ context.Context, _ string, req LoginRequest, _ func(DeviceCodePrompt)) (*LoginResponse, error) {
			implCalled = true
			captured = req
			return &LoginResponse{
				Claims:    &auth.Claims{Email: "user@example.com"},
				ExpiresAt: time.Now().Add(time.Hour),
			}, nil
		},
	}

	srv := &AuthHandlerGRPCServer{Impl: mockImpl}
	stream := &mockLoginServerStream{ctx: context.Background()}

	err := srv.Login(&proto.LoginRequest{
		HandlerName: "entra",
		TenantId:    "tid-123",
		Flow:        string(auth.FlowInteractive),
		Browser:     "'msedge'",
	}, stream)
	require.NoError(t, err)
	assert.True(t, implCalled)
	assert.Equal(t, "msedge", captured.Browser)
	assert.Equal(t, "tid-123", captured.TenantID)

	// Defense-in-depth: leading-dash flag injection over gRPC is rejected before calling s.Impl.Login.
	implCalled = false
	badStream := &mockLoginServerStream{ctx: context.Background()}
	err = srv.Login(&proto.LoginRequest{
		HandlerName: "entra",
		Flow:        string(auth.FlowInteractive),
		Browser:     "--remote-debugging-port=9222",
	}, badStream)
	require.NoError(t, err)
	assert.False(t, implCalled, "invalid browser on LoginRequest must not reach Impl.Login")
	require.Len(t, badStream.messages, 1)
	errPayload, ok := badStream.messages[0].Payload.(*proto.LoginStreamMessage_Error)
	require.True(t, ok)
	assert.Contains(t, errPayload.Error, "invalid browser:")
}
