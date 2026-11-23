// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package loginui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/oakwood-commons/kvx/pkg/core"
	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

// mockHandler is a minimal auth.Handler for exercising login presentation paths.
type mockHandler struct {
	name        string
	displayName string
	loginFunc   func(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error)
}

func (m *mockHandler) Name() string { return m.name }
func (m *mockHandler) DisplayName() string {
	if m.displayName != "" {
		return m.displayName
	}
	return m.name
}

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
		assert.Contains(t, out, waitingMessage)
	})

	t.Run("block is unstyled plain text without icon noise", func(t *testing.T) {
		t.Parallel()
		out := run(t, auth.FlowInteractive, "")
		assert.NotContains(t, out, "💡")
		assert.Equal(t, 1, strings.Count(out, signInHeading))
		lines := strings.Split(strings.TrimSpace(out), "\n")
		require.NotEmpty(t, lines)
		assert.Equal(t, signInHeading, lines[0])
		assert.Equal(t, "  https://verify.example", lines[1])
	})
}

func TestRunLogin_PlainURLOncePerRun(t *testing.T) {
	t.Parallel()

	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, settings.NewCliParams())
	handler := &mockHandler{
		name: "entra",
		loginFunc: func(_ context.Context, opts auth.LoginOptions) (*auth.Result, error) {
			if opts.DeviceCodeCallback != nil {
				opts.DeviceCodeCallback("", "https://verify.example", "")
				opts.DeviceCodeCallback("", "https://verify.example", "")
			}
			return &auth.Result{}, nil
		},
	}

	_, err := RunLogin(context.Background(), w, "scafctl", handler, auth.LoginOptions{Flow: auth.FlowInteractive})
	require.NoError(t, err)
	out := outBuf.String()
	assert.Equal(t, 1, strings.Count(out, "https://verify.example"))
	assert.Equal(t, 1, strings.Count(out, signInHeading))
	assert.Equal(t, 1, strings.Count(out, waitingMessage))
}

func TestRunLogin_PlainDeviceCodeFallbackPrints(t *testing.T) {
	t.Parallel()

	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, settings.NewCliParams())
	handler := &mockHandler{
		name: "entra",
		loginFunc: func(_ context.Context, opts auth.LoginOptions) (*auth.Result, error) {
			opts.DeviceCodeCallback("", "https://auth.example/authorize", "")
			opts.DeviceCodeCallback("WXYZ-1234", "https://verify.example/device", "")
			return &auth.Result{}, nil
		},
	}

	_, err := RunLogin(context.Background(), w, "scafctl", handler, auth.LoginOptions{Flow: auth.FlowInteractive})
	require.NoError(t, err)
	out := outBuf.String()
	assert.Equal(t, 1, strings.Count(out, "https://auth.example/authorize"))
	assert.Equal(t, 1, strings.Count(out, "https://verify.example/device"))
	assert.Contains(t, out, "Enter the code: WXYZ-1234")
}

func TestRunLogin_InteractiveRendersPlainOnNonTTY(t *testing.T) {
	t.Parallel()

	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, settings.NewCliParams())

	pasteBackSeen := false
	handler := &mockHandler{
		name: "entra",
		loginFunc: func(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
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

func TestRunLogin_PlainDeviceCodeNonTTYUsesUnifiedCopy(t *testing.T) {
	t.Parallel()

	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, settings.NewCliParams())
	handler := &mockHandler{
		name: "entra",
		loginFunc: func(_ context.Context, opts auth.LoginOptions) (*auth.Result, error) {
			opts.DeviceCodeCallback("", "https://auth.example/authorize?x=1", "")
			return &auth.Result{}, nil
		},
	}

	_, err := RunLogin(context.Background(), w, "scafctl", handler, auth.LoginOptions{Flow: auth.FlowDeviceCode})
	require.NoError(t, err)
	out := outBuf.String()
	assert.Contains(t, out, signInHeading)
	assert.Contains(t, out, "  https://auth.example/authorize?x=1")
	assert.Contains(t, out, waitingMessage)
}

func TestRunLogin_QuietInteractiveStaysSilent(t *testing.T) {
	t.Parallel()

	ioStreams, outBuf, errBuf := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	cliParams.IsQuiet = true
	w := writer.New(ioStreams, cliParams)

	handler := &mockHandler{
		name: "entra",
		loginFunc: func(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
			if auth.PasteBackFromContext(ctx) != nil {
				t.Error("quiet session must not receive a paste-back prompt")
			}
			if opts.DeviceCodeCallback != nil {
				opts.DeviceCodeCallback("", "https://auth.example", "")
			}
			return &auth.Result{}, nil
		},
	}

	_, err := RunLogin(context.Background(), w, "scafctl", handler, auth.LoginOptions{Flow: auth.FlowInteractive})
	require.NoError(t, err)
	assert.Empty(t, outBuf.String())
	assert.Empty(t, errBuf.String())
}

func TestRunInteractiveTUI_EarlyCompletion(t *testing.T) {
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
		result, err := runInteractiveTUI(context.Background(), w, "scafctl", handler, auth.LoginOptions{}, w.IOStreams())
		require.NoError(t, err)
		require.NotNil(t, result)
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
		_, err := runInteractiveTUI(context.Background(), w, "scafctl", handler, auth.LoginOptions{}, w.IOStreams())
		require.Error(t, err)
		assert.ErrorIs(t, err, loginErr)
	})
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func stubInteractiveRunner(t *testing.T, binaryName string, handler *mockHandler, stub func(any, tui.Config, ...tea.ProgramOption) error) (wait func() (*auth.Result, error), out *syncBuffer) {
	t.Helper()
	waitFn, outBuf, _ := stubInteractiveRunnerWithStreams(t, binaryName, handler, stub)
	return waitFn, outBuf
}

