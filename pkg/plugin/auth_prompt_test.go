// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/go-logr/logr/funcr"
	"github.com/oakwood-commons/scafctl-plugin-sdk/plugin/proto"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAuthPromptBroker_BeginPromptEnd(t *testing.T) {
	t.Parallel()

	broker := &AuthPromptBroker{}
	fn := func(ctx context.Context, authURL, redirectURI string) (string, error) {
		return "pasted", nil
	}

	end := broker.Begin("entra", fn)
	got, ok := broker.Prompt("entra")
	assert.True(t, ok, "prompt active during login window")
	assert.NotNil(t, got)

	// Wrong handler is refused even while a login is active.
	_, ok = broker.Prompt("github")
	assert.False(t, ok, "prompt must be scoped to the calling handler")

	end()
	_, ok = broker.Prompt("entra")
	assert.False(t, ok, "prompt closed after login returns")

	// end is idempotent.
	end()
}

func TestAuthPromptBroker_LastBeginWins(t *testing.T) {
	t.Parallel()

	broker := &AuthPromptBroker{}
	endA := broker.Begin("entra", nil)
	endB := broker.Begin("entra", func(ctx context.Context, authURL, redirectURI string) (string, error) {
		return "b", nil
	})

	_, okA := broker.Prompt("entra")
	require.True(t, okA)

	// The earlier window must not clear the later one.
	endA()
	_, ok := broker.Prompt("entra")
	assert.True(t, ok, "end of an older window must not clear the active one")

	endB()
	_, ok = broker.Prompt("entra")
	assert.False(t, ok)
}

func TestAuthPromptBroker_NilFuncIsInteractionState(t *testing.T) {
	t.Parallel()

	broker := &AuthPromptBroker{}
	defer broker.Begin("entra", nil)()

	fn, ok := broker.Prompt("entra")
	assert.True(t, ok, "login is active")
	assert.Nil(t, fn, "non-interactive session carries a nil prompt")
}

func TestAuthPromptBroker_Concurrent(t *testing.T) {
	t.Parallel()

	broker := &AuthPromptBroker{}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			end := broker.Begin("entra", nil)
			_, _ = broker.Prompt("entra")
			end()
		}()
	}
	wg.Wait()
	_, ok := broker.Prompt("entra")
	assert.False(t, ok, "no window survives the storm")
}

