// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package loginui

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

// mockHandler is a minimal auth.Handler for exercising the plain-text login
// path. Only Login, Name, and DisplayName carry behaviour; the rest return
// zero values.
type mockHandler struct {
	name      string
	loginFunc func(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error)
}

func (m *mockHandler) Name() string        { return m.name }
func (m *mockHandler) DisplayName() string { return m.name }

func (m *mockHandler) Login(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
	return m.loginFunc(ctx, opts)
}

func (m *mockHandler) Logout(_ context.Context) error                 { return nil }
func (m *mockHandler) Status(_ context.Context) (*auth.Status, error) { return &auth.Status{}, nil }

func (m *mockHandler) GetToken(_ context.Context, _ auth.TokenOptions) (*auth.Token, error) {
	return &auth.Token{}, nil
}

func (m *mockHandler) InjectAuth(_ context.Context, _ *http.Request, _ auth.TokenOptions) error {
	return nil
}

func (m *mockHandler) SupportedFlows() []auth.Flow     { return nil }
func (m *mockHandler) Capabilities() []auth.Capability { return nil }

func newTestWriter(t *testing.T) *writer.Writer {
	t.Helper()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	return writer.New(ioStreams, settings.NewCliParams())
}

func TestRunLogin_PlainSuccess(t *testing.T) {
	t.Parallel()

	callbackFired := false
	handler := &mockHandler{
		name: "gcp",
		loginFunc: func(_ context.Context, opts auth.LoginOptions) (*auth.Result, error) {
			// The plain path installs a device-code callback that prints
			// instructions; exercise it.
			if opts.DeviceCodeCallback != nil {
				callbackFired = true
				opts.DeviceCodeCallback("CODE123", "https://verify.example", "")
			}
			return &auth.Result{Claims: &auth.Claims{Username: "alice"}}, nil
		},
	}

	w := newTestWriter(t)
	result, err := RunLogin(context.Background(), w, "scafctl", handler, auth.LoginOptions{Flow: auth.FlowDeviceCode})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "alice", result.Claims.DisplayIdentity())
	assert.True(t, callbackFired, "plain path should invoke the device-code callback")
}

func TestRunLogin_PlainError(t *testing.T) {
	t.Parallel()

	loginErr := errors.New("boom")
	handler := &mockHandler{
		name: "gcp",
		loginFunc: func(_ context.Context, _ auth.LoginOptions) (*auth.Result, error) {
			return nil, loginErr
		},
	}

	w := newTestWriter(t)
	result, err := RunLogin(context.Background(), w, "scafctl", handler, auth.LoginOptions{Flow: auth.FlowServicePrincipal})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorIs(t, err, loginErr)
	assert.NotErrorIs(t, err, auth.ErrUserCancelled)
}

func TestRunLogin_PlainCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	handler := &mockHandler{
		name: "gcp",
		loginFunc: func(ctx context.Context, _ auth.LoginOptions) (*auth.Result, error) {
			return nil, ctx.Err()
		},
	}

	w := newTestWriter(t)
	_, err := RunLogin(ctx, w, "scafctl", handler, auth.LoginOptions{Flow: auth.FlowServicePrincipal})
	require.Error(t, err)
	assert.ErrorIs(t, err, auth.ErrUserCancelled)
}

// TestRunStatusTUI_EarlyCompletion exercises the device-code TUI path when the
// login finishes before a device code is surfaced, which returns without ever
// launching the interactive TUI (untestable without a PTY).
func TestRunStatusTUI_EarlyCompletion(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		handler := &mockHandler{
			name: "gcp",
			loginFunc: func(_ context.Context, _ auth.LoginOptions) (*auth.Result, error) {
				return &auth.Result{Claims: &auth.Claims{Username: "alice"}}, nil
			},
		}
		w := newTestWriter(t)
		result, err := runStatusTUI(context.Background(), w, "scafctl", handler, auth.LoginOptions{}, w.IOStreams())
		require.NoError(t, err)
		assert.Equal(t, "alice", result.Claims.DisplayIdentity())
	})

	t.Run("error", func(t *testing.T) {
		t.Parallel()
		loginErr := errors.New("boom")
		handler := &mockHandler{
			name: "gcp",
			loginFunc: func(_ context.Context, _ auth.LoginOptions) (*auth.Result, error) {
				return nil, loginErr
			},
		}
		w := newTestWriter(t)
		_, err := runStatusTUI(context.Background(), w, "scafctl", handler, auth.LoginOptions{}, w.IOStreams())
		require.Error(t, err)
		assert.ErrorIs(t, err, loginErr)
	})
}