func stubInteractiveRunnerWithStreams(t *testing.T, binaryName string, handler *mockHandler, stub func(any, tui.Config, ...tea.ProgramOption) error) (wait func() (*auth.Result, error), out, errOut *syncBuffer) {
	t.Helper()
	out = &syncBuffer{}
	errOut = &syncBuffer{}
	ioStreams := &terminal.IOStreams{
		In:     io.NopCloser(bytes.NewReader(nil)),
		Out:    out,
		ErrOut: errOut,
	}
	w := writer.New(ioStreams, settings.NewCliParams())

	prev := runStatusScreen
	runStatusScreen = stub
	t.Cleanup(func() { runStatusScreen = prev })

	var (
		result *auth.Result
		err    error
	)
	doneRun := make(chan struct{})
	go func() {
		defer close(doneRun)
		result, err = runInteractiveTUI(context.Background(), w, binaryName, handler, auth.LoginOptions{Flow: auth.FlowInteractive}, w.IOStreams())
	}()
	return func() (*auth.Result, error) {
		select {
		case <-doneRun:
		case <-time.After(5 * time.Second):
			t.Fatal("runner did not return; test gates left open?")
		}
		return result, err
	}, out, errOut
}

// TestRunInteractiveTUI_StaysActiveWhilePasteBackParked verifies that a plugin
// calling PromptAuthResponse immediately alongside its localhost callback
// server does not tear down or skip the kvx status TUI: the TUI stays active,
// displays a concise scheme://host/path summary while Copy URL / Open URL stay
// bound to the full URL, and flashes "Authenticated as ..." when the browser
// callback finishes first.
func TestRunInteractiveTUI_StaysActiveWhilePasteBackParked(t *testing.T) {
	fullAuthURL := "https://login.example.com/common/oauth2/v2.0/authorize?client_id=abc&redirect_uri=http%3A%2F%2Flocalhost%3A45189&code_challenge=xyz&state=st1"
	outcomeGate := make(chan struct{})
	pasteErrCh := make(chan error, 1)

	handler := &mockHandler{
		name:        "entra",
		displayName: "Microsoft Entra ID",
		loginFunc: func(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
			pasteCtx, cancelPaste := context.WithCancel(ctx)
			defer cancelPaste()

			prompt := auth.PasteBackFromContext(ctx)
			if prompt != nil {
				go func() {
					_, err := prompt(pasteCtx, fullAuthURL, "http://localhost:45189")
					pasteErrCh <- err
				}()
			}
			opts.DeviceCodeCallback("", fullAuthURL, "")

			select {
			case <-outcomeGate:
				// Simulate localhost browser callback arriving first and cancelling pasteCtx.
				cancelPaste()
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return &auth.Result{Claims: &auth.Claims{Email: "alice@example.com"}}, nil
		},
	}

	tuiUp := make(chan struct{})
	stub := func(data any, cfg tui.Config, _ ...tea.ProgramOption) error {
		m := data.(map[string]any)
		assert.Equal(t, "Sign in to Microsoft Entra ID", m["title"])
		assert.Equal(t, fullAuthURL, m["url"], "full authorization URL preserved for Copy/Open actions")
		assert.Equal(t, "https://login.example.com/common/oauth2/v2.0/authorize", m["endpoint"], "concise endpoint summary strips PKCE query")

		require.NotNil(t, cfg.DisplaySchema)
		require.NotNil(t, cfg.DisplaySchema.Status)
		st := cfg.DisplaySchema.Status
		require.Len(t, st.DisplayFields, 1)
		assert.Equal(t, "URL", st.DisplayFields[0].Label)
		assert.Equal(t, "endpoint", st.DisplayFields[0].Field)
		require.Len(t, st.Actions, 2)
		assert.Equal(t, "Copy URL", st.Actions[0].Label)
		assert.Equal(t, "url", st.Actions[0].Field)
		assert.Equal(t, "Open URL", st.Actions[1].Label)
		assert.Equal(t, "url", st.Actions[1].Field)
		require.NotNil(t, st.Input, "inline status input must be configured")
		assert.True(t, st.Input.Masked, "inline status input must be masked")
		assert.NotEmpty(t, st.Input.Label)
		assert.NotEmpty(t, st.Input.Placeholder)
		require.NotNil(t, cfg.OnStatusInput, "OnStatusInput must be wired")
		assert.Equal(t, "mycli", cfg.AppName, "embedder binary name forwarded to kvx")

		close(tuiUp)
		select {
		case r := <-cfg.Done:
			assert.NoError(t, r.Err)
			assert.Equal(t, "Authenticated as alice@example.com", r.Message)
		case <-time.After(5 * time.Second):
			return errors.New("timed out waiting for status completion")
		}
		return nil
	}

	wait, out := stubInteractiveRunner(t, "mycli", handler, stub)
	<-tuiUp
	close(outcomeGate)

	result, err := wait()
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "alice@example.com", result.Claims.DisplayIdentity())
	assert.Empty(t, out.String(), "no plain-text URL or paste prompt printed on stdout")

	select {
	case pasteErr := <-pasteErrCh:
		assert.ErrorIs(t, pasteErr, context.Canceled, "parked PasteBackFunc unblocks cleanly when browser callback finishes")
	case <-time.After(5 * time.Second):
		t.Fatal("PasteBackFunc goroutine did not unblock")
	}
}