func TestHostServiceServer_PromptAuthResponse(t *testing.T) {
	t.Parallel()

	t.Run("returns pasted value during active login", func(t *testing.T) {
		t.Parallel()
		broker := &AuthPromptBroker{}
		server := &HostServiceServer{Deps: HostServiceDeps{PromptBroker: broker}}
		pasted := "http://localhost:8400/callback?code=abc&state=xyz"
		defer broker.Begin("entra", func(ctx context.Context, authURL, redirectURI string) (string, error) {
			assert.Equal(t, "https://login.example/auth", authURL)
			assert.Equal(t, "http://localhost:8400/callback", redirectURI)
			return pasted, nil
		})()

		resp, err := server.PromptAuthResponse(context.Background(), &proto.PromptAuthResponseRequest{
			HandlerName:      "entra",
			AuthorizationUrl: "https://login.example/auth",
			RedirectUri:      "http://localhost:8400/callback",
		})
		require.NoError(t, err)
		assert.Equal(t, pasted, resp.Value)
	})

	t.Run("refused outside an active login", func(t *testing.T) {
		t.Parallel()
		server := &HostServiceServer{Deps: HostServiceDeps{PromptBroker: &AuthPromptBroker{}}}
		_, err := server.PromptAuthResponse(context.Background(), &proto.PromptAuthResponseRequest{
			HandlerName: "entra",
		})
		require.Error(t, err)
		// The SDK v0.18.0 HostService contract documents Unavailable for
		// calls outside an active Login.
		assert.Equal(t, codes.Unavailable, status.Code(err))
		assert.NotContains(t, err.Error(), "code=", "status message must not leak pasted data")
	})

	t.Run("refused for a different handler's active login", func(t *testing.T) {
		t.Parallel()
		broker := &AuthPromptBroker{}
		server := &HostServiceServer{Deps: HostServiceDeps{PromptBroker: broker}}
		defer broker.Begin("entra", nil)()
		_, err := server.PromptAuthResponse(context.Background(), &proto.PromptAuthResponseRequest{
			HandlerName: "github",
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unavailable, status.Code(err))
	})

	t.Run("unavailable when the session is not interactive", func(t *testing.T) {
		t.Parallel()
		broker := &AuthPromptBroker{}
		server := &HostServiceServer{Deps: HostServiceDeps{PromptBroker: broker}}
		defer broker.Begin("entra", nil)()
		_, err := server.PromptAuthResponse(context.Background(), &proto.PromptAuthResponseRequest{
			HandlerName: "entra",
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unavailable, status.Code(err))
	})

	t.Run("unimplemented when the host wires no broker", func(t *testing.T) {
		t.Parallel()
		server := &HostServiceServer{Deps: HostServiceDeps{}}
		_, err := server.PromptAuthResponse(context.Background(), &proto.PromptAuthResponseRequest{
			HandlerName: "entra",
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("rejects a non-https authorization URL", func(t *testing.T) {
		t.Parallel()
		broker := &AuthPromptBroker{}
		server := &HostServiceServer{Deps: HostServiceDeps{PromptBroker: broker}}
		end := broker.Begin("entra", func(context.Context, string, string) (string, error) {
			t.Error("prompt must not run for a rejected authorization URL")
			return "", nil
		})
		defer end()
		_, err := server.PromptAuthResponse(context.Background(), &proto.PromptAuthResponseRequest{
			HandlerName:      "entra",
			AuthorizationUrl: "http://evil.example-not-localhost/authorize",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("rejects an empty or malformed authorization URL", func(t *testing.T) {
		t.Parallel()
		broker := &AuthPromptBroker{}
		server := &HostServiceServer{Deps: HostServiceDeps{PromptBroker: broker}}
		for _, authURL := range []string{"", "not a url", "ftp://example/auth", "https:evil.example/auth", "https:///auth"} {
			end := broker.Begin("entra", func(context.Context, string, string) (string, error) {
				t.Error("prompt must not run for a rejected authorization URL")
				return "", nil
			})
			_, err := server.PromptAuthResponse(context.Background(), &proto.PromptAuthResponseRequest{
				HandlerName:      "entra",
				AuthorizationUrl: authURL,
			})
			end()
			require.Error(t, err)
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
		}
	})

	t.Run("rejects a missing or malformed redirect URI before prompting", func(t *testing.T) {
		t.Parallel()
		broker := &AuthPromptBroker{}
		server := &HostServiceServer{Deps: HostServiceDeps{PromptBroker: broker}}
		for _, redirectURI := range []string{"", "/callback", "not a url"} {
			end := broker.Begin("entra", func(context.Context, string, string) (string, error) {
				t.Error("prompt must not run for a rejected redirect URI")
				return "", nil
			})
			_, err := server.PromptAuthResponse(context.Background(), &proto.PromptAuthResponseRequest{
				HandlerName:      "entra",
				AuthorizationUrl: "https://login.example/auth",
				RedirectUri:      redirectURI,
			})
			end()
			require.Error(t, err)
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
		}
	})

	t.Run("untrusted authorization URL maps to InvalidArgument", func(t *testing.T) {
		t.Parallel()
		broker := &AuthPromptBroker{}
		server := &HostServiceServer{Deps: HostServiceDeps{PromptBroker: broker}}
		inner := func(context.Context, string, string) (string, error) {
			t.Error("inner prompt must not run for an untrusted authorization URL")
			return "", nil
		}
		defer broker.Begin("entra", trustedPasteBack(inner, "entra", []string{"login.example"}, false))()
		_, err := server.PromptAuthResponse(context.Background(), &proto.PromptAuthResponseRequest{
			HandlerName:      "entra",
			AuthorizationUrl: "https://evil.example/auth",
			RedirectUri:      "http://localhost:8400/callback",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("accepts a loopback http authorization URL", func(t *testing.T) {
		t.Parallel()
		broker := &AuthPromptBroker{}
		server := &HostServiceServer{Deps: HostServiceDeps{PromptBroker: broker}}
		end := broker.Begin("entra", func(context.Context, string, string) (string, error) {
			return "http://localhost:8400/callback?code=x", nil
		})
		defer end()
		resp, err := server.PromptAuthResponse(context.Background(), &proto.PromptAuthResponseRequest{
			HandlerName:      "entra",
			AuthorizationUrl: "http://localhost:8400/authorize",
			RedirectUri:      "http://localhost:8400/callback",
		})
		require.NoError(t, err)
		assert.Equal(t, "http://localhost:8400/callback?code=x", resp.Value)
	})

	t.Run("canceled context maps to gRPC Canceled", func(t *testing.T) {
		t.Parallel()
		broker := &AuthPromptBroker{}
		server := &HostServiceServer{Deps: HostServiceDeps{PromptBroker: broker}}
		defer broker.Begin("entra", func(ctx context.Context, _, _ string) (string, error) {
			return "", ctx.Err()
		})()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := server.PromptAuthResponse(ctx, &proto.PromptAuthResponseRequest{
			HandlerName:      "entra",
			AuthorizationUrl: "https://login.example/auth",
			RedirectUri:      "http://localhost:8400/callback",
		})
		require.Error(t, err)
		assert.Equal(t, codes.Canceled, status.Code(err))
	})

	t.Run("prompt failure maps to Internal without leaking the value", func(t *testing.T) {
		t.Parallel()
		broker := &AuthPromptBroker{}
		server := &HostServiceServer{Deps: HostServiceDeps{PromptBroker: broker}}
		readErr := errors.New("read failure")
		end := broker.Begin("entra", func(context.Context, string, string) (string, error) {
			return "", readErr
		})
		defer end()
		_, err := server.PromptAuthResponse(context.Background(), &proto.PromptAuthResponseRequest{
			HandlerName:      "entra",
			AuthorizationUrl: "https://login.example/auth",
			RedirectUri:      "http://localhost:8400/callback",
		})
		require.Error(t, err)
		assert.Equal(t, codes.Internal, status.Code(err))
	})
}

func TestHostServiceServer_PromptAuthResponse_ValidationErrorDoesNotLeak(t *testing.T) {
	t.Parallel()

	broker := &AuthPromptBroker{}
	server := &HostServiceServer{Deps: HostServiceDeps{PromptBroker: broker}}
	end := broker.Begin("entra", func(context.Context, string, string) (string, error) {
		return "http://evil.example/callback?code=SECRETCODE", nil
	})
	defer end()

	_, err := server.PromptAuthResponse(context.Background(), &proto.PromptAuthResponseRequest{
		HandlerName:      "entra",
		AuthorizationUrl: "https://login.example/auth",
		RedirectUri:      "http://localhost:8400/callback",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.NotContains(t, err.Error(), "SECRETCODE", "error text must not echo the pasted value")
}

func TestValidatePastedRedirectURL(t *testing.T) {
	t.Parallel()

	errCases := []struct {
		name        string
		value       string
		redirectURI string
	}{
		{"empty value", "", "http://localhost:8400/callback"},
		{"too long", strings.Repeat("a", promptPasteMaxLen+1), ""},
		{"not a url", "just-text", ""},
		{"relative url", "/callback?code=x", ""},
		{"scheme mismatch", "https://localhost:8400/callback?code=x", "http://localhost:8400/callback"},
		{"host mismatch", "http://localhost:9999/callback?code=x", "http://localhost:8400/callback"},
		{"path mismatch", "http://localhost:8400/other?code=x", "http://localhost:8400/callback"},
	}
	for _, tc := range errCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Error(t, validatePastedRedirectURL(tc.value, tc.redirectURI))
		})
	}

	// A missing or malformed expected prefix must fail closed rather than
	// disable the redirect guard.
	for _, redirectURI := range []string{"", ":", "/callback", "https:///cb"} {
		assert.Error(t, validatePastedRedirectURL("https://other.example/done?code=x", redirectURI), redirectURI)
	}

	okCases := []struct {
		name        string
		value       string
		redirectURI string
	}{
		{"exact match with query", "http://localhost:8400/callback?code=x&state=y", "http://localhost:8400/callback"},
		{"trailing slash tolerance", "http://localhost:8400/callback/?code=x", "http://localhost:8400/callback"},
	}
	for _, tc := range okCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.NoError(t, validatePastedRedirectURL(tc.value, tc.redirectURI))
		})
	}
}

func TestPromptAuthResponse_NeverLogged(t *testing.T) {
	t.Parallel()

	// A capturing logr sink records everything the RPC context emits; the
	// pasted value (which carries an authorization code) must not appear
	// in it at any verbosity.
	var logBuf bytes.Buffer
	lgr := funcr.New(func(_, val string) { logBuf.WriteString(val + "\n") }, funcr.Options{Verbosity: 10})
	ctx := logger.WithLogger(context.Background(), &lgr)

	broker := &AuthPromptBroker{}
	server := &HostServiceServer{Deps: HostServiceDeps{PromptBroker: broker}}
	pasted := "http://localhost:8400/callback?code=TOPSECRET"
	end := broker.Begin("entra", func(context.Context, string, string) (string, error) {
		return pasted, nil
	})
	defer end()

	resp, err := server.PromptAuthResponse(ctx, &proto.PromptAuthResponseRequest{
		HandlerName:      "entra",
		AuthorizationUrl: "https://login.example/auth",
		RedirectUri:      "http://localhost:8400/callback",
	})
	require.NoError(t, err)
	assert.Equal(t, pasted, resp.Value, "the value reaches only the RPC response")
	assert.NotContains(t, logBuf.String(), "TOPSECRET", "pasted value never logged, even at debug verbosity")
	assert.NotContains(t, logBuf.String(), pasted, "pasted value never logged, even at debug verbosity")
}

// TestAuthHandlerGRPCClient_ConfigureAuthHandler_AdvertisesHostCapabilities
// asserts the configure request carries the host capability advertisement:
// plugin handlers consult it to rank interactive flows ahead of device code
// in sessions where the browser redirect may not reach this machine.
func TestAuthHandlerGRPCClient_ConfigureAuthHandler_AdvertisesHostCapabilities(t *testing.T) {
	t.Parallel()

	t.Run("prompt capability advertised when the host serves it", func(t *testing.T) {
		t.Parallel()
		mock := &mockAuthHandlerServiceClient{
			configureAuthHandlerResp: &proto.ConfigureAuthHandlerResponse{ProtocolVersion: PluginProtocolVersion},
		}
		client := &AuthHandlerGRPCClient{client: mock, supportsPromptAuth: true}

		err := client.ConfigureAuthHandler(context.Background(), "entra", ProviderConfig{})
		require.NoError(t, err)
		require.NotNil(t, mock.lastConfigureReq.HostCapabilities)
		assert.True(t, mock.lastConfigureReq.HostCapabilities.PromptAuthResponse)
	})

	t.Run("prompt capability not advertised when the host lacks it", func(t *testing.T) {
		t.Parallel()
		mock := &mockAuthHandlerServiceClient{
			configureAuthHandlerResp: &proto.ConfigureAuthHandlerResponse{ProtocolVersion: PluginProtocolVersion},
		}
		client := &AuthHandlerGRPCClient{client: mock}

		err := client.ConfigureAuthHandler(context.Background(), "entra", ProviderConfig{})
		require.NoError(t, err)
		require.NotNil(t, mock.lastConfigureReq.HostCapabilities)
		assert.False(t, mock.lastConfigureReq.HostCapabilities.PromptAuthResponse)
	})
}

func TestHostDepsFromAuthRegistry_WiresPromptBroker(t *testing.T) {
	t.Parallel()

	authReg := auth.NewRegistry()
	deps := HostDepsFromAuthRegistry(authReg)
	require.NotNil(t, deps)
	assert.NotNil(t, deps.PromptBroker, "auth-host deps carry a prompt broker")

	assert.Nil(t, HostDepsFromAuthRegistry(nil))
}

// BenchmarkValidatePastedRedirectURL covers the per-paste validation cost on
// the hot path of every paste-back response.
func BenchmarkValidatePastedRedirectURL(b *testing.B) {
	paste := "http://localhost:8400/callback?code=a-long-authorization-code&state=some-state"
	redirect := "http://localhost:8400/callback"
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := validatePastedRedirectURL(paste, redirect); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkAuthPromptBrokerBeginPrompt covers the gate bookkeeping wrapped
// around every plugin login.
func BenchmarkAuthPromptBrokerBeginPrompt(b *testing.B) {
	broker := &AuthPromptBroker{}
	fn := func(context.Context, string, string) (string, error) { return "", nil }
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		end := broker.Begin("entra", fn)
		_, _ = broker.Prompt("entra")
		end()
	}
}

// TestAuthHandlerWrapper_LoginRegistersPromptWindow asserts the login wrapper
// opens the PromptAuthResponse window for exactly the duration of the
// plugin's Login RPC and serves the prompt the login UI installed into ctx.
func TestAuthHandlerWrapper_LoginRegistersPromptWindow(t *testing.T) {
	t.Parallel()

	broker := &AuthPromptBroker{}
	promptCalled := false
	ctx := auth.WithPasteBack(context.Background(), func(ctx context.Context, _, _ string) (string, error) {
		promptCalled = true
		return "http://localhost:8400/callback?code=abc", nil
	})

	client := &AuthHandlerClient{
		plugin: &MockAuthHandlerPlugin{
			loginFunc: func(context.Context, string, LoginRequest, func(DeviceCodePrompt)) (*LoginResponse, error) {
				fn, ok := broker.Prompt("entra")
				require.True(t, ok, "window open during Login")
				require.NotNil(t, fn, "interactive prompt installed from ctx")
				_, _ = fn(context.Background(), "https://login.example/auth", "http://localhost:8400/callback")
				return &LoginResponse{}, nil
			},
		},
		hostDeps: &HostServiceDeps{PromptBroker: broker},
	}
	wrapper := NewAuthHandlerWrapper(client, AuthHandlerInfo{Name: "entra", DisplayName: "Entra ID"})

	_, err := wrapper.Login(ctx, auth.LoginOptions{Flow: auth.FlowInteractive})
	require.NoError(t, err)
	assert.True(t, promptCalled, "no prompt was served during Login")

	_, ok := broker.Prompt("entra")
	assert.False(t, ok, "window closed after Login returns")
}

func TestAuthHandlerWrapper_LoginWindowNilPromptWhenNotInteractive(t *testing.T) {
	t.Parallel()

	broker := &AuthPromptBroker{}
	client := &AuthHandlerClient{
		plugin: &MockAuthHandlerPlugin{
			loginFunc: func(context.Context, string, LoginRequest, func(DeviceCodePrompt)) (*LoginResponse, error) {
				fn, ok := broker.Prompt("entra")
				assert.True(t, ok, "window open during Login")
				assert.Nil(t, fn, "non-interactive session carries a nil prompt")
				return &LoginResponse{}, nil
			},
		},
		hostDeps: &HostServiceDeps{PromptBroker: broker},
	}
	wrapper := NewAuthHandlerWrapper(client, AuthHandlerInfo{Name: "entra", DisplayName: "Entra ID"})

	_, err := wrapper.Login(context.Background(), auth.LoginOptions{})
	require.NoError(t, err)
	_, ok := broker.Prompt("entra")
	assert.False(t, ok, "window closed after Login returns")
}

// TestAuthHandlerWrapper_LoginNoBrokerIsNoop asserts a client without host
// deps (no broker) logs in normally without the paste-back gate.
func TestAuthHandlerWrapper_LoginNoBroker(t *testing.T) {
	t.Parallel()

	client := &AuthHandlerClient{plugin: &MockAuthHandlerPlugin{}}
	wrapper := NewAuthHandlerWrapper(client, AuthHandlerInfo{Name: "entra", DisplayName: "Entra ID"})
	_, err := wrapper.Login(auth.WithPasteBack(context.Background(), func(context.Context, string, string) (string, error) {
		return "pasted", nil
	}), auth.LoginOptions{})
	require.NoError(t, err)
}

func TestTrustedPasteBack(t *testing.T) {
	t.Parallel()

	assert.Nil(t, trustedPasteBack(nil, "h", nil, true), "nil prompt stays nil")

	called := false
	inner := func(context.Context, string, string) (string, error) {
		called = true
		return "ok", nil
	}
	cases := []struct {
		name    string
		trusted []string
		require bool
		authURL string
		wantErr bool
	}{
		{"no policy allows any", nil, false, "https://any.example/a", false},
		{"exact domain", []string{"login.example"}, false, "https://login.example/a", false},
		{"subdomain", []string{"example"}, false, "https://login.example/a", false},
		{"case and trailing dot", []string{"Login.Example."}, false, "https://login.example./a", false},
		{"outside trusted list", []string{"login.example"}, false, "https://evil.example/a", true},
		{"suffix lookalike", []string{"login.example"}, false, "https://badlogin.example/a", true},
		{"hostless", []string{"login.example"}, false, "https:///a", true},
		{"required but unconfigured", nil, true, "https://login.example/a", true},
	}
	for _, tc := range cases {
		called = false
		v, err := trustedPasteBack(inner, "h", tc.trusted, tc.require)(context.Background(), tc.authURL, "http://localhost/cb")
		if tc.wantErr {
			require.ErrorIs(t, err, ErrUntrustedAuthorizationURL, tc.name)
			assert.False(t, called, tc.name)
			continue
		}
		require.NoError(t, err, tc.name)
		assert.Equal(t, "ok", v, tc.name)
	}
}

func TestIsolatePromptBroker(t *testing.T) {
	t.Parallel()

	orig := &AuthPromptBroker{}
	shared := &HostServiceDeps{PromptBroker: orig}
	a := &clientOptions{hostDeps: shared}
	b := &clientOptions{hostDeps: shared}
	a.isolatePromptBroker()
	b.isolatePromptBroker()
	require.NotNil(t, a.hostDeps.PromptBroker)
	assert.NotSame(t, a.hostDeps.PromptBroker, b.hostDeps.PromptBroker, "each client gets its own broker")
	assert.NotSame(t, shared.PromptBroker, a.hostDeps.PromptBroker)
	assert.Same(t, orig, shared.PromptBroker, "input deps untouched")

	none := &clientOptions{hostDeps: &HostServiceDeps{}}
	none.isolatePromptBroker()
	assert.Nil(t, none.hostDeps.PromptBroker, "no broker stays nil")
	(&clientOptions{}).isolatePromptBroker() // nil deps: no panic
}
