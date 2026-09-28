// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package loginui renders the interactive login experience shared by the
// 'auth login' and 'kube login' commands. The device-code flow runs the kvx
// status-screen TUI when stdout is a terminal; every other flow renders
// plain-text instructions, which lets browser flows offer the paste-back
// prompt (auth.PasteBackFunc) when the redirect cannot reach this machine.
//
// RunLogin returns the login Result and never prints the final success line or
// error text — callers render their own output. This lets both commands share
// one interactive presentation while keeping their distinct result rendering.
package loginui

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/input"
	skvx "github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"golang.org/x/term"
)

// deviceCodeData carries the verification URL and user code surfaced by a
// handler's DeviceCodeCallback.
type deviceCodeData struct {
	userCode        string
	verificationURI string
}

// loginOutcome carries the result of a handler.Login call across goroutines.
type loginOutcome struct {
	result *auth.Result
	err    error
}

// RunLogin executes an interactive login for the given handler, presenting
// the device-code status TUI when running on a terminal (device-code flow
// only) and plain-text instructions for every other flow. It installs a
// SIGINT handler that cancels the login.
//
// It returns the login Result on success. Callers render their own success
// output; RunLogin does not print the final "logged in" line. Errors are
// returned wrapped with an exit code and are not printed, so callers control
// error presentation. The opts.DeviceCodeCallback is managed internally and any
// caller-supplied callback is ignored.
func RunLogin(ctx context.Context, w *writer.Writer, binaryName string, handler auth.Handler, opts auth.LoginOptions) (*auth.Result, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	go func() {
		// Watch for an interrupt, but also unblock when the login finishes so the
		// goroutine never outlives the call. signal.Stop halts delivery but does
		// not close sigChan, so without the ctx.Done() arm this would leak one
		// goroutine per invocation (loginui is a reusable library).
		select {
		case <-sigChan:
			// Diagnostics go to stderr: RunLogin is a reusable library and must
			// not corrupt a caller's structured stdout output.
			w.PlainStderr("")
			w.WarnStderr("Authentication cancelled by user.")
			cancel()
		case <-ctx.Done():
		}
	}()
	defer signal.Stop(sigChan)

	ioStreams := w.IOStreams()

	// Browser-capable flows (authorization code + PKCE and friends) render
	// in plain text rather than a full-screen TUI: the paste-back prompt
	// (auth.PasteBackFunc, bridged to HostService.PromptAuthResponse) needs
	// direct access to stdin, which a running TUI would own exclusively.
	// The device-code flow keeps its status TUI; it has no callback problem
	// to paste back from.
	var interactiveFlow bool
	switch opts.Flow {
	case auth.FlowInteractive, auth.FlowGcloudADC, auth.FlowGitHubApp, "":
		interactiveFlow = true
	case auth.FlowDeviceCode:
		if skvx.IsTerminal(ioStreams.Out) {
			return runStatusTUI(ctx, w, binaryName, handler, opts, ioStreams)
		}
	case auth.FlowServicePrincipal, auth.FlowWorkloadIdentity, auth.FlowPAT,
		auth.FlowMetadata, auth.FlowClientCredentials, auth.FlowOnBehalfOf:
		// Non-interactive flows fall through to the plain-text path below.
	}

	// Offer the paste-back prompt when this session can actually render it:
	// an interactive terminal on a platform with cancellation-safe reads.
	// Plugin-backed handlers bridge the installed function to
	// HostService.PromptAuthResponse (see AuthPromptBroker).
	if interactiveFlow && !w.IsQuiet() && interactiveTerminal(ioStreams) && input.InteractiveLineSupported() {
		ctx = auth.WithPasteBack(ctx, newPasteBackPrompt(w, ioStreams))
	}

	// Plain-text login path (non-terminal, or flows rendered as plain text).
	opts.DeviceCodeCallback = func(userCode, verificationURI, _ string) {
		w.Info("")
		w.Info("To sign in, use a web browser to open the page:")
		w.Infof("  %s", verificationURI)
		// Browser/interactive logins surface the authorization URL without
		// a user code; only device-code logins carry one.
		if userCode != "" {
			w.Info("")
			w.Infof("Enter the code: %s", userCode)
		}
		w.Info("")
		w.Info("Waiting for authentication...")
	}

	result, err := handler.Login(ctx, opts)
	if err != nil {
		if ctx.Err() != nil {
			return nil, exitcode.WithCode(auth.ErrUserCancelled, exitcode.GeneralError)
		}
		return nil, exitcode.WithCode(fmt.Errorf("authentication failed: %w", err), exitcode.GeneralError)
	}
	return result, nil
}

