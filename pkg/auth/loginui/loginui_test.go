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
	"github.com/oakwood-commons/kvx/pkg/tui"
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

// TestRunLogin_PlainURLOncePerRun asserts the #879 dedupe floor: a handler
// that re-fires the device-code callback never prints the authorize URL
// (or the block heading) more than once per login run.
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

// TestRunLogin_InteractiveRendersPlain asserts interactive (browser) flows
// take the plain-text path on a non-interactive session: the device-code
// callback prints instructions and no paste-back prompt is offered.
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

// TestRunLogin_PlainDeviceCodeNonTTYUsesUnifiedCopy pins the non-TTY (remote
// workspace) presentation to the same unified copy as every other surface
// (#879): it is the only layout those sessions ever see.
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
	assert.NotContains(t, out, pasteBackPromptText, "no paste hint without an interactive session")
}

// TestRunLogin_QuietInteractiveStaysSilent preserves the #877 --quiet rule:
// a quiet interactive login renders no block and never installs a
// paste-back prompt that could block invisibly.
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

// TestRunInteractiveTUI_EarlyCompletion covers the hybrid runner when the
// login finishes before a URL is surfaced; it returns without ever
// launching the TUI (whose terminal interaction needs a PTY).
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

// TestRunInteractiveTUI_PasteBeforeURL covers the plain handoff taken with
// no TUI: the handler requests paste-back before surfacing the URL, so the
// prompt owns the screen, prints the unified block (heading + URL + paste
// hint) exactly once, and never launches the TUI.
func TestRunInteractiveTUI_PasteBeforeURL(t *testing.T) {
	t.Parallel()

	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, settings.NewCliParams())
	authURL := "https://login.example/authorize?client_id=abc"

	var promptErr error
	handler := &mockHandler{
		name: "entra",
		loginFunc: func(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
			prompt := auth.PasteBackFromContext(ctx)
			if prompt == nil {
				// Capture for the test goroutine; require.Goexit on the
				// login goroutine would hang the runner's select.
				promptErr = errors.New("hybrid runner installs a paste-back prompt")
				return nil, promptErr
			}
			// No DeviceCodeCallback: the URL arrives only via the prompt.
			// The test streams are not a TTY, so the read errors -- but
			// only after the paste block rendered.
			_, promptErr = prompt(ctx, authURL, "http://localhost:8400/callback")
			return &auth.Result{Claims: &auth.Claims{Username: "alice"}}, nil
		},
	}

	result, err := runInteractiveTUI(context.Background(), w, "scafctl", handler, auth.LoginOptions{}, w.IOStreams())
	require.NoError(t, err)
	require.Error(t, promptErr, "the non-TTY read must fail after rendering the block")
	require.NotNil(t, result)

	out := outBuf.String()
	assert.Equal(t, 1, strings.Count(out, authURL), "authorize URL prints exactly once")
	assert.Equal(t, 1, strings.Count(out, signInHeading))
	assert.Equal(t, 1, strings.Count(out, pasteBackPromptText))
	assert.NotContains(t, out, waitingMessage, "the prompt owns the screen; no plain waiting line")
	assert.NotContains(t, out, "💡")
}

// syncBuffer is a mutex-guarded bytes.Buffer. The stub-driven runner tests
// print from several goroutines (runner, prompt, handler), and a plain
// bytes.Buffer would race against a test-side poll of the rendered output.
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

// stubInteractiveRunner drives runInteractiveTUI with the tui.Run seam
// replaced, which makes the TUI lifecycle deterministic without a PTY. The
// returned wait function blocks until the runner finishes; only after
// calling it are the result and error safe to read. out is safe to poll
// from the test goroutine at any time (the runner prints to it from its
// own goroutine).
//
// Every stub test keeps the same invariants that make the interleavings
// race-free: the outcome stays gated until the test main goroutine has
// proof (via the stub or a poll of out) that the runner passed the URL
// select and any post-exit probe, and the paste prompt is invoked from its
// own goroutine.
//
// These tests do not run in parallel: they replace the package-level
// runStatusScreen seam.
func stubInteractiveRunner(t *testing.T, handler *mockHandler, stub func(any, tui.Config, ...tea.ProgramOption) error) (wait func() (*auth.Result, error), out *syncBuffer) {
	t.Helper()
	out = &syncBuffer{}
	errOut := &bytes.Buffer{}
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
		result, err = runInteractiveTUI(context.Background(), w, "scafctl", handler, auth.LoginOptions{Flow: auth.FlowInteractive}, w.IOStreams())
	}()
	return func() (*auth.Result, error) {
		select {
		case <-doneRun:
		case <-time.After(5 * time.Second):
			t.Fatal("runner did not return; test gates left open?")
		}
		return result, err
	}, out
}

