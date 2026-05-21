// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"errors"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLazyAuthHandlerWrapper_Name(t *testing.T) {
	lazy := NewLazyAuthHandlerWrapper(LazyAuthHandlerConfig{
		Name:    "github",
		BinPath: "/fake/path",
	})

	assert.Equal(t, "github", lazy.Name())
	assert.False(t, lazy.IsInitialized())
}

func TestLazyAuthHandlerWrapper_DisplayName_BeforeInit(t *testing.T) {
	lazy := NewLazyAuthHandlerWrapper(LazyAuthHandlerConfig{
		Name:    "entra",
		BinPath: "/fake/path",
	})

	// Before initialization, display name falls back to handler name.
	assert.Equal(t, "entra", lazy.DisplayName())
}

func TestLazyAuthHandlerWrapper_SupportedFlows_InvalidBinary_ReturnsNil(t *testing.T) {
	lazy := NewLazyAuthHandlerWrapper(LazyAuthHandlerConfig{
		Name:    "gcp",
		BinPath: "/fake/path",
	})

	// Plugin binary doesn't exist — init fails, returns nil.
	assert.Nil(t, lazy.SupportedFlows())
}

func TestLazyAuthHandlerWrapper_Capabilities_InvalidBinary_ReturnsNil(t *testing.T) {
	lazy := NewLazyAuthHandlerWrapper(LazyAuthHandlerConfig{
		Name:    "gcp",
		BinPath: "/fake/path",
	})

	assert.Nil(t, lazy.Capabilities())
}

func TestLazyAuthHandlerWrapper_Client_BeforeInit(t *testing.T) {
	lazy := NewLazyAuthHandlerWrapper(LazyAuthHandlerConfig{
		Name:    "github",
		BinPath: "/fake/path",
	})

	assert.Nil(t, lazy.Client())
}

func TestLazyAuthHandlerWrapper_InitError_PropagatesOnLogin(t *testing.T) {
	initErr := errors.New("binary not found")
	lazy := &LazyAuthHandlerWrapper{
		name:        "broken",
		displayName: "broken",
		initFn: func(_ context.Context) (*AuthHandlerWrapper, error) {
			return nil, initErr
		},
	}

	_, err := lazy.Login(context.Background(), auth.LoginOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "binary not found")
	assert.True(t, lazy.IsInitialized() == false || lazy.wrapper.Load() == nil)
}