// TestRunBrowserTUI_EarlyCompletion exercises the browser TUI path when the
// login finishes before any TUI is shown.
func TestRunBrowserTUI_EarlyCompletion(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		handler := &mockHandler{
			name: "gcp",
			loginFunc: func(_ context.Context, _ auth.LoginOptions) (*auth.Result, error) {
				return &auth.Result{Claims: &auth.Claims{Username: "bob"}}, nil
			},
		}
		w := newTestWriter(t)
		result, err := runBrowserTUI(context.Background(), w, "scafctl", handler, auth.LoginOptions{}, w.IOStreams())
		require.NoError(t, err)
		assert.Equal(t, "bob", result.Claims.DisplayIdentity())
	})

	t.Run("error", func(t *testing.T) {
		t.Parallel()
		loginErr := errors.New("boom")
		handler := &mockHandler{
			name: "gcp",
			loginFunc: func(_ context.Context, _ auth.LoginOptions) (*auth.Result, error) {
				return nil, loginErr
			},
		}
		w := newTestWriter(t)
		_, err := runBrowserTUI(context.Background(), w, "scafctl", handler, auth.LoginOptions{}, w.IOStreams())
		require.Error(t, err)
		assert.ErrorIs(t, err, loginErr)
	})
}

func TestLoginIdentity(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "alice", loginIdentity(&auth.Result{Claims: &auth.Claims{Username: "alice"}}))
	assert.Equal(t, "unknown user", loginIdentity(&auth.Result{}))
}

func TestLoginError(t *testing.T) {
	t.Parallel()

	// Cancelled context maps to ErrUserCancelled regardless of the wrapped error.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.ErrorIs(t, loginError(ctx, errors.New("ignored")), auth.ErrUserCancelled)

	// Live context wraps the underlying error as an authentication failure.
	underlying := errors.New("bad token")
	err := loginError(context.Background(), underlying)
	assert.ErrorIs(t, err, underlying)
	assert.NotErrorIs(t, err, auth.ErrUserCancelled)
}

func TestBrowserTUISchema(t *testing.T) {
	t.Parallel()

	t.Run("device code", func(t *testing.T) {
		t.Parallel()
		data, schema := browserTUISchema("GitHub", true, deviceCodeData{userCode: "ABC", verificationURI: "https://verify"})
		assert.Equal(t, "ABC", data["code"])
		assert.Equal(t, "https://verify", data["url"])
		assert.Equal(t, "Waiting for authentication...", schema.Status.WaitMessage)
		assert.Len(t, schema.Status.Actions, 2)
	})

	t.Run("browser auth url", func(t *testing.T) {
		t.Parallel()
		data, schema := browserTUISchema("GitHub", true, deviceCodeData{verificationURI: "https://auth"})
		_, hasCode := data["code"]
		assert.False(t, hasCode)
		assert.Equal(t, "https://auth", data["url"])
		assert.Equal(t, "Waiting for browser authentication...", schema.Status.WaitMessage)
		assert.Len(t, schema.Status.Actions, 1)
	})

	t.Run("minimal browser", func(t *testing.T) {
		t.Parallel()
		data, schema := browserTUISchema("GitHub", false, deviceCodeData{})
		assert.Equal(t, "Sign in to GitHub", data["title"])
		_, hasURL := data["url"]
		assert.False(t, hasURL)
		assert.Empty(t, schema.Status.Actions)
		assert.Empty(t, schema.Status.DisplayFields)
	})
}