// gatedOutcomeHandler fires the URL callback, publishes the paste prompt,
// and parks until the test closes the gate.
func gatedOutcomeHandler(promptCh chan auth.PasteBackFunc, outcomeGate chan struct{}) *mockHandler {
	return &mockHandler{
		name: "entra",
		loginFunc: func(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
			opts.DeviceCodeCallback("", "https://auth.example", "")
			select {
			case promptCh <- auth.PasteBackFromContext(ctx):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			select {
			case <-outcomeGate:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return &auth.Result{Claims: &auth.Claims{Username: "alice"}}, nil
		},
	}
}

// TestRunInteractiveTUI_UserQuitFallsBackToPlain pins the quit handoff: the
// TUI exits without the login finishing or a paste request, and the runner
// reprints the unified URL block so the URL stays visible while the login
// continues in plain text.
func TestRunInteractiveTUI_UserQuitFallsBackToPlain(t *testing.T) {
	promptCh := make(chan auth.PasteBackFunc, 1)
	outcomeGate := make(chan struct{})
	handler := gatedOutcomeHandler(promptCh, outcomeGate)

	tuiUp := make(chan struct{})
	stubEnd := make(chan struct{})
	stub := func(data any, _ tui.Config, _ ...tea.ProgramOption) error {
		assert.Equal(t, "https://auth.example", data.(map[string]any)["url"])
		assert.Equal(t, "Sign in to entra", data.(map[string]any)["title"])
		close(tuiUp)
		<-stubEnd
		return nil
	}

	wait, out := stubInteractiveRunner(t, handler, stub)

	prompt, ok := <-promptCh
	require.True(t, ok, "the hybrid runner installs a paste-back prompt")
	require.NotNil(t, prompt)
	<-tuiUp // The URL select resolved; the TUI is "running".

	// Release the "user quit" exit. The outcome stays gated, so the
	// runner's outcome probe can only resolve to "no outcome".
	close(stubEnd)

	// The fallback block printing is the park-proof: it happens after the
	// probe resolved and before the runner's outcome wait, so once the
	// block is on screen the quit handoff is complete and ungating the
	// outcome cannot flip any decision.
	require.Eventually(t, func() bool {
		rendered := out.String()
		return strings.Contains(rendered, signInHeading) && strings.Contains(rendered, waitingMessage)
	}, 5*time.Second, 5*time.Millisecond)

	close(outcomeGate)

	result, err := wait()
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "alice", result.Claims.DisplayIdentity())

	outStr := out.String()
	assert.Equal(t, 1, strings.Count(outStr, "https://auth.example"), "URL prints exactly once")
	assert.Equal(t, 1, strings.Count(outStr, signInHeading))
	assert.Equal(t, 1, strings.Count(outStr, waitingMessage))
	assert.NotContains(t, outStr, pasteBackPromptText, "no paste hint without a paste request")
	assert.NotContains(t, outStr, "💡")
}

// TestRunInteractiveTUI_TUIStartErrorFallsBackToPlain pins the degradation
// path: when the status screen cannot even start, the login continues with
// the unified plain block instead of failing.
func TestRunInteractiveTUI_TUIStartErrorFallsBackToPlain(t *testing.T) {
	promptCh := make(chan auth.PasteBackFunc, 1)
	outcomeGate := make(chan struct{})
	handler := gatedOutcomeHandler(promptCh, outcomeGate)

	tuiUp := make(chan struct{})
	stubEnd := make(chan struct{})
	stub := func(_ any, _ tui.Config, _ ...tea.ProgramOption) error {
		close(tuiUp)
		<-stubEnd
		return errors.New("tui boom")
	}

	wait, out := stubInteractiveRunner(t, handler, stub)

	<-tuiUp
	close(stubEnd)
	close(outcomeGate)

	result, err := wait()
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "alice", result.Claims.DisplayIdentity())
	outStr := out.String()
	assert.Contains(t, outStr, signInHeading)
	assert.Contains(t, outStr, "https://auth.example")
	assert.Contains(t, outStr, waitingMessage)
	assert.NotContains(t, outStr, "tui boom", "the display failure must not fail the login")
}

