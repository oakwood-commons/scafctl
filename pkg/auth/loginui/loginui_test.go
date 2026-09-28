// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package loginui

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
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

// TestRunLogin_PlainDeviceCodeCallback asserts the live behavior of the
// plain renderer's device-code callback: the code line is printed only when
// the handler actually surfaced a user code (browser/interactive logins send
// an authorization URL with no code).
func TestRunLogin_PlainDeviceCodeCallback(t *testing.T) {
	t.Parallel()

	run := func(t *testing.T, flow auth.Flow, userCode string) string {
		t.Helper()
		ioStreams, outBuf, _ := terminal.NewTestIOStreams()
		w := writer.New(ioStreams, settings.NewCliParams())
		handler := &mockHandler{
			name: "entra",
			loginFunc: func(_ context.Context, opts auth.LoginOptions) (*auth.Result, error) {
				if opts.DeviceCodeCallback != nil {
					opts.DeviceCodeCallback(userCode, "https://verify.example", "")
				}
				return &auth.Result{}, nil
			},
		}
		_, err := RunLogin(context.Background(), w, "scafctl", handler, auth.LoginOptions{Flow: flow})
		require.NoError(t, err)
		return outBuf.String()
	}

	t.Run("device code with user code prints the code line", func(t *testing.T) {
		t.Parallel()
		out := run(t, auth.FlowDeviceCode, "CODE123")
		assert.Contains(t, out, "Enter the code: CODE123")
		assert.Contains(t, out, "https://verify.example")
	})

	t.Run("browser flow without user code omits the code line", func(t *testing.T) {
		t.Parallel()
		out := run(t, auth.FlowInteractive, "")
		assert.NotContains(t, out, "Enter the code:")
		assert.Contains(t, out, "https://verify.example")
		assert.Contains(t, out, "Waiting for authentication...")
	})
}

// TestRunLogin_InteractiveRendersPlain asserts interactive (browser) flows
// take the plain-text path: the device-code callback prints instructions
// and no paste-back prompt is offered on a non-interactive session.
func TestRunLogin_InteractiveRendersPlain(t *testing.T) {
	t.Parallel()

	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, settings.NewCliParams())

	pasteBackSeen := false
	handler := &mockHandler{
		name: "entra",
		loginFunc: func(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
			// Non-interactive test streams: the login UI must not have
			// installed a paste-back prompt.
			if auth.PasteBackFromContext(ctx) != nil {
				pasteBackSeen = true
			}
			if opts.DeviceCodeCallback != nil {
				opts.DeviceCodeCallback("", "https://auth.example", "")
			}
			return &auth.Result{Claims: &auth.Claims{Username: "alice"}}, nil
		},
	}

	result, err := RunLogin(context.Background(), w, "scafctl", handler, auth.LoginOptions{Flow: auth.FlowInteractive})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, pasteBackSeen, "no paste-back prompt on non-interactive sessions")
	assert.Contains(t, outBuf.String(), "https://auth.example")
}

// TestNewPasteBackPrompt_Render asserts the host-authored prompt block: the
// authorization URL, the paste-back instruction, and nothing else before the
// read.
func TestNewPasteBackPrompt_Render(t *testing.T) {
	t.Parallel()

	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, settings.NewCliParams())

	prompt := newPasteBackPrompt(w, ioStreams)
	authURL := "https://login.example/authorize?client_id=abc"
	_, err := prompt(context.Background(), authURL, "http://localhost:8400/callback")

	// The test streams hold no TTY, so the read fails -- but only after the
	// prompt text was rendered.
	require.Error(t, err)
	out := outBuf.String()
	assert.Contains(t, out, "Open this URL in your browser:")
	assert.Contains(t, out, authURL)
	assert.Contains(t, out, pasteBackPromptText)

	// Cancellation surfaces as the context error rather than a read error.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = prompt(ctx, authURL, "")
	assert.ErrorIs(t, err, context.Canceled)
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

func TestInteractiveTerminal_DevNullIsNotTTY(t *testing.T) {
	t.Parallel()

	devNull, err := os.Open(os.DevNull)
	require.NoError(t, err)
	t.Cleanup(func() { _ = devNull.Close() })

	// /dev/null is a character device but not a terminal: paste-back must
	// stay off so the prompt is never written somewhere invisible.
	assert.False(t, interactiveTerminal(&terminal.IOStreams{In: devNull, Out: devNull, ErrOut: devNull}))
	assert.False(t, isTTY(&bytes.Buffer{}))
}