// runStatusTUI runs the device-code login flow using the kvx status-screen TUI.
// It:
//  1. Starts handler.Login in a goroutine with a DeviceCodeCallback that captures
//     the verification URL and user code.
//  2. Waits for the device code before launching the TUI (avoids empty-data start).
//  3. Launches tui.Run with a DisplaySchema status view and a Done channel that
//     receives the login outcome when the goroutine completes.
func runStatusTUI(
	ctx context.Context,
	w *writer.Writer,
	binaryName string,
	handler auth.Handler,
	opts auth.LoginOptions,
	ioStreams *terminal.IOStreams,
) (*auth.Result, error) {
	deviceCodeChan := make(chan deviceCodeData, 1)
	outcomeChan := make(chan loginOutcome, 1)
	done := make(chan tui.StatusResult, 1)

	opts.DeviceCodeCallback = func(userCode, verificationURI, _ string) {
		select {
		case deviceCodeChan <- deviceCodeData{userCode: userCode, verificationURI: verificationURI}:
		default:
		}
	}

	go func() {
		result, err := handler.Login(ctx, opts)
		outcomeChan <- loginOutcome{result: result, err: err}
	}()

	// Wait for the device code, an early completion, or cancellation.
	w.Verbosef("Initiating authentication with %s...", handler.DisplayName())
	var dci deviceCodeData
	select {
	case dci = <-deviceCodeChan:
		// Device code ready - proceed to TUI.
	case outcome := <-outcomeChan:
		// Login completed before device code was shown (unusual).
		if outcome.err != nil {
			return nil, loginError(ctx, outcome.err)
		}
		return outcome.result, nil
	case <-ctx.Done():
		return nil, exitcode.WithCode(auth.ErrUserCancelled, exitcode.GeneralError)
	}

	// Forward the login outcome to the TUI done channel.
	// capturedOutcome is safe to read after outcomeReady is closed because
	// close(outcomeReady) happens-before the receive on that channel.
	var capturedOutcome loginOutcome
	outcomeReady := make(chan struct{})
	go func() {
		outcome := <-outcomeChan
		capturedOutcome = outcome
		if outcome.err != nil {
			done <- tui.StatusResult{Err: outcome.err}
		} else {
			done <- tui.StatusResult{Message: "Authenticated as " + loginIdentity(outcome.result)}
		}
		close(outcomeReady)
	}()

	data := map[string]any{
		"title": fmt.Sprintf("Sign in to %s", handler.DisplayName()),
		"url":   dci.verificationURI,
		"code":  dci.userCode,
	}

	schema := &tui.DisplaySchema{
		Version: "v1",
		Status: &tui.StatusDisplayConfig{
			TitleField:     "title",
			WaitMessage:    "Waiting for authentication...",
			SuccessMessage: "Authenticated successfully!",
			DoneBehavior:   tui.DoneBehaviorExitAfterDelay,
			DoneDelay:      "2s",
			DisplayFields: []tui.StatusFieldDisplay{
				{Label: "URL", Field: "url"},
				{Label: "Code", Field: "code"},
			},
			Actions: []tui.StatusActionConfig{
				{
					Label: "Copy code",
					Type:  "copy-value",
					Field: "code",
					Keys:  tui.StatusKeyBindings{Vim: "c", Emacs: "alt+c", Function: "f2"},
				},
				{
					Label: "Open URL",
					Type:  "open-url",
					Field: "url",
					Keys:  tui.StatusKeyBindings{Vim: "o", Emacs: "alt+o", Function: "f3"},
				},
			},
		},
	}

	cfg := tui.DefaultConfig()
	cfg.AppName = binaryName
	cfg.DisplaySchema = schema
	cfg.Done = done

	teaOpts := tui.WithIO(ioStreams.In, ioStreams.Out)
	if runErr := tui.Run(data, cfg, teaOpts...); runErr != nil {
		if ctx.Err() != nil {
			return nil, exitcode.WithCode(auth.ErrUserCancelled, exitcode.GeneralError)
		}
		return nil, fmt.Errorf("authentication display failed: %w", runErr)
	}

	// TUI exited normally (done channel received, DoneDelay elapsed).
	// The outcomeReady channel should already be closed at this point.
	select {
	case <-outcomeReady:
		// Outcome is captured - continue to post-display.
	default:
		// TUI exited before login completed (user quit early) - treat as cancelled.
		return nil, exitcode.WithCode(auth.ErrUserCancelled, exitcode.GeneralError)
	}

	if capturedOutcome.err != nil {
		return nil, loginError(ctx, capturedOutcome.err)
	}
	return capturedOutcome.result, nil
}