// TestRunInteractiveTUI_PasteDuringTUIHandsOffToPlain pins the core #879
// hybrid handoff end to end: the plugin requests paste-back while the status
// TUI is up, the TUI exits with the handoff message, and the paste prompt
// takes over the plain screen (URL block + paste hint, no fallback block).
func TestRunInteractiveTUI_PasteDuringTUIHandsOffToPlain(t *testing.T) {
	promptCh := make(chan auth.PasteBackFunc, 1)
	outcomeGate := make(chan struct{})
	handler := gatedOutcomeHandler(promptCh, outcomeGate)

	tuiUp := make(chan struct{})
	handoffSeen := make(chan struct{})
	stub := func(_ any, cfg tui.Config, _ ...tea.ProgramOption) error {
		close(tuiUp)
		// Simulate the engine: run until the paste request drives the
		// Done channel, then exit cleanly.
		select {
		case r := <-cfg.Done:
			assert.Equal(t, pasteExitMessage, r.Message)
			close(handoffSeen)
		case <-time.After(5 * time.Second):
			return errors.New("no paste exit message arrived")
		}
		return nil
	}

	wait, out := stubInteractiveRunner(t, handler, stub)

	prompt, ok := <-promptCh
	require.True(t, ok)
	require.NotNil(t, prompt)

	<-tuiUp

	pasteDone := make(chan struct{})
	go func() {
		defer close(pasteDone)
		// The test streams are not a TTY, so the read errors -- but only
		// after the paste block rendered.
		_, err := prompt(context.Background(), "https://auth.example", "http://localhost:8400/callback")
		assert.Error(t, err)
	}()

	<-handoffSeen // The TUI exited for the paste handoff.
	<-pasteDone   // The prompt rendered and its read failed (non-TTY).
	close(outcomeGate)

	result, err := wait()
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "alice", result.Claims.DisplayIdentity())

	outStr := out.String()
	assert.Equal(t, 1, strings.Count(outStr, "https://auth.example"), "URL prints exactly once across the handoff")
	assert.Equal(t, 1, strings.Count(outStr, signInHeading))
	assert.Equal(t, 1, strings.Count(outStr, pasteBackPromptText))
	assert.NotContains(t, outStr, waitingMessage, "the handoff block replaces the waiting line")
	assert.NotContains(t, outStr, "💡")
}

// TestRunInteractiveTUI_OutcomeCompletesDuringTUI pins the success handoff:
// the login finishes while the TUI is up, the outcome drives the Done
// channel ("Authenticated as ..."), and the runner returns the result
// without printing any further block.
func TestRunInteractiveTUI_OutcomeCompletesDuringTUI(t *testing.T) {
	promptCh := make(chan auth.PasteBackFunc, 1)
	outcomeGate := make(chan struct{})
	handler := gatedOutcomeHandler(promptCh, outcomeGate)

	tuiUp := make(chan struct{})
	stub := func(_ any, cfg tui.Config, _ ...tea.ProgramOption) error {
		close(tuiUp)
		select {
		case r := <-cfg.Done:
			assert.Equal(t, fmt.Sprintf(successMessageFmt, "alice"), r.Message)
		case <-time.After(5 * time.Second):
			return errors.New("no success message arrived")
		}
		return nil
	}

	wait, out := stubInteractiveRunner(t, handler, stub)

	<-tuiUp
	close(outcomeGate)

	result, err := wait()
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "alice", result.Claims.DisplayIdentity())
	assert.Empty(t, out.String(), "the success handoff prints nothing")
}

// TestRunInteractiveTUI_CancelledWithoutOutcome covers cancellation while
// the hybrid waits without a URL outcome: the runner returns
// ErrUserCancelled without launching the TUI.
func TestRunInteractiveTUI_CancelledWithoutOutcome(t *testing.T) {
	t.Parallel()

	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, settings.NewCliParams())

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel up front: without a URL the only armed channel is ctx.Done,
	// so no TUI is ever launched.
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
	assert.Empty(t, outBuf.String(), "no URL block after cancellation")
}