// TestRunInteractiveTUI_InlinePastePreValidationAndRetry verifies that invalid
// pastes submitted via OnStatusInput return a validation error without
// consuming the one-shot PasteBackFunc, allowing inline retry until a valid
// loopback redirect URL (including scheme-less Chromium pastes) is submitted.
func TestRunInteractiveTUI_InlinePastePreValidationAndRetry(t *testing.T) {
	fullAuthURL := "https://login.example.com/authorize?client_id=abc&state=xyz"

	handler := &mockHandler{
		name: "entra",
		loginFunc: func(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
			opts.DeviceCodeCallback("", fullAuthURL, "")
			prompt := auth.PasteBackFromContext(ctx)
			if prompt == nil {
				return nil, errors.New("expected PasteBackFunc in context")
			}
			pasted, err := prompt(ctx, fullAuthURL, "http://localhost:45189/callback")
			if err != nil {
				return nil, err
			}
			if pasted != "http://localhost:45189/callback?code=AUTH_CODE_123&state=xyz" {
				return nil, fmt.Errorf("unexpected pasted value: %s", pasted)
			}
			return &auth.Result{Claims: &auth.Claims{Email: "remote@example.com"}}, nil
		},
	}

	stub := func(_ any, cfg tui.Config, _ ...tea.ProgramOption) error {
		// Bad pastes must return errors without unblocking PasteBackFunc, even if
		// OnStatusInput fires right as PromptAuthResponse is registering its URI.
		err := cfg.OnStatusInput("http://localhost:9999/callback?code=wrongport")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "http://localhost:45189/callback")
		require.Error(t, cfg.OnStatusInput("not-a-url"))
		require.Error(t, cfg.OnStatusInput(fullAuthURL), "pasting the authorize URL itself must be rejected")
		require.Error(t, cfg.OnStatusInput("http://localhost:45189/callback?state=only"), "missing code/error/#access_token must be rejected")
		require.Error(t, cfg.OnStatusInput("http://localhost:45189/callback-evil?code=AUTH_CODE_123"), "segment boundary lookalike must be rejected")

		// Valid scheme-less Chromium paste normalizes to http:// and resolves PasteBackFunc.
		require.NoError(t, cfg.OnStatusInput("localhost:45189/callback?code=AUTH_CODE_123&state=xyz"))

		select {
		case r := <-cfg.Done:
			assert.NoError(t, r.Err)
			assert.Equal(t, "Authenticated as remote@example.com", r.Message)
		case <-time.After(5 * time.Second):
			return errors.New("timed out waiting for completion after valid paste")
		}
		return nil
	}

	wait, out := stubInteractiveRunner(t, "scafctl", handler, stub)
	result, err := wait()
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "remote@example.com", result.Claims.DisplayIdentity())
	assert.Empty(t, out.String())
}

// TestRunInteractiveTUI_PasteOnlyHandlerStillLaunchesTUI verifies that even if
// a plugin calls PromptAuthResponse without firing DeviceCodeCallback first,
// the authorization URL from PromptAuthResponse launches the kvx status TUI.
func TestRunInteractiveTUI_PasteOnlyHandlerStillLaunchesTUI(t *testing.T) {
	authURL := "https://login.example.com/oauth/authorize?client_id=123"
	handler := &mockHandler{
		name: "entra",
		loginFunc: func(ctx context.Context, _ auth.LoginOptions) (*auth.Result, error) {
			prompt := auth.PasteBackFromContext(ctx)
			if prompt == nil {
				return nil, errors.New("missing prompt")
			}
			val, err := prompt(ctx, authURL, "http://localhost:8400/callback")
			if err != nil {
				return nil, err
			}
			if !strings.Contains(val, "code=ok") {
				return nil, errors.New("missing code")
			}
			return &auth.Result{Claims: &auth.Claims{Username: "alice"}}, nil
		},
	}

	stub := func(data any, cfg tui.Config, _ ...tea.ProgramOption) error {
		m := data.(map[string]any)
		assert.Equal(t, "https://login.example.com/oauth/authorize", m["endpoint"])
		require.NoError(t, cfg.OnStatusInput("http://localhost:8400/callback?code=ok"))
		r := <-cfg.Done
		assert.Equal(t, "Authenticated as alice", r.Message)
		return nil
	}

	wait, out := stubInteractiveRunner(t, "scafctl", handler, stub)
	result, err := wait()
	require.NoError(t, err)
	assert.Equal(t, "alice", result.Claims.DisplayIdentity())
	assert.Empty(t, out.String())
}