func TestLazyAuthHandlerWrapper_InitError_PropagatesOnStatus(t *testing.T) {
	initErr := errors.New("plugin crashed")
	lazy := &LazyAuthHandlerWrapper{
		name:        "broken",
		displayName: "broken",
		initFn: func(_ context.Context) (*AuthHandlerWrapper, error) {
			return nil, initErr
		},
	}

	_, err := lazy.Status(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plugin crashed")
}

func TestLazyAuthHandlerWrapper_InitError_PropagatesOnGetToken(t *testing.T) {
	lazy := &LazyAuthHandlerWrapper{
		name:        "broken",
		displayName: "broken",
		initFn: func(_ context.Context) (*AuthHandlerWrapper, error) {
			return nil, errors.New("timeout")
		},
	}

	_, err := lazy.GetToken(context.Background(), auth.TokenOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
}

func TestLazyAuthHandlerWrapper_InitError_PropagatesOnLogout(t *testing.T) {
	lazy := &LazyAuthHandlerWrapper{
		name:        "broken",
		displayName: "broken",
		initFn: func(_ context.Context) (*AuthHandlerWrapper, error) {
			return nil, errors.New("not available")
		},
	}

	err := lazy.Logout(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not available")
}

func TestLazyAuthHandlerWrapper_InitError_PropagatesOnListCachedTokens(t *testing.T) {
	lazy := &LazyAuthHandlerWrapper{
		name:        "broken",
		displayName: "broken",
		initFn: func(_ context.Context) (*AuthHandlerWrapper, error) {
			return nil, errors.New("startup failed")
		},
	}

	_, err := lazy.ListCachedTokens(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "startup failed")
}

func TestLazyAuthHandlerWrapper_InitError_PropagatesOnPurge(t *testing.T) {
	lazy := &LazyAuthHandlerWrapper{
		name:        "broken",
		displayName: "broken",
		initFn: func(_ context.Context) (*AuthHandlerWrapper, error) {
			return nil, errors.New("gone")
		},
	}

	_, err := lazy.PurgeExpiredTokens(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gone")
}

func TestLazyAuthHandlerWrapper_InitError_PropagatesOnDetectFlows(t *testing.T) {
	lazy := &LazyAuthHandlerWrapper{
		name:        "broken",
		displayName: "broken",
		initFn: func(_ context.Context) (*AuthHandlerWrapper, error) {
			return nil, errors.New("no exec permission")
		},
	}

	_, err := lazy.DetectAvailableFlows(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no exec permission")
}

func TestLazyAuthHandlerWrapper_InitCalledOnce(t *testing.T) {
	callCount := 0
	lazy := &LazyAuthHandlerWrapper{
		name:        "counter",
		displayName: "counter",
		initFn: func(_ context.Context) (*AuthHandlerWrapper, error) {
			callCount++
			return nil, errors.New("fail")
		},
	}

	// Call multiple methods — initFn should only be invoked once.
	_, _ = lazy.Login(context.Background(), auth.LoginOptions{})
	_, _ = lazy.Status(context.Background())
	_ = lazy.Logout(context.Background())

	assert.Equal(t, 1, callCount)
}

func TestLazyAuthHandlerWrapper_InjectAuth_PropagatesError(t *testing.T) {
	lazy := &LazyAuthHandlerWrapper{
		name:        "broken",
		displayName: "broken",
		initFn: func(_ context.Context) (*AuthHandlerWrapper, error) {
			return nil, errors.New("cannot start")
		},
	}

	err := lazy.InjectAuth(context.Background(), nil, auth.TokenOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot start")
}

func TestLazyAuthHandlerWrapper_Capabilities_TriggersInit(t *testing.T) {
	wrapper := NewAuthHandlerWrapper(nil, AuthHandlerInfo{
		Name:         "entra",
		DisplayName:  "Microsoft Entra ID",
		Capabilities: []auth.Capability{auth.CapScopesOnLogin, auth.CapScopesOnTokenRequest},
	})

	initCalled := false
	lazy := &LazyAuthHandlerWrapper{
		name:        "entra",
		displayName: "entra",
		initFn: func(_ context.Context) (*AuthHandlerWrapper, error) {
			initCalled = true
			return wrapper, nil
		},
	}

	caps := lazy.Capabilities()
	assert.True(t, initCalled, "Capabilities() should trigger plugin initialization")
	assert.Contains(t, caps, auth.CapScopesOnTokenRequest)
	assert.Contains(t, caps, auth.CapScopesOnLogin)
	assert.True(t, lazy.IsInitialized())
}

func TestLazyAuthHandlerWrapper_Capabilities_InitError_ReturnsNil(t *testing.T) {
	lazy := &LazyAuthHandlerWrapper{
		name:        "broken",
		displayName: "broken",
		initFn: func(_ context.Context) (*AuthHandlerWrapper, error) {
			return nil, errors.New("plugin binary missing")
		},
	}

	caps := lazy.Capabilities()
	assert.Nil(t, caps)
}

func TestLazyAuthHandlerWrapper_SupportedFlows_TriggersInit(t *testing.T) {
	wrapper := NewAuthHandlerWrapper(nil, AuthHandlerInfo{
		Name:        "entra",
		DisplayName: "Microsoft Entra ID",
		Flows:       []auth.Flow{auth.FlowDeviceCode, auth.FlowInteractive},
	})

	initCalled := false
	lazy := &LazyAuthHandlerWrapper{
		name:        "entra",
		displayName: "entra",
		initFn: func(_ context.Context) (*AuthHandlerWrapper, error) {
			initCalled = true
			return wrapper, nil
		},
	}

	flows := lazy.SupportedFlows()
	assert.True(t, initCalled, "SupportedFlows() should trigger plugin initialization")
	assert.Contains(t, flows, auth.FlowDeviceCode)
	assert.Contains(t, flows, auth.FlowInteractive)
	assert.True(t, lazy.IsInitialized())
}

func TestLazyAuthHandlerWrapper_SupportedFlows_InitError_ReturnsNil(t *testing.T) {
	lazy := &LazyAuthHandlerWrapper{
		name:        "broken",
		displayName: "broken",
		initFn: func(_ context.Context) (*AuthHandlerWrapper, error) {
			return nil, errors.New("not installed")
		},
	}

	flows := lazy.SupportedFlows()
	assert.Nil(t, flows)
}

func TestLazyAuthHandlerWrapper_ApplyOverrides_TriggersInit(t *testing.T) {
	initCalled := false
	lazy := &LazyAuthHandlerWrapper{
		name:        "entra",
		displayName: "entra",
		initFn: func(_ context.Context) (*AuthHandlerWrapper, error) {
			initCalled = true
			return nil, errors.New("no plugin for test")
		},
	}

	err := lazy.ApplyOverrides(context.Background(), map[string]string{"clientId": "custom-id"})
	assert.True(t, initCalled, "ApplyOverrides should trigger init")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no plugin for test")
}

func TestLazyAuthHandlerWrapper_ApplyOverrides_EmptyMapSkipsInit(t *testing.T) {
	initCalled := false
	lazy := &LazyAuthHandlerWrapper{
		name:        "entra",
		displayName: "entra",
		initFn: func(_ context.Context) (*AuthHandlerWrapper, error) {
			initCalled = true
			return nil, errors.New("should not reach here")
		},
	}

	err := lazy.ApplyOverrides(context.Background(), map[string]string{})
	assert.False(t, initCalled, "ApplyOverrides with empty map should not trigger init")
	assert.NoError(t, err)

	err = lazy.ApplyOverrides(context.Background(), nil)
	assert.False(t, initCalled, "ApplyOverrides with nil map should not trigger init")
	assert.NoError(t, err)
}

func TestLazyAuthHandlerWrapper_SetContext_UsedByMetadataMethods(t *testing.T) {
	type ctxKey struct{}
	wiredCtx := context.WithValue(context.Background(), ctxKey{}, "wired")

	var receivedCtx context.Context
	lazy := &LazyAuthHandlerWrapper{
		name:        "github",
		displayName: "github",
		initFn: func(ctx context.Context) (*AuthHandlerWrapper, error) {
			receivedCtx = ctx
			return nil, errors.New("init fails for test")
		},
	}

	lazy.SetContext(wiredCtx)

	// SupportedFlows uses getBaseCtx — should receive the wired context (with timeout).
	_ = lazy.SupportedFlows()
	require.NotNil(t, receivedCtx)
	assert.Equal(t, "wired", receivedCtx.Value(ctxKey{}))
}

func TestLazyAuthHandlerWrapper_SetContext_OnlyFirstCallTakesEffect(t *testing.T) {
	type ctxKey struct{}
	first := context.WithValue(context.Background(), ctxKey{}, "first")
	second := context.WithValue(context.Background(), ctxKey{}, "second")

	var receivedCtx context.Context
	lazy := &LazyAuthHandlerWrapper{
		name:        "github",
		displayName: "github",
		initFn: func(ctx context.Context) (*AuthHandlerWrapper, error) {
			receivedCtx = ctx
			return nil, errors.New("init fails for test")
		},
	}

	lazy.SetContext(first)
	lazy.SetContext(second) // should be ignored

	_ = lazy.Capabilities()
	require.NotNil(t, receivedCtx)
	assert.Equal(t, "first", receivedCtx.Value(ctxKey{}))
}

func TestLazyAuthHandlerWrapper_SetContext_IgnoresBackgroundContext(t *testing.T) {
	lazy := &LazyAuthHandlerWrapper{
		name:        "github",
		displayName: "github",
		initFn: func(_ context.Context) (*AuthHandlerWrapper, error) {
			return nil, errors.New("init fails for test")
		},
	}

	// SetContext with context.Background() should be a no-op.
	lazy.SetContext(context.Background())
	assert.Nil(t, lazy.baseCtx)
}