// TestNewPasteBackPrompt_Render asserts the host-authored paste block in
// both of its modes: when a URL block is already on the plain screen only
// the paste hint renders (#879 dedupe), and without one (the TUI handoff)
// the full block prints with the same unified heading.
func TestNewPasteBackPrompt_Render(t *testing.T) {
	t.Parallel()

	authURL := "https://login.example/authorize?client_id=abc"

	t.Run("hint only after a URL block", func(t *testing.T) {
		t.Parallel()
		ioStreams, outBuf, _ := terminal.NewTestIOStreams()
		w := writer.New(ioStreams, settings.NewCliParams())
		h := newPasteHandoff()

		// The user quit the TUI early: the runner reprinted the URL block.
		h.printURLBlockFallback(w, authURL)

		prompt := newPasteBackPrompt(w, ioStreams, h)
		_, err := prompt(context.Background(), authURL, "http://localhost:8400/callback")
		// The test streams hold no TTY, so the read fails -- but only after
		// the prompt text was rendered.
		require.Error(t, err)
		out := outBuf.String()
		assert.Equal(t, 1, strings.Count(out, authURL), "paste prompt refrains from repeating the URL")
		assert.Equal(t, 1, strings.Count(out, signInHeading))
		assert.Contains(t, out, pasteBackPromptText)
	})

	t.Run("full block when no URL was printed", func(t *testing.T) {
		t.Parallel()
		ioStreams, outBuf, _ := terminal.NewTestIOStreams()
		w := writer.New(ioStreams, settings.NewCliParams())
		h := newPasteHandoff()

		prompt := newPasteBackPrompt(w, ioStreams, h)
		_, err := prompt(context.Background(), authURL, "http://localhost:8400/callback")
		require.Error(t, err)
		out := outBuf.String()
		assert.Contains(t, out, signInHeading)
		assert.Contains(t, out, authURL)
		assert.Contains(t, out, pasteBackPromptText)
		assert.Equal(t, 1, strings.Count(out, authURL))
	})

	t.Run("cancellation surfaces as the context error", func(t *testing.T) {
		t.Parallel()
		ioStreams, _, _ := terminal.NewTestIOStreams()
		w := writer.New(ioStreams, settings.NewCliParams())
		h := newPasteHandoff()
		prompt := newPasteBackPrompt(w, ioStreams, h)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := prompt(ctx, authURL, "")
		assert.ErrorIs(t, err, context.Canceled)
	})
}

// TestPasteHandoff_RequestDrivesTUIDown pins the hybrid handoff mechanics:
// the first request makes the running TUI exit via its Done channel, later
// requests are no-ops, and markTuiDown releases a waiting prompt exactly
// once (and is idempotent).
func TestPasteHandoff_RequestDrivesTUIDown(t *testing.T) {
	t.Parallel()

	done := make(chan tui.StatusResult, 2)
	h := newPasteHandoff()
	assert.False(t, h.engaged())

	h.markTuiUp(done)
	h.requestPaste()
	h.requestPaste()

	assert.True(t, h.engaged())
	select {
	case r := <-done:
		assert.Equal(t, pasteExitMessage, r.Message)
		assert.NoError(t, r.Err)
	default:
		t.Fatal("expected the TUI Done channel to receive the exit message")
	}
	select {
	case <-done:
		t.Fatal("a second request must not send a second exit message")
	default:
	}

	h.markTuiDown()
	h.markTuiDown()
	select {
	case <-h.tuiDown:
	default:
		t.Fatal("markTuiDown must release the waiting prompt")
	}
}

// TestPasteHandoff_PlainRequestReleasesPrompt covers the no-TUI case: a
// paste request with the screen already plain releases the prompt itself.
func TestPasteHandoff_PlainRequestReleasesPrompt(t *testing.T) {
	t.Parallel()

	h := newPasteHandoff()
	h.requestPaste()
	select {
	case <-h.tuiDown:
	default:
		t.Fatal("request without a TUI must close tuiDown directly")
	}
	assert.True(t, h.engaged())

	// Marking a (never-launched) TUI down afterwards stays safe.
	h.markTuiDown()
}

// TestCaptureOutcome_ForwardsToDone pins the outcome forwarder: the login
// outcome becomes a TUI Done message (success names the identity, error
// carries the error), a full Done buffer drops the send without losing the
// captured result, and closed() reports without blocking.
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
		done := make(chan tui.StatusResult, 1) // exactly one slot
		done <- tui.StatusResult{Message: "occupied"}
		outcomeChan <- loginOutcome{result: &auth.Result{Claims: &auth.Claims{Username: "alice"}}}
		oc := captureOutcome(outcomeChan, done)

		require.Eventually(t, oc.closed, 5*time.Second, 10*time.Millisecond)
		select {
		case r := <-done:
			assert.Equal(t, "occupied", r.Message, "the dropped send must not evict the queued result")
		default:
			t.Fatal("pre-queued result expected")
		}

		result, err := oc.wait(context.Background())
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "alice", result.Claims.DisplayIdentity())
	})
}

// TestCaptureOutcome_WaitCancelled pins the cancellation arm: a cancelled
// context surfaces as ErrUserCancelled rather than a hang or a raw
// context error from the login goroutine.
func TestCaptureOutcome_WaitCancelled(t *testing.T) {
	t.Parallel()

	outcomeChan := make(chan loginOutcome, 1) // never receives
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