// pasteBackPromptText is the host-authored instruction shown when an
// interactive login offers the paste-back path. By contract the prompt
// wording is written by the host only -- plugins supply URLs, never text --
// so a handler cannot use PromptAuthResponse to phish for input.
const pasteBackPromptText = "If your browser shows a connection error after sign-in, paste the full address from its address bar:"

// interactiveTerminal reports whether stdin, stdout, and stderr are all
// terminals, i.e. the session can render prompts and read answers from a
// human. stderr must be a terminal too: the paste read echoes typed bytes
// there, and a redirected stderr would capture the authorization code.
func interactiveTerminal(ioStreams *terminal.IOStreams) bool {
	if !skvx.IsTerminal(ioStreams.Out) || !skvx.IsTerminal(ioStreams.ErrOut) {
		return false
	}
	in, ok := ioStreams.In.(*os.File)
	return ok && term.IsTerminal(int(in.Fd())) //nolint:gosec // Fd() fits in int on all supported platforms
}

// newPasteBackPrompt builds the PasteBackFunc installed into interactive
// logins: it renders the host-authored paste instructions and reads the
// pasted redirect URL with a cancellation-safe terminal read (the RPC
// context cancels when the plugin's own localhost callback arrives first,
// and the read leaves no reader behind to steal the next input). The pasted
// value is never written to any log or debug output: typed characters are
// echoed to stderr only, mirroring plain terminal input.
func newPasteBackPrompt(w *writer.Writer, ioStreams *terminal.IOStreams) auth.PasteBackFunc {
	return func(ctx context.Context, authorizationURL, _ string) (string, error) {
		w.Info("")
		w.Info("Open this URL in your browser:")
		w.Infof("  %s", authorizationURL)
		w.Info("")
		w.Info(pasteBackPromptText)
		line, err := input.ReadInteractiveLine(ctx, ioStreams.In, ioStreams.ErrOut)
		if err != nil {
			if ctx.Err() != nil {
				// The callback arrived (or the login was canceled): the
				// prompt goes away and the half-typed line is discarded.
				w.PlainStderr("")
				return "", ctx.Err()
			}
			return "", fmt.Errorf("reading pasted address: %w", err)
		}
		return strings.TrimSpace(line), nil
	}
}

// loginError maps a handler.Login error to an exit-coded error, translating a
// cancelled context into ErrUserCancelled.
func loginError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return exitcode.WithCode(auth.ErrUserCancelled, exitcode.GeneralError)
	}
	return exitcode.WithCode(fmt.Errorf("authentication failed: %w", err), exitcode.GeneralError)
}

// loginIdentity returns a display identity for a login result, falling back to
// a placeholder when the claims carry no identity.
func loginIdentity(result *auth.Result) string {
	identity := result.Claims.DisplayIdentity()
	if identity == "" {
		identity = "unknown user"
	}
	return identity
}