// TestRunInteractiveTUI_UserQuitCancelsLogin verifies that exiting the TUI
// early (Esc / Ctrl+C) cancels the in-flight login context and unblocks any
// parked PasteBackFunc with ErrUserCancelled.
func TestRunInteractiveTUI_UserQuitCancelsLogin(t *testing.T) {
	pasteUnblocked := make(chan error, 1)
	handler := &mockHandler{
		name: "entra",
		loginFunc: func(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
			opts.DeviceCodeCallback("", "https://auth.example/authorize?client_id=1", "")
			prompt := auth.PasteBackFromContext(ctx)
			if prompt != nil {
				go func() {
					_, err := prompt(ctx, "https://auth.example/authorize?client_id=1", "http://localhost:8400/callback")
					pasteUnblocked <- err
				}()
			}
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	stub := func(_ any, _ tui.Config, _ ...tea.ProgramOption) error {
		return nil // simulate user pressing Esc / Ctrl+C
	}

	wait, out := stubInteractiveRunner(t, "scafctl", handler, stub)
	_, err := wait()
	require.Error(t, err)
	assert.ErrorIs(t, err, auth.ErrUserCancelled)
	assert.Empty(t, out.String(), "user quit must not dump a plain URL block")

	select {
	case pErr := <-pasteUnblocked:
		assert.ErrorIs(t, pErr, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("parked PasteBackFunc did not unblock on TUI quit")
	}
}

func TestRunInteractiveTUI_DeviceCodeResolvedByHandler(t *testing.T) {
	outcomeGate := make(chan struct{})
	handler := &mockHandler{
		name: "github",
		loginFunc: func(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
			opts.DeviceCodeCallback("ABCD-EFGH", "https://verify.example/device", "")
			select {
			case <-outcomeGate:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return &auth.Result{Claims: &auth.Claims{Username: "bob"}}, nil
		},
	}

	tuiUp := make(chan struct{})
	stub := func(data any, cfg tui.Config, _ ...tea.ProgramOption) error {
		m := data.(map[string]any)
		assert.Equal(t, "ABCD-EFGH", m["code"])
		st := cfg.DisplaySchema.Status
		require.Len(t, st.DisplayFields, 2)
		assert.Equal(t, "code", st.DisplayFields[1].Field)
		assert.Equal(t, "code", st.Actions[0].Field)
		assert.Nil(t, st.Input, "device-code fallback has no redirect paste input")
		assert.Nil(t, cfg.OnStatusInput)
		close(tuiUp)
		r := <-cfg.Done
		assert.Equal(t, "Authenticated as bob", r.Message)
		return nil
	}

	wait, _ := stubInteractiveRunner(t, "scafctl", handler, stub)
	<-tuiUp
	close(outcomeGate)

	result, err := wait()
	require.NoError(t, err)
	assert.Equal(t, "bob", result.Claims.DisplayIdentity())
}

func TestRunInteractiveTUI_TUIStartErrorFallsBackToPlain(t *testing.T) {
	outcomeGate := make(chan struct{})
	handler := &mockHandler{
		name: "entra",
		loginFunc: func(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
			opts.DeviceCodeCallback("", "https://auth.example/authorize", "")
			select {
			case <-outcomeGate:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return &auth.Result{Claims: &auth.Claims{Username: "alice"}}, nil
		},
	}

	tuiUp := make(chan struct{})
	stub := func(_ any, _ tui.Config, _ ...tea.ProgramOption) error {
		close(tuiUp)
		return errors.New("tui boom")
	}

	wait, out := stubInteractiveRunner(t, "scafctl", handler, stub)
	<-tuiUp
	require.Eventually(t, func() bool {
		return strings.Contains(out.String(), signInHeading)
	}, 5*time.Second, 5*time.Millisecond)
	close(outcomeGate)

	result, err := wait()
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "alice", result.Claims.DisplayIdentity())
	outStr := out.String()
	assert.Contains(t, outStr, signInHeading)
	assert.Contains(t, outStr, "https://auth.example/authorize")
	assert.Contains(t, outStr, waitingMessage)
	assert.NotContains(t, outStr, "tui boom")
}

// TestRunInteractiveTUI_TUIStartErrorPlainPasteFallback verifies that when the
// status TUI fails to start during a remote or paste-only login, PasteBackFunc
// switches to hidden plain-text terminal input, warns and retries on invalid
// pastes without echoing secrets, and completes authentication on a valid paste.
func TestRunInteractiveTUI_TUIStartErrorPlainPasteFallback(t *testing.T) {
	authURL := "https://login.example.com/oauth2/v2.0/authorize?client_id=abc&state=xyz"
	handler := &mockHandler{
		name: "entra",
		loginFunc: func(ctx context.Context, _ auth.LoginOptions) (*auth.Result, error) {
			prompt := auth.PasteBackFromContext(ctx)
			if prompt == nil {
				return nil, errors.New("expected PasteBackFunc in context")
			}
			val, err := prompt(ctx, authURL, "http://localhost:45189/callback")
			if err != nil {
				return nil, err
			}
			if val != "http://localhost:45189/callback?code=REMOTE_CODE_1&state=xyz" {
				return nil, fmt.Errorf("unexpected normalized paste: %s", val)
			}
			return &auth.Result{Claims: &auth.Claims{Email: "remote@example.com"}}, nil
		},
	}

	var calls int
	prevReader := readPlainPasteLine
	readPlainPasteLine = func(_ context.Context, _ io.Reader, _ io.Writer) (string, error) {
		calls++
		if calls == 1 {
			return "https://login.example.com/oauth2/v2.0/authorize?code=SECRET_BAD", nil
		}
		return "localhost:45189/callback?code=REMOTE_CODE_1&state=xyz", nil
	}
	t.Cleanup(func() { readPlainPasteLine = prevReader })

	stub := func(_ any, _ tui.Config, _ ...tea.ProgramOption) error {
		return errors.New("tui startup failed")
	}

	wait, out, errOut := stubInteractiveRunnerWithStreams(t, "scafctl", handler, stub)
	result, err := wait()
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "remote@example.com", result.Claims.DisplayIdentity())
	assert.Equal(t, 2, calls)

	outStr := out.String()
	assert.Equal(t, 1, strings.Count(outStr, signInHeading))
	assert.Equal(t, 1, strings.Count(outStr, authURL))
	assert.Equal(t, 1, strings.Count(outStr, pasteBackPromptText))

	errStr := errOut.String()
	assert.Contains(t, errStr, "Invalid redirect URL")
	assert.NotContains(t, errStr, "SECRET_BAD", "validation warnings must never echo pasted values")
}

// TestRunInteractiveTUI_TUIStartErrorUnblocksPasteBackWhenInputUnavailable
// verifies that when the status TUI fails to start and plain-text terminal
// line input is unavailable, PasteBackFunc fails immediately with the TUI
// startup error instead of blocking indefinitely on OnStatusInput, while still
// allowing a handler to fall back to device code on the plain screen.
func TestRunInteractiveTUI_TUIStartErrorUnblocksPasteBackWhenInputUnavailable(t *testing.T) {
	t.Run("paste-only handler fails promptly with TUI startup error", func(t *testing.T) {
		tuiErr := errors.New("terminal raw mode failed")
		handler := &mockHandler{
			name: "entra",
			loginFunc: func(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
				opts.DeviceCodeCallback("", "https://auth.example/authorize", "")
				prompt := auth.PasteBackFromContext(ctx)
				if prompt == nil {
					return nil, errors.New("expected PasteBackFunc in context")
				}
				_, err := prompt(ctx, "https://auth.example/authorize", "http://localhost:8400/callback")
				return nil, err
			},
		}

		stub := func(_ any, _ tui.Config, _ ...tea.ProgramOption) error {
			return tuiErr
		}

		wait, out := stubInteractiveRunner(t, "scafctl", handler, stub)
		_, err := wait()
		require.Error(t, err)
		assert.ErrorIs(t, err, tuiErr)
		assert.NotContains(t, out.String(), pasteBackPromptText, "dead paste prompt must not print when line input is unavailable")
	})

	t.Run("handler falling back to device code after prompt error prints device code", func(t *testing.T) {
		tuiErr := errors.New("terminal raw mode failed")
		handler := &mockHandler{
			name: "entra",
			loginFunc: func(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
				opts.DeviceCodeCallback("", "https://auth.example/authorize", "")
				prompt := auth.PasteBackFromContext(ctx)
				if prompt == nil {
					return nil, errors.New("expected PasteBackFunc in context")
				}
				if _, err := prompt(ctx, "https://auth.example/authorize", "http://localhost:8400/callback"); err == nil {
					return nil, errors.New("expected PasteBackFunc to fail when plain input is unavailable")
				}
				opts.DeviceCodeCallback("WXYZ-9876", "https://verify.example/device", "")
				return &auth.Result{Claims: &auth.Claims{Username: "fallback-user"}}, nil
			},
		}

		stub := func(_ any, _ tui.Config, _ ...tea.ProgramOption) error {
			return tuiErr
		}

		wait, out := stubInteractiveRunner(t, "scafctl", handler, stub)
		result, err := wait()
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "fallback-user", result.Claims.DisplayIdentity())
		assert.Contains(t, out.String(), "Enter the code: WXYZ-9876")
	})
}

// TestRunInteractiveTUI_TUIStartErrorLocalCallbackCancelsPlainPaste verifies
// that when the status TUI fails to start and plain-text paste fallback is
// waiting for terminal input, a local browser callback cancelling pasteCtx
// unblocks the plain-text reader cleanly and completes authentication.
func TestRunInteractiveTUI_TUIStartErrorLocalCallbackCancelsPlainPaste(t *testing.T) {
	outcomeGate := make(chan struct{})
	pasteErrCh := make(chan error, 1)
	readerWaiting := make(chan struct{})

	prevReader := readPlainPasteLine
	readPlainPasteLine = func(ctx context.Context, _ io.Reader, _ io.Writer) (string, error) {
		close(readerWaiting)
		<-ctx.Done()
		return "", ctx.Err()
	}
	t.Cleanup(func() { readPlainPasteLine = prevReader })

	handler := &mockHandler{
		name: "entra",
		loginFunc: func(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
			pasteCtx, cancelPaste := context.WithCancel(ctx)
			defer cancelPaste()

			opts.DeviceCodeCallback("", "https://auth.example/authorize", "")
			prompt := auth.PasteBackFromContext(ctx)
			if prompt != nil {
				go func() {
					_, err := prompt(pasteCtx, "https://auth.example/authorize", "http://localhost:8400/callback")
					pasteErrCh <- err
				}()
			}

			select {
			case <-outcomeGate:
				cancelPaste()
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return &auth.Result{Claims: &auth.Claims{Username: "local-user"}}, nil
		},
	}

	stub := func(_ any, _ tui.Config, _ ...tea.ProgramOption) error {
		return errors.New("tui boom")
	}

	wait, out := stubInteractiveRunner(t, "scafctl", handler, stub)
	select {
	case <-readerWaiting:
	case <-time.After(5 * time.Second):
		t.Fatal("plain paste fallback reader was never entered")
	}
	close(outcomeGate)

	result, err := wait()
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "local-user", result.Claims.DisplayIdentity())
	assert.Contains(t, out.String(), pasteBackPromptText)

	select {
	case pErr := <-pasteErrCh:
		assert.ErrorIs(t, pErr, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("plain paste fallback did not unblock on pasteCtx cancellation")
	}
}

// TestRunInteractiveTUI_TUIStartErrorReaderFailurePropagates verifies that a
// plain-text terminal reader failure after a TUI startup error wraps both
// errors and unblocks the login immediately.
func TestRunInteractiveTUI_TUIStartErrorReaderFailurePropagates(t *testing.T) {
	tuiErr := errors.New("tui init failed")
	readErr := errors.New("termios read failed")

	prevReader := readPlainPasteLine
	readPlainPasteLine = func(_ context.Context, _ io.Reader, _ io.Writer) (string, error) {
		return "", readErr
	}
	t.Cleanup(func() { readPlainPasteLine = prevReader })

	handler := &mockHandler{
		name: "entra",
		loginFunc: func(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
			opts.DeviceCodeCallback("", "https://auth.example/authorize", "")
			prompt := auth.PasteBackFromContext(ctx)
			_, err := prompt(ctx, "https://auth.example/authorize", "http://localhost:8400/callback")
			return nil, err
		},
	}

	stub := func(_ any, _ tui.Config, _ ...tea.ProgramOption) error {
		return tuiErr
	}

	wait, _ := stubInteractiveRunner(t, "scafctl", handler, stub)
	_, err := wait()
	require.Error(t, err)
	assert.ErrorIs(t, err, tuiErr)
	assert.ErrorIs(t, err, readErr)
}

func TestRunInteractiveTUI_CancelledWithoutOutcome(t *testing.T) {
	t.Parallel()

	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, settings.NewCliParams())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	handler := &mockHandler{
		name: "entra",
		loginFunc: func(ctx context.Context, _ auth.LoginOptions) (*auth.Result, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	_, err := runInteractiveTUI(ctx, w, "scafctl", handler, auth.LoginOptions{}, w.IOStreams())
	require.Error(t, err)
	assert.ErrorIs(t, err, auth.ErrUserCancelled)
	assert.Empty(t, outBuf.String())
}

// TestBuildInteractiveStatusTUI_Snapshot verifies the rendered kvx status
// screen snapshot at 100 columns: concise endpoint URL (without raw PKCE
// query clutter), waiting spinner, frame-1 auto-focused inline input label +
// placeholder, and safe modifier/function action keys in the footer.
func TestBuildInteractiveStatusTUI_Snapshot(t *testing.T) {
	done := make(chan tui.StatusResult, 1)
	ps := newPasteSession()
	fullURL := "https://login.example.com/common/oauth2/v2.0/authorize?client_id=00000000-0000-0000-0000-000000000000&code_challenge=SUPERLONGPKCECHALLENGE&state=SECRETSTATE"
	cfg, data := buildInteractiveStatusTUI("scafctl", "Microsoft Entra ID", deviceCodeData{verificationURI: fullURL}, done, func(raw string) error {
		return ps.submit(context.Background(), raw)
	})
	cfg.Width = 100
	cfg.Height = 20
	cfg.NoColor = true

	root, err := core.LoadObject(data)
	require.NoError(t, err)
	snap := tui.RenderSnapshot(root, cfg)

	assert.Contains(t, snap, "Sign in to Microsoft Entra ID")
	assert.Contains(t, snap, "https://login.example.com/common/oauth2/v2.0/authorize")
	assert.NotContains(t, snap, "SUPERLONGPKCECHALLENGE", "raw PKCE query parameters must not clutter the status card")
	assert.Contains(t, snap, waitingMessage)
	assert.Contains(t, snap, pasteInputLabel)
	assert.Contains(t, snap, pasteInputPlaceholder)
	assert.Contains(t, snap, "Copy URL")
	assert.Contains(t, snap, "Open URL")
}

func TestValidateStatusPasteInput(t *testing.T) {
	t.Parallel()

	okCases := []struct {
		name        string
		raw         string
		expectedURI string
		want        string
	}{
		{
			name:        "full http localhost with code",
			raw:         "http://localhost:45189/callback?code=abc&state=xyz",
			expectedURI: "http://localhost:45189/callback",
			want:        "http://localhost:45189/callback?code=abc&state=xyz",
		},
		{
			name:        "scheme-less localhost port from Chromium",
			raw:         "  localhost:45189/?code=abc&state=xyz  ",
			expectedURI: "http://localhost:45189",
			want:        "http://localhost:45189/?code=abc&state=xyz",
		},
		{
			name:        "uppercase scheme-less LOCALHOST normalized",
			raw:         "LOCALHOST:8400/callback?code=MixedCaseCode",
			expectedURI: "http://localhost:8400/callback",
			want:        "http://localhost:8400/callback?code=MixedCaseCode",
		},
		{
			name:        "uppercase full URL LOCALHOST canonicalized",
			raw:         "http://LOCALHOST:8400/callback?code=MixedCaseCode",
			expectedURI: "http://localhost:8400/callback",
			want:        "http://localhost:8400/callback?code=MixedCaseCode",
		},
		{
			name:        "scheme-less 127.0.0.2 loopback IP normalized",
			raw:         "127.0.0.2:8400/callback?code=abc",
			expectedURI: "http://127.0.0.2:8400/callback",
			want:        "http://127.0.0.2:8400/callback?code=abc",
		},
		{
			name:        "scheme-less 127.0.0.1 with error param",
			raw:         "127.0.0.1:8400/callback?error=access_denied",
			expectedURI: "http://127.0.0.1:8400/callback",
			want:        "http://127.0.0.1:8400/callback?error=access_denied",
		},
		{
			name:        "scheme-less IPv6 loopback with fragment access_token",
			raw:         "[::1]:8400/callback#access_token=tok123&token_type=Bearer",
			expectedURI: "http://[::1]:8400/callback",
			want:        "http://[::1]:8400/callback#access_token=tok123&token_type=Bearer",
		},
		{
			name:        "no expectedRedirectURI yet still validates loopback and code",
			raw:         "localhost:51234/?code=early",
			expectedURI: "",
			want:        "http://localhost:51234/?code=early",
		},
	}

	for _, tc := range okCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := validateStatusPasteInput(tc.raw, tc.expectedURI)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	errCases := []struct {
		name        string
		raw         string
		expectedURI string
	}{
		{"empty", "   ", "http://localhost:8400/callback"},
		{"exceeds max length", "http://localhost:8400/cb?code=" + strings.Repeat("x", maxPasteRedirectURLLen), ""},
		{"plain garbage", "not-a-url-SECRET123", ""},
		{"external authorization URL", "https://login.example.com/oauth2/authorize?code=SECRET123", ""},
		{"non-loopback IP", "http://192.168.1.10:8400/callback?code=SECRET123", ""},
		{"userinfo spoofing", "http://evil@localhost:8400/callback?code=SECRET123", ""},
		{"missing code error and access_token", "http://localhost:8400/callback?state=SECRET123", ""},
		{"empty code value", "http://localhost:8400/callback?code=&state=SECRET123", ""},
		{"empty fragment access_token", "http://localhost:8400/callback#access_token=", ""},
		{"port mismatch against expectedRedirectURI", "http://localhost:9999/callback?code=SECRET123", "http://localhost:8400/callback"},
		{"path mismatch against expectedRedirectURI", "http://localhost:8400/other?code=SECRET123", "http://localhost:8400/callback"},
		{"scheme-less rejected when expectedRedirectURI uses https", "localhost:8400/callback?code=SECRET123", "https://localhost:8400/callback"},
	}

	for _, tc := range errCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := validateStatusPasteInput(tc.raw, tc.expectedURI)
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "SECRET123", "validation errors must never echo pasted secrets")
		})
	}
}

