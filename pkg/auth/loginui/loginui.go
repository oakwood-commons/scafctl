// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package loginui renders the interactive login experience shared by the
// 'auth login' and 'kube login' commands. Interactive terminal runs show the
// kvx status-screen TUI for both device-code and browser flows: browser flows
// display a concise endpoint summary with copy/open actions for the full
// authorization URL alongside an auto-focused masked inline input for pasting
// localhost redirect URLs in remote SSH or dev-container sessions, falling back
// to a hidden plain-text paste prompt if the status TUI cannot start.
// Non-terminal and quiet sessions fall back to a clean plain-text layout
// without installing an interactive paste prompt.
//
// RunLogin returns the login Result and never prints the final success line or
// error text — callers render their own output. This lets both commands share
// one interactive presentation while keeping their distinct result rendering.
package loginui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/input"
	skvx "github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"golang.org/x/term"
)

const (
	// signInHeading is the single heading under which a sign-in URL is
	// printed on plain-text fallback surfaces.
	signInHeading = "To sign in, use a web browser to open the page:"
	// pasteBackPromptText is printed on plain-text fallback surfaces when the
	// status TUI cannot start and hidden terminal line input is available.
	// Prompt wording is always host-authored so plugins supply only URLs.
	pasteBackPromptText = "If your browser shows a connection error after sign-in, paste the full address from its address bar (input is hidden):"
	// waitingMessage closes the URL block while the login is in flight.
	waitingMessage = "Waiting for authentication..."
	// pasteInputLabel explains the inline redirect URL input rendered inside
	// the interactive login status TUI for remote SSH / Codespaces sessions.
	pasteInputLabel = "Remote / SSH / Codespaces: paste redirect URL if localhost fails:"
	// pasteInputPlaceholder illustrates the expected loopback callback URL shape.
	pasteInputPlaceholder = "http://localhost:<port>/?code=..."
	// maxPasteRedirectURLLen bounds accepted redirect URL input length.
	maxPasteRedirectURLLen = 8192
	// pasteActivationGrace bounds how long OnStatusInput waits for an in-flight
	// HostService.PromptAuthResponse call to register its expected redirect URI
	// when DeviceCodeCallback arrives slightly ahead of the reverse gRPC call.
	pasteActivationGrace = 250 * time.Millisecond
	// deviceCodeDoneDelay is how long the device-code TUI displays its
	// completion flash before returning the terminal.
	deviceCodeDoneDelay = "2s"
	// interactiveDoneDelay is how long the interactive status TUI shows its
	// completion message ("Authenticated as ...") before exiting.
	interactiveDoneDelay = "400ms"
	// authenticatedMessage is the fallback status flash when a login completes.
	authenticatedMessage = "Authenticated successfully!"
	// successMessageFmt formats the TUI's success exit message.
	successMessageFmt = "Authenticated as %s"
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
// the kvx status-screen TUI on interactive terminals and plain-text
// instructions everywhere else. It installs a SIGINT handler that cancels the
// login.
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

	if interactiveFlow && !w.IsQuiet() && interactiveTerminal(ioStreams) {
		return runInteractiveTUI(ctx, w, binaryName, handler, opts, ioStreams)
	}

	// Plain-text login path (non-terminal sessions, --quiet, and flows that
	// never render a status screen). Non-interactive sessions do not install a
	// PasteBackFunc so PromptAuthResponse returns Unavailable immediately
	// rather than blocking on an invisible prompt.
	var (
		lastMu     sync.Mutex
		lastPrompt *deviceCodeData
	)
	opts.DeviceCodeCallback = func(userCode, verificationURI, _ string) {
		lastMu.Lock()
		defer lastMu.Unlock()
		cur := deviceCodeData{userCode: userCode, verificationURI: verificationURI}
		if lastPrompt != nil && *lastPrompt == cur {
			return
		}
		lastPrompt = &cur
		printSignInBlock(w, verificationURI, userCode)
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

// printSignInBlock renders the unified plain sign-in block: one heading, the
// URL once, the user code when the handler surfaced one (device-code logins
// only), and the waiting line.
func printSignInBlock(w *writer.Writer, verificationURI, userCode string) {
	w.Plainln("")
	w.Plainln(signInHeading)
	w.Plainln("  " + verificationURI)
	if userCode != "" {
		w.Plainln("")
		w.Plainln("Enter the code: " + userCode)
	}
	w.Plainln("")
	w.Plainln(waitingMessage)
}

// runStatusTUI runs the device-code login flow using the kvx status-screen TUI.
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
			done <- tui.StatusResult{Message: fmt.Sprintf(successMessageFmt, loginIdentity(outcome.result))}
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
			WaitMessage:    waitingMessage,
			SuccessMessage: authenticatedMessage,
			DoneBehavior:   tui.DoneBehaviorExitAfterDelay,
			DoneDelay:      deviceCodeDoneDelay,
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

// pasteSession coordinates the in-TUI redirect URL input (OnStatusInput) with
// the background PasteBackFunc invoked by HostService.PromptAuthResponse, and
// transitions PasteBackFunc to a plain-text terminal reader (or an immediate
// unavailability error) if the status TUI fails to start.
// Submitted values wait briefly for an active PromptAuthResponse window and are
// pre-validated against its expected redirect URI before entering the one-shot
// channel so malformed or premature pastes surface an inline warning flash and
// allow retry without consuming the plugin's single RPC call.
type pasteSession struct {
	mu                  sync.Mutex
	expectedRedirectURI string
	active              bool
	fallbackErr         error
	lastPrintedPrompt   *deviceCodeData
	ready               chan struct{}
	readyOnce           sync.Once
	tuiFailed           chan struct{}
	tuiFailedOnce       sync.Once
	submitted           chan string
}

func newPasteSession() *pasteSession {
	return &pasteSession{
		ready:     make(chan struct{}),
		tuiFailed: make(chan struct{}),
		submitted: make(chan string, 1),
	}
}

// activate marks the paste-back prompt window open with redirectURI and returns
// a deactivation callback to run when PasteBackFunc exits.
func (ps *pasteSession) activate(redirectURI string) func() {
	ps.mu.Lock()
	if redirectURI != "" {
		ps.expectedRedirectURI = redirectURI
	}
	ps.active = true
	ps.mu.Unlock()
	ps.readyOnce.Do(func() { close(ps.ready) })

	var once sync.Once
	return func() {
		once.Do(func() {
			ps.mu.Lock()
			ps.active = false
			ps.mu.Unlock()
		})
	}
}

func (ps *pasteSession) state() (active bool, expectedRedirectURI string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.active, ps.expectedRedirectURI
}

// enterPlainFallback records the TUI startup error and closes tuiFailed so any
// parked or future PasteBackFunc switches away from the dead OnStatusInput path.
func (ps *pasteSession) enterPlainFallback(err error) {
	ps.tuiFailedOnce.Do(func() {
		ps.mu.Lock()
		ps.fallbackErr = err
		ps.mu.Unlock()
		close(ps.tuiFailed)
	})
}

func (ps *pasteSession) plainFallbackErr() error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.fallbackErr
}

func (ps *pasteSession) isPlainFallback() bool {
	select {
	case <-ps.tuiFailed:
		return true
	default:
		return false
	}
}

// printPlainBlockOnce prints a sign-in block under ps.mu when d carries a URL
// and differs from the most recently printed plain block.
func (ps *pasteSession) printPlainBlockOnce(w *writer.Writer, d deviceCodeData) {
	if d.verificationURI == "" {
		return
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.lastPrintedPrompt != nil && *ps.lastPrintedPrompt == d {
		return
	}
	cur := d
	ps.lastPrintedPrompt = &cur
	printSignInBlock(w, d.verificationURI, d.userCode)
}

func (ps *pasteSession) hasPrintedPlainBlock() bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.lastPrintedPrompt != nil
}

// readPlainFallback serves PasteBackFunc when the status TUI could not start.
// When cancellation-safe terminal line reading is available, it prints the
// host-authored paste instructions and reads/validates hidden input; otherwise
// it fails immediately with the TUI error so PromptAuthResponse never hangs.
func (ps *pasteSession) readPlainFallback(
	pasteCtx context.Context,
	loginCtx context.Context,
	w *writer.Writer,
	ioStreams *terminal.IOStreams,
	authorizationURL string,
	redirectURI string,
) (string, error) {
	tuiErr := ps.plainFallbackErr()
	if !canReadPlainPaste(ioStreams) {
		return "", fmt.Errorf("paste-back prompt unavailable after status TUI error: %w", tuiErr)
	}

	if !ps.hasPrintedPlainBlock() && authorizationURL != "" {
		ps.printPlainBlockOnce(w, deviceCodeData{verificationURI: authorizationURL})
	}

	ps.mu.Lock()
	w.Plainln("")
	w.Plainln(pasteBackPromptText)
	ps.mu.Unlock()

	readCtx, cancelRead := context.WithCancel(pasteCtx)
	stop := context.AfterFunc(loginCtx, cancelRead)
	defer func() {
		stop()
		cancelRead()
	}()

	reader := input.ReadInteractiveLine
	if readPlainPasteLine != nil {
		reader = readPlainPasteLine
	}

	for {
		line, err := reader(readCtx, ioStreams.In, io.Discard)
		if err != nil {
			if pasteCtx.Err() != nil {
				return "", pasteCtx.Err()
			}
			if loginCtx.Err() != nil {
				return "", loginCtx.Err()
			}
			return "", fmt.Errorf("reading pasted redirect URL after status TUI error (%w): %w", tuiErr, err)
		}
		// Hidden input does not echo a newline to the terminal.
		w.PlainStderr("")

		normalized, valErr := validateStatusPasteInput(line, redirectURI)
		if valErr != nil {
			w.WarnStderr(fmt.Sprintf("Invalid redirect URL: %v", valErr))
			continue
		}
		return normalized, nil
	}
}

// submit validates a redirect URL pasted into the status TUI input before
// delivering it to the waiting PasteBackFunc.
func (ps *pasteSession) submit(ctx context.Context, raw string) error {
	select {
	case <-ps.ready:
	default:
		timer := time.NewTimer(pasteActivationGrace)
		select {
		case <-ps.ready:
			timer.Stop()
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}

	active, expectedURI := ps.state()
	if !active {
		return errors.New("redirect URL paste-back is not active for this login")
	}

	normalized, err := validateStatusPasteInput(raw, expectedURI)
	if err != nil {
		return err
	}
	select {
	case ps.submitted <- normalized:
		return nil
	default:
		return errors.New("redirect URL has already been submitted")
	}
}

// outcomeCapture owns the login outcome channel: it consumes the single
// outcome, forwards a StatusResult to the TUI's Done channel (non-blocking in
// case the TUI already exited), and exposes the outcome via wait/closed.
type outcomeCapture struct {
	captured loginOutcome
	ready    chan struct{}
}

func captureOutcome(outcomeChan <-chan loginOutcome, done chan<- tui.StatusResult) *outcomeCapture {
	oc := &outcomeCapture{ready: make(chan struct{})}
	go func() {
		oc.captured = <-outcomeChan
		close(oc.ready)
		if oc.captured.err != nil {
			select {
			case done <- tui.StatusResult{Err: oc.captured.err}:
			default:
			}
		} else {
			select {
			case done <- tui.StatusResult{Message: fmt.Sprintf(successMessageFmt, loginIdentity(oc.captured.result))}:
			default:
			}
		}
	}()
	return oc
}

// closed reports without blocking whether the outcome has been captured.
func (oc *outcomeCapture) closed() bool {
	select {
	case <-oc.ready:
		return true
	default:
		return false
	}
}

// wait blocks for the outcome or context cancellation.
func (oc *outcomeCapture) wait(ctx context.Context) (*auth.Result, error) {
	select {
	case <-oc.ready:
		if oc.captured.err != nil {
			return nil, loginError(ctx, oc.captured.err)
		}
		return oc.captured.result, nil
	case <-ctx.Done():
		return nil, exitcode.WithCode(auth.ErrUserCancelled, exitcode.GeneralError)
	}
}

// runStatusScreen is tui.Run as a test seam: tests replace it to assert how
// the status screen is configured and to drive completion or submission
// deterministically without a PTY. Production code never reassigns it.
var runStatusScreen = tui.Run

// readPlainPasteLine overrides input.ReadInteractiveLine in tests so plain-text
// paste fallback can be exercised deterministically without a PTY. Production
// code leaves it nil.
var readPlainPasteLine func(ctx context.Context, in io.Reader, echo io.Writer) (string, error)

func canReadPlainPaste(ioStreams *terminal.IOStreams) bool {
	if readPlainPasteLine != nil {
		return true
	}
	return input.InteractiveLineSupported() && isTTY(ioStreams.In)
}

// runInteractiveTUI runs an interactive browser login inside the kvx status TUI
// with an auto-focused masked inline input for redirect URL paste-back:
//
//  1. Starts handler.Login in a goroutine with a DeviceCodeCallback that
//     captures the authorize URL and an in-memory PasteBackFunc backed by
//     pasteSession.
//  2. Launches the kvx status screen displaying a concise scheme://host/path
//     endpoint summary while keeping Copy URL and Open URL bound to the full
//     authorization URL, plus StatusDisplayConfig.Input (Masked: true) and
//     cfg.OnStatusInput for remote SSH / Codespaces redirect URL paste-back.
//  3. When the localhost browser callback completes first, PasteBackFunc
//     cancels cleanly via its RPC context and the TUI flashes "Authenticated as
//     ..." before exiting. When the user pastes a redirect URL into the TUI,
//     OnStatusInput pre-validates it before resolving PasteBackFunc so typos or
//     wrong URLs flash a retryable warning inline without consuming the RPC.
//     If the status TUI fails to start, runInteractiveTUI prints the plain
//     sign-in block and switches PasteBackFunc to hidden plain-text terminal
//     input (or fails PasteBackFunc immediately if terminal line input is
//     unavailable).
func runInteractiveTUI(
	ctx context.Context,
	w *writer.Writer,
	binaryName string,
	handler auth.Handler,
	opts auth.LoginOptions,
	ioStreams *terminal.IOStreams,
) (*auth.Result, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	urlChan := make(chan deviceCodeData, 1)
	outcomeChan := make(chan loginOutcome, 1)
	done := make(chan tui.StatusResult, 1)
	ps := newPasteSession()

	opts.DeviceCodeCallback = func(userCode, verificationURI, _ string) {
		d := deviceCodeData{userCode: userCode, verificationURI: verificationURI}
		select {
		case urlChan <- d:
		default:
		}
		if ps.isPlainFallback() {
			ps.printPlainBlockOnce(w, d)
		}
	}

	ctx = auth.WithPasteBack(ctx, func(pasteCtx context.Context, authorizationURL, redirectURI string) (string, error) {
		defer ps.activate(redirectURI)()
		if authorizationURL != "" {
			select {
			case urlChan <- deviceCodeData{verificationURI: authorizationURL}:
			default:
			}
		}
		select {
		case val := <-ps.submitted:
			return val, nil
		case <-ps.tuiFailed:
			return ps.readPlainFallback(pasteCtx, ctx, w, ioStreams, authorizationURL, redirectURI)
		case <-pasteCtx.Done():
			return "", pasteCtx.Err()
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})

	go func() {
		result, err := handler.Login(ctx, opts)
		outcomeChan <- loginOutcome{result: result, err: err}
	}()

	w.Verbosef("Initiating authentication with %s...", handler.DisplayName())

	var prompt deviceCodeData
	select {
	case prompt = <-urlChan:
	case outcome := <-outcomeChan:
		if outcome.err != nil {
			return nil, loginError(ctx, outcome.err)
		}
		return outcome.result, nil
	case <-ctx.Done():
		return nil, exitcode.WithCode(auth.ErrUserCancelled, exitcode.GeneralError)
	}

	oc := captureOutcome(outcomeChan, done)

	cfg, data := buildInteractiveStatusTUI(binaryName, handler.DisplayName(), prompt, done, func(raw string) error {
		return ps.submit(ctx, raw)
	})

	teaOpts := tui.WithIO(ioStreams.In, ioStreams.Out)
	if runErr := runStatusScreen(data, cfg, teaOpts...); runErr != nil {
		if ctx.Err() != nil {
			return nil, exitcode.WithCode(auth.ErrUserCancelled, exitcode.GeneralError)
		}
		// If the TUI could not start (e.g. an unusable terminal configuration),
		// print the plain sign-in block and switch PasteBackFunc to plain-text
		// terminal line input (or fail PasteBackFunc immediately when terminal
		// line input is unavailable) so neither local callbacks nor remote
		// paste-back logins stall.
		ps.printPlainBlockOnce(w, prompt)
		ps.enterPlainFallback(runErr)
		return oc.wait(ctx)
	}

	if oc.closed() {
		return oc.wait(ctx)
	}

	// User exited the TUI early (Esc or Ctrl+C) before authentication completed.
	cancel()
	return nil, exitcode.WithCode(auth.ErrUserCancelled, exitcode.GeneralError)
}

// buildInteractiveStatusTUI constructs the tui.Config (including DisplaySchema)
// and data map for an interactive login status screen.
func buildInteractiveStatusTUI(
	binaryName string,
	handlerDisplayName string,
	prompt deviceCodeData,
	done <-chan tui.StatusResult,
	onSubmit tui.StatusInputSubmitFunc,
) (tui.Config, map[string]any) {
	authURL := prompt.verificationURI
	data := map[string]any{
		"title":    fmt.Sprintf("Sign in to %s", handlerDisplayName),
		"url":      authURL,
		"endpoint": summarizeAuthorizationURL(authURL),
	}

	fields := []tui.StatusFieldDisplay{{Label: "URL", Field: "endpoint"}}
	copyAction := tui.StatusActionConfig{
		Label: "Copy URL",
		Type:  "copy-value",
		Field: "url",
		Keys:  tui.StatusKeyBindings{Vim: "c", Emacs: "alt+c", Function: "f2"},
	}

	var statusInput *tui.StatusInputConfig
	var statusSubmit tui.StatusInputSubmitFunc

	if prompt.userCode != "" {
		// Handler resolved interactive/empty flow internally to device code.
		data["code"] = prompt.userCode
		fields = append(fields, tui.StatusFieldDisplay{Label: "Code", Field: "code"})
		copyAction.Label = "Copy code"
		copyAction.Field = "code"
	} else {
		statusInput = &tui.StatusInputConfig{
			Label:       pasteInputLabel,
			Placeholder: pasteInputPlaceholder,
			Masked:      true,
			MaxLength:   maxPasteRedirectURLLen,
		}
		statusSubmit = onSubmit
	}

	schema := &tui.DisplaySchema{
		Version: "v1",
		Status: &tui.StatusDisplayConfig{
			TitleField:     "title",
			WaitMessage:    waitingMessage,
			SuccessMessage: authenticatedMessage,
			DoneBehavior:   tui.DoneBehaviorExitAfterDelay,
			DoneDelay:      interactiveDoneDelay,
			DisplayFields:  fields,
			Input:          statusInput,
			Actions: []tui.StatusActionConfig{
				copyAction,
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
	cfg.OnStatusInput = statusSubmit

	return cfg, data
}

// summarizeAuthorizationURL formats an authorization URL as a concise
// "scheme://host/path" display string (omitting query parameters, fragments,
// and userinfo) so long OAuth PKCE URLs fit cleanly inside the status card
// while Copy URL and Open URL remain bound to the full URL.
func summarizeAuthorizationURL(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || !parsed.IsAbs() || parsed.Scheme == "" || parsed.Host == "" {
		if idx := strings.IndexAny(trimmed, "?#"); idx >= 0 {
			return trimmed[:idx]
		}
		return trimmed
	}
	path := parsed.EscapedPath()
	if path == "/" {
		path = ""
	}
	return parsed.Scheme + "://" + parsed.Host + path
}

// validateStatusPasteInput normalizes and validates a redirect URL submitted
// from the interactive status TUI before delivering it to PasteBackFunc.
// Returned errors never embed the pasted value itself.
func validateStatusPasteInput(raw, expectedRedirectURI string) (string, error) {
	if len(raw) > maxPasteRedirectURLLen {
		return "", fmt.Errorf("pasted redirect URL exceeds maximum length of %d characters", maxPasteRedirectURLLen)
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("paste the full redirect URL from your browser's address bar")
	}

	var want *url.URL
	if expectedRedirectURI != "" {
		if parsedWant, err := url.Parse(expectedRedirectURI); err == nil && parsedWant.IsAbs() && parsedWant.Host != "" {
			want = parsedWant
		}
	}

	normalized := normalizeSchemeLessLoopbackURL(trimmed, want)
	parsed, err := url.Parse(normalized)
	if err != nil || !parsed.IsAbs() || (!strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) || parsed.Host == "" || parsed.User != nil {
		return "", errors.New("expected a localhost redirect URL (e.g. http://localhost:<port>/?code=...)")
	}
	if !isLoopbackRedirectHost(parsed.Hostname()) {
		return "", errors.New("expected a localhost redirect URL, not an external address")
	}

	if want != nil {
		wantPath := strings.TrimSuffix(want.Path, "/")
		gotPath := strings.TrimSuffix(parsed.Path, "/")
		if !strings.EqualFold(parsed.Scheme, want.Scheme) || !strings.EqualFold(parsed.Host, want.Host) ||
			(wantPath != "" && gotPath != wantPath && !strings.HasPrefix(gotPath, wantPath+"/")) {
			return "", fmt.Errorf("pasted URL does not match expected redirect %s", expectedRedirectURI)
		}
	}

	if !hasOAuthRedirectPayload(parsed) {
		return "", errors.New("redirect URL must contain code=, error=, or #access_token=")
	}

	canonical := canonicalizeLoopbackSchemeAndHost(normalized, want)
	if len(canonical) > maxPasteRedirectURLLen {
		return "", fmt.Errorf("pasted redirect URL exceeds maximum length of %d characters", maxPasteRedirectURLLen)
	}
	return canonical, nil
}

// normalizeSchemeLessLoopbackURL restores the "http://" scheme when a browser
// strips it while copying a loopback URL (localhost, 127.0.0.0/8, or [::1])
// from the address bar, even when query or fragment parameters contain URL
// values such as error_uri=https://... or iss=https://....
func normalizeSchemeLessLoopbackURL(trimmed string, want *url.URL) string {
	if hasOuterURLScheme(trimmed) {
		return trimmed
	}
	if want != nil && !strings.EqualFold(want.Scheme, "http") {
		return trimmed
	}
	candidate := "http://" + trimmed
	parsed, err := url.Parse(candidate)
	if err != nil || parsed.Host == "" || parsed.User != nil || !isLoopbackRedirectHost(parsed.Hostname()) {
		return trimmed
	}
	return candidate
}

// hasOuterURLScheme reports whether s starts with an RFC 3986 "<scheme>://"
// prefix (or a malformed leading "://") rather than only containing "://"
// inside a path, query, or fragment parameter.
func hasOuterURLScheme(s string) bool {
	idx := strings.Index(s, "://")
	if idx < 0 {
		return false
	}
	if idx == 0 {
		return true
	}
	for i := 0; i < idx; i++ {
		c := s[i]
		isAlpha := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		isDigit := c >= '0' && c <= '9'
		if i == 0 {
			if !isAlpha {
				return false
			}
			continue
		}
		if !isAlpha && !isDigit && c != '+' && c != '-' && c != '.' {
			return false
		}
	}
	return true
}

// canonicalizeLoopbackSchemeAndHost lowercases the URL scheme and host (or
// aligns them with want when present) while preserving path, raw query, and
// fragment bytes verbatim.
func canonicalizeLoopbackSchemeAndHost(rawURL string, want *url.URL) string {
	idx := strings.Index(rawURL, "://")
	if idx < 0 {
		return rawURL
	}
	afterScheme := rawURL[idx+3:]
	hostEnd := strings.IndexAny(afterScheme, "/?#")
	if hostEnd < 0 {
		hostEnd = len(afterScheme)
	}
	scheme := strings.ToLower(rawURL[:idx])
	host := strings.ToLower(afterScheme[:hostEnd])
	if want != nil {
		scheme = want.Scheme
		host = want.Host
	}
	return scheme + "://" + host + afterScheme[hostEnd:]
}

// isLoopbackRedirectHost reports whether host is "localhost" or a loopback IP
// address (such as 127.0.0.1 or ::1).
func isLoopbackRedirectHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// hasOAuthRedirectPayload reports whether parsed carries a non-empty OAuth
// authorization response parameter (code=, error=, or #access_token=).
func hasOAuthRedirectPayload(parsed *url.URL) bool {
	if parsed.RawQuery != "" {
		q := parsed.Query()
		if q.Get("code") != "" || q.Get("error") != "" {
			return true
		}
	}
	if escapedFragment := parsed.EscapedFragment(); escapedFragment != "" {
		fq := (&url.URL{RawQuery: escapedFragment}).Query()
		if fq.Get("access_token") != "" || fq.Get("code") != "" || fq.Get("error") != "" {
			return true
		}
	}
	return false
}

// interactiveTerminal reports whether stdin, stdout, and stderr are all
// terminals, i.e. the session can render prompts and read answers from a
// human.
func interactiveTerminal(ioStreams *terminal.IOStreams) bool {
	// A real TTY check (not os.ModeCharDevice): /dev/null is a char device,
	// and a prompt written there would leave the login blocked invisibly.
	return isTTY(ioStreams.In) && isTTY(ioStreams.Out) && isTTY(ioStreams.ErrOut)
}

func isTTY(s any) bool {
	f, ok := s.(*os.File)
	return ok && term.IsTerminal(int(f.Fd())) //nolint:gosec // Fd() fits in int on all supported platforms
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
