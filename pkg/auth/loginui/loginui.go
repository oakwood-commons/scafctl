// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package loginui renders the interactive login experience shared by the
// 'auth login' and 'kube login' commands. It runs an auth handler's login while
// presenting progress: the kvx status-screen TUI when stdout is a terminal
// (device-code and browser flows), or plain-text instructions otherwise.
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
	"time"

	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	skvx "github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

// browserCallbackWait bounds how long the browser TUI waits for a device-code
// callback before assuming a pure browser (auth-code) flow.
const browserCallbackWait = 500 * time.Millisecond

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
// progress via the kvx status TUI when running on a terminal (device-code and
// browser flows) and plain-text instructions otherwise. It installs a SIGINT
// handler that cancels the login.
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

	// Use the kvx status TUI for interactive flows when running on a terminal.
	// Non-interactive flows (SP, WI, PAT, metadata, client-credentials) fall
	// through to the plain-text login path below.
	if skvx.IsTerminal(ioStreams.Out) {
		switch opts.Flow {
		case auth.FlowDeviceCode:
			return runStatusTUI(ctx, w, binaryName, handler, opts, ioStreams)
		case auth.FlowInteractive, auth.FlowGcloudADC, auth.FlowGitHubApp, "":
			return runBrowserTUI(ctx, w, binaryName, handler, opts, ioStreams)
		case auth.FlowServicePrincipal, auth.FlowWorkloadIdentity, auth.FlowPAT,
			auth.FlowMetadata, auth.FlowClientCredentials, auth.FlowOnBehalfOf:
			// Non-interactive flows fall through to the plain-text path below.
		}
	}

	// Plain-text login path (non-terminal, or non-device-code flows).
	opts.DeviceCodeCallback = func(userCode, verificationURI, _ string) {
		w.Info("")
		w.Info("To sign in, use a web browser to open the page:")
		w.Infof("  %s", verificationURI)
		w.Info("")
		w.Infof("Enter the code: %s", userCode)
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
	if runErr := skvx.WithRenderLock(func() error { return tui.Run(data, cfg, teaOpts...) }); runErr != nil {
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

// runBrowserTUI runs the auth-code (browser) login flow using the kvx status
// screen TUI. It also sets a DeviceCodeCallback so that handlers which
// internally use device code within a FlowInteractive (e.g., GitHub without
// client_secret) get the rich device-code TUI instead.
func runBrowserTUI(
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

	// Wait briefly for a device code callback. If the handler internally
	// uses device code (e.g., GitHub FlowInteractive without client_secret),
	// we want to show the device-code TUI instead of the browser TUI.
	w.Verbosef("Initiating authentication with %s...", handler.DisplayName())

	var useDeviceCodeTUI bool
	var dci deviceCodeData
	select {
	case dci = <-deviceCodeChan:
		useDeviceCodeTUI = true
	case outcome := <-outcomeChan:
		// Login completed before any TUI was shown.
		if outcome.err != nil {
			return nil, loginError(ctx, outcome.err)
		}
		return outcome.result, nil
	case <-time.After(browserCallbackWait):
		// No device code within the wait window — assume browser flow.
		useDeviceCodeTUI = false
	case <-ctx.Done():
		return nil, exitcode.WithCode(auth.ErrUserCancelled, exitcode.GeneralError)
	}

	// Forward the login outcome to the TUI done channel.
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

	data, schema := browserTUISchema(handler.DisplayName(), useDeviceCodeTUI, dci)

	cfg := tui.DefaultConfig()
	cfg.AppName = binaryName
	cfg.DisplaySchema = schema
	cfg.Done = done

	teaOpts := tui.WithIO(ioStreams.In, ioStreams.Out)
	if runErr := skvx.WithRenderLock(func() error { return tui.Run(data, cfg, teaOpts...) }); runErr != nil {
		if ctx.Err() != nil {
			return nil, exitcode.WithCode(auth.ErrUserCancelled, exitcode.GeneralError)
		}
		return nil, fmt.Errorf("authentication display failed: %w", runErr)
	}

	// TUI exited normally.
	select {
	case <-outcomeReady:
		// Outcome is captured.
	default:
		return nil, exitcode.WithCode(auth.ErrUserCancelled, exitcode.GeneralError)
	}

	if capturedOutcome.err != nil {
		return nil, loginError(ctx, capturedOutcome.err)
	}
	return capturedOutcome.result, nil
}

// browserTUISchema builds the TUI data and display schema for the browser login
// flow, adapting to whether a device code, a browser auth URL, or neither was
// surfaced by the handler.
func browserTUISchema(displayName string, useDeviceCodeTUI bool, dci deviceCodeData) (map[string]any, *tui.DisplaySchema) {
	switch {
	case useDeviceCodeTUI && dci.userCode != "":
		// Real device code flow (e.g., GitHub without client_secret).
		return map[string]any{
				"title": fmt.Sprintf("Sign in to %s", displayName),
				"url":   dci.verificationURI,
				"code":  dci.userCode,
			}, &tui.DisplaySchema{
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
	case useDeviceCodeTUI && dci.userCode == "" && dci.verificationURI != "":
		// Browser auth code flow — handler reported the auth URL via callback.
		return map[string]any{
				"title": fmt.Sprintf("Sign in to %s", displayName),
				"url":   dci.verificationURI,
			}, &tui.DisplaySchema{
				Version: "v1",
				Status: &tui.StatusDisplayConfig{
					TitleField:     "title",
					WaitMessage:    "Waiting for browser authentication...",
					SuccessMessage: "Authenticated successfully!",
					DoneBehavior:   tui.DoneBehaviorExitAfterDelay,
					DoneDelay:      "2s",
					DisplayFields: []tui.StatusFieldDisplay{
						{Label: "URL", Field: "url"},
					},
					Actions: []tui.StatusActionConfig{
						{
							Label: "Re-open in browser",
							Type:  "open-url",
							Field: "url",
							Keys:  tui.StatusKeyBindings{Vim: "o", Emacs: "alt+o", Function: "f3"},
						},
					},
				},
			}
	default:
		// No callback fired — show minimal browser waiting TUI.
		return map[string]any{
				"title": fmt.Sprintf("Sign in to %s", displayName),
			}, &tui.DisplaySchema{
				Version: "v1",
				Status: &tui.StatusDisplayConfig{
					TitleField:     "title",
					WaitMessage:    "Waiting for browser authentication...",
					SuccessMessage: "Authenticated successfully!",
					DoneBehavior:   tui.DoneBehaviorExitAfterDelay,
					DoneDelay:      "2s",
				},
			}
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