func TestSummarizeAuthorizationURL(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "", summarizeAuthorizationURL("  "))
	assert.Equal(t,
		"https://login.example.com/tenant/oauth2/v2.0/authorize",
		summarizeAuthorizationURL("https://login.example.com/tenant/oauth2/v2.0/authorize?client_id=1&state=secret#frag"),
	)
	assert.Equal(t,
		"https://auth.example.com",
		summarizeAuthorizationURL("https://auth.example.com/?client_id=1"),
	)
	assert.Equal(t,
		"not-abs",
		summarizeAuthorizationURL("not-abs?secret=1"),
	)
}

func TestPasteSession_ActivationAndDuplicateSubmit(t *testing.T) {
	t.Parallel()

	ps := newPasteSession()
	// Before PromptAuthResponse activates, submissions fail cleanly without locking the channel.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.Error(t, ps.submit(ctx, "http://localhost:8400/cb?code=premature"))

	deactivate := ps.activate("http://localhost:8400/cb")
	require.NoError(t, ps.submit(context.Background(), "http://localhost:8400/cb?code=first"))
	err := ps.submit(context.Background(), "http://localhost:8400/cb?code=second")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already been submitted")

	deactivate()
	err = ps.submit(context.Background(), "http://localhost:8400/cb?code=afterclose")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not active")
}

func BenchmarkValidateStatusPasteInput(b *testing.B) {
	raw := "localhost:45189/callback?code=0.AXEA_some_long_authorization_code&state=some_state"
	expected := "http://localhost:45189/callback"
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := validateStatusPasteInput(raw, expected); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSummarizeAuthorizationURL(b *testing.B) {
	raw := "https://login.example.com/common/oauth2/v2.0/authorize?client_id=00000000-0000-0000-0000-000000000000&redirect_uri=http%3A%2F%2Flocalhost%3A45189&code_challenge=xyz&state=abc"
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if got := summarizeAuthorizationURL(raw); got == "" {
			b.Fatal("empty summary")
		}
	}
}

func TestCaptureOutcome_ForwardsToDone(t *testing.T) {
	t.Parallel()

	t.Run("success forwards an authenticated-as message", func(t *testing.T) {
		t.Parallel()
		outcomeChan := make(chan loginOutcome, 1)
		done := make(chan tui.StatusResult, 2)
		outcomeChan <- loginOutcome{result: &auth.Result{Claims: &auth.Claims{Username: "alice"}}}
		oc := captureOutcome(outcomeChan, done)

		var forwarded *tui.StatusResult
		require.Eventually(t, func() bool {
			select {
			case r, ok := <-done:
				if ok {
					forwarded = &r
				}
				return true
			default:
				return false
			}
		}, 5*time.Second, 5*time.Millisecond)
		require.NotNil(t, forwarded)
		assert.Equal(t, fmt.Sprintf(successMessageFmt, "alice"), forwarded.Message)
		assert.NoError(t, forwarded.Err)
		require.True(t, oc.closed())

		result, err := oc.wait(context.Background())
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "alice", result.Claims.DisplayIdentity())
	})

	t.Run("error outcome is forwarded and mapped to a login failure", func(t *testing.T) {
		t.Parallel()
		outcomeChan := make(chan loginOutcome, 1)
		done := make(chan tui.StatusResult, 2)
		loginErr := errors.New("boom")
		outcomeChan <- loginOutcome{err: loginErr}
		oc := captureOutcome(outcomeChan, done)

		var forwarded *tui.StatusResult
		require.Eventually(t, func() bool {
			select {
			case r, ok := <-done:
				if ok {
					forwarded = &r
				}
				return true
			default:
				return false
			}
		}, 5*time.Second, 5*time.Millisecond)
		require.NotNil(t, forwarded)
		assert.ErrorIs(t, forwarded.Err, loginErr)
		require.True(t, oc.closed())

		_, err := oc.wait(context.Background())
		require.Error(t, err)
		assert.ErrorIs(t, err, loginErr)
		assert.NotErrorIs(t, err, auth.ErrUserCancelled)
	})

	t.Run("a full done buffer drops the send but keeps the outcome", func(t *testing.T) {
		t.Parallel()
		outcomeChan := make(chan loginOutcome, 1)
		done := make(chan tui.StatusResult, 1)
		done <- tui.StatusResult{Message: "occupied"}
		outcomeChan <- loginOutcome{result: &auth.Result{Claims: &auth.Claims{Username: "alice"}}}
		oc := captureOutcome(outcomeChan, done)

		require.Eventually(t, oc.closed, 5*time.Second, 10*time.Millisecond)
		select {
		case r := <-done:
			assert.Equal(t, "occupied", r.Message)
		default:
			t.Fatal("pre-queued result expected")
		}

		result, err := oc.wait(context.Background())
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "alice", result.Claims.DisplayIdentity())
	})
}

func TestCaptureOutcome_WaitCancelled(t *testing.T) {
	t.Parallel()

	outcomeChan := make(chan loginOutcome, 1)
	done := make(chan tui.StatusResult, 2)
	oc := captureOutcome(outcomeChan, done)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := oc.wait(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, auth.ErrUserCancelled)
}

func TestLoginIdentity(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "alice", loginIdentity(&auth.Result{Claims: &auth.Claims{Username: "alice"}}))
	assert.Equal(t, "unknown user", loginIdentity(&auth.Result{}))
}

func TestLoginError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.ErrorIs(t, loginError(ctx, errors.New("ignored")), auth.ErrUserCancelled)

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

	assert.False(t, interactiveTerminal(&terminal.IOStreams{In: devNull, Out: devNull, ErrOut: devNull}))
	assert.False(t, isTTY(&bytes.Buffer{}))
}
