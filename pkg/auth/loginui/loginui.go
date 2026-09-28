// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package loginui renders the interactive login experience shared by the
// 'auth login' and 'kube login' commands. Terminal runs show the kvx
// status-screen TUI: the device-code flow permanently, and browser
// (interactive) flows in a hybrid arrangement -- the box displays the
// sign-in URL with copy/open actions while the login is in flight, and the
// flow exits cleanly to the unified plain-text paste block the moment a
// paste-back response is requested (or the user quits the box, which falls
// back to the same plain flow). Every non-terminal run, and every flow that
// cannot render a prompt, uses the unified plain layout (#879): the
// authorize URL appears exactly once under one heading, and the paste
// hint is the only text appended when paste-back engages.
//
// The TUI and the paste prompt never share the keyboard (#877): the
// paste-back line read (auth.PasteBackFunc, bridged to
// HostService.PromptAuthResponse) needs line-oriented stdin, so it is only
// started after tui.Run has returned and the terminal is plain again.
//
// RunLogin returns the login Result and never prints the final success line or
// error text — callers render their own output. This lets both commands share
// one interactive presentation while keeping their distinct result rendering.
package loginui

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"

	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/input"
	skvx "github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"golang.org/x/term"
)

// The sign-in copy is host-authored and unified across every surface that
// renders a login URL (#879): the plain block, the plain paste hint, and the
// hybrid handoff block all use the same heading, so a login reads as one
// instruction flow (URL -> sign-in -> paste hint -> hidden input).
const (
	// signInHeading is the single heading under which a sign-in URL is
	// printed. It matches the wording the paste hint refers back to.
	signInHeading = "To sign in, use a web browser to open the page:"
	// pasteBackPromptText is the only line added when paste-back engages.
	// By contract the prompt wording is written by the host only --
	// plugins supply URLs, never text -- so a handler cannot use
	// PromptAuthResponse to phish for input.
	pasteBackPromptText = "If your browser shows a connection error after sign-in, paste the full address from its address bar (input is hidden):"
	// waitingMessage closes the URL block while the login is in flight.
	waitingMessage = "Waiting for authentication..."
	// pasteExitMessage is flashed by the status TUI while it exits for the
	// plain paste handoff.
	pasteExitMessage = "Opening the manual paste prompt..."
	// interactiveDoneDelay is how long the interactive status TUI shows its
	// exit message (paste handoff or "Authenticated as ...") before
	// returning the terminal. It is shorter than the device-code TUI's
	// delay so the paste block appears promptly.
	interactiveDoneDelay = "400ms"
	// authenticatedMessage is the status flash when a login completes;
	// successMessageFmt names the logged-in identity below it.
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
// the kvx status-screen TUI on terminals (device-code flow permanently,
// browser flows in a hybrid arrangement with the paste-back prompt) and the
// unified plain-text instructions everywhere else. It installs a SIGINT
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

	// Hybrid (#879, option c): an interactive session that can actually
	// render the paste-back prompt (an interactive terminal on a platform
	// with cancellation-safe reads, not --quiet) presents the status TUI
	// first; the paste prompt takes over as clean plain text the moment it
	// becomes relevant. The condition is exactly the one under which the
	// paste-back prompt is offered at all, so a session that cannot prompt
	// (non-TTY, redirected stderr, --quiet) keeps the plain renderer and
	// never installs a prompt -- --quiet can never block on an invisible
	// prompt (#877).
	if interactiveFlow && !w.IsQuiet() && interactiveTerminal(ioStreams) && input.InteractiveLineSupported() {
		return runInteractiveTUI(ctx, w, binaryName, handler, opts, ioStreams)
	}

	// Plain-text login path (non-terminal sessions, --quiet, and flows that
	// never render a status screen). A handler that re-fires the callback
	// with the same prompt never duplicates the block; a different prompt
	// (e.g. an interactive login falling back to device code) still prints.
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
// only), and the waiting line. The block is deliberately unstyled -- no
// icons or bullets -- so the instructions read as one coherent flow (#879).
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

// pasteHandoff coordinates the switch from an interactive login's status TUI
// to the plain-text paste prompt. A raw-mode TUI and the paste prompt's
// line-oriented stdin read can never be active at once (#877), so the paste
// function first requests the handoff, then blocks on tuiDown until the
// runner has taken the TUI down (or reports that none was ever launched),
// and only then prints and reads.
type pasteHandoff struct {
	mu sync.Mutex
	// requested is set by the first paste request; later requests are
	// no-ops (the handoff already happened).
	requested bool
	// urlPrinted reports whether a sign-in URL block is already visible on
	// the plain screen; the paste prompt then renders the hint only.
	urlPrinted bool
	// doneCh is the running status TUI's Done channel while the TUI is up;
	// nil otherwise.
	doneCh chan<- tui.StatusResult
	// tuiDown is closed exactly once when the screen is plain (either the
	// TUI exited, it was never launched, or no TUI could be launched).
	tuiDown   chan struct{}
	closeDown sync.Once
}

func newPasteHandoff() *pasteHandoff {
	return &pasteHandoff{tuiDown: make(chan struct{})}
}

// markTuiUp records the running TUI's Done channel: paste requests from now
// on drive the TUI down by sending it an exit message.
func (h *pasteHandoff) markTuiUp(done chan<- tui.StatusResult) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.doneCh = done
}

// markTuiDown releases any waiting paste prompt: the terminal is plain.
// Safe to call multiple times.
func (h *pasteHandoff) markTuiDown() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.doneCh = nil
	h.closeDown.Do(func() { close(h.tuiDown) })
}

// engaged reports whether a paste request already drove the handoff.
func (h *pasteHandoff) engaged() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.requested
}

// requestPaste asks for the plain-text paste handoff. While the status TUI
// is up it makes the TUI exit cleanly (the message flashes briefly, then
// tui.Run returns and the runner calls markTuiDown); with the screen already
// plain it releases the prompt directly.
func (h *pasteHandoff) requestPaste() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.requested {
		return
	}
	h.requested = true
	if h.doneCh != nil {
		select {
		case h.doneCh <- tui.StatusResult{Message: pasteExitMessage}:
		default:
		}
		return
	}
	h.closeDown.Do(func() { close(h.tuiDown) })
}

// printURLBlockFallback renders the plain sign-in block for a user who quit
// the status TUI early: the URL stays visible for the rest of the login and
// the waiting line explains what happens next. It holds the handoff lock
// while printing so a concurrent paste prompt cannot interleave.
func (h *pasteHandoff) printURLBlockFallback(w *writer.Writer, verificationURI, userCode string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.urlPrinted = true
	printSignInBlock(w, verificationURI, userCode)
}

// announcePaste renders the paste instructions right before the hidden read.
// When the sign-in URL is not already on the plain screen (the handoff from
// the TUI, whose alt screen is wiped on exit), the URL block prints first
// with the same heading, keeping the flow identical to the non-TTY layout.
func (h *pasteHandoff) announcePaste(w *writer.Writer, authorizationURL string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.urlPrinted {
		h.urlPrinted = true
		w.Plainln("")
		w.Plainln(signInHeading)
		w.Plainln("  " + authorizationURL)
		w.Plainln("")
	}
	w.Plainln(pasteBackPromptText)
}

// outcomeCapture owns the login outcome channel on behalf of a phase that no
// longer selects on it directly: it consumes the single outcome, forwards a
// StatusResult to the TUI's Done channel (non-blocking -- the TUI may have
// already exited), and makes the outcome readable via wait/closed.
//
// captured is safe to read after ready is closed because close(ready)
// happens-before any receive on it. The closure precedes the Done forward,
// so a consumer of the forwarded message (the TUI, or a test stub) can rely
// on the outcome being published.
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

// wait blocks for the outcome (or a cancelled login, which the login
// goroutine then reports as its error).
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
// the status screen is launched and to drive the TUI-exit handoff
// deterministically without a PTY. Production code never reassigns it.
var runStatusScreen = tui.Run

// runInteractiveTUI runs a paste-capable interactive (browser) login in the
// hybrid arrangement (#879, option c):
//
//  1. Starts handler.Login in a goroutine with a DeviceCodeCallback that
//     captures the authorize URL (browser flows send no user code), and a
//     paste-back prompt that coordinates with this runner.
//  2. Waits for the URL before launching the TUI, then shows the kvx status
//     box with copy/open URL actions.
//  3. When the login completes, the outcome drives the Done channel and the
//     box shows "Authenticated as ..." before exiting. When the plugin
//     requests a paste-back response, the paste prompt makes the TUI exit
//     with a short handoff message and prints the unified plain paste block
//     after tui.Run has returned. When the user quits the box early, the
//     plain sign-in block prints and the login continues in plain text --
//     also the manual way to reach the paste prompt.
//
// Paste-back requests that arrive before the URL (a handler that skips the
// URL callback) are served directly in plain text without ever launching the
// TUI. The TUI and the paste read never share stdin: ReadInteractiveLine
// only starts after markTuiDown.
func runInteractiveTUI(
	ctx context.Context,
	w *writer.Writer,
	binaryName string,
	handler auth.Handler,
	opts auth.LoginOptions,
	ioStreams *terminal.IOStreams,
) (*auth.Result, error) {
	urlChan := make(chan deviceCodeData, 1)
	outcomeChan := make(chan loginOutcome, 1)
	// done has cap 2 because it has exactly two senders (the outcome
	// forwarder and one paste request), which keeps both non-blocking
	// sends drop-free.
	done := make(chan tui.StatusResult, 2)
	handoff := newPasteHandoff()

	// Only the first URL drives the display: the URL prints exactly once
	// per login run even if the handler re-fires the callback.
	// A handler that resolves to device code internally (an empty or
	// interactive flow without a client secret) supplies a user code; it is
	// carried through so the box and the fallback block show it.
	opts.DeviceCodeCallback = func(userCode, verificationURI, _ string) {
		select {
		case urlChan <- deviceCodeData{userCode: userCode, verificationURI: verificationURI}:
		default:
		}
	}

	ctx = auth.WithPasteBack(ctx, newPasteBackPrompt(w, ioStreams, handoff))

	go func() {
		result, err := handler.Login(ctx, opts)
		outcomeChan <- loginOutcome{result: result, err: err}
	}()

	w.Verbosef("Initiating authentication with %s...", handler.DisplayName())

	// Wait for the sign-in URL, an early completion, or cancellation. A
	// paste request alongside this wait cannot strand a prompt: with no
	// TUI up it releases itself (requestPaste closes tuiDown).
	var prompt deviceCodeData
	select {
	case prompt = <-urlChan:
	case outcome := <-outcomeChan:
		// Login completed before a URL was shown (unusual).
		if outcome.err != nil {
			return nil, loginError(ctx, outcome.err)
		}
		return outcome.result, nil
	case <-ctx.Done():
		return nil, exitcode.WithCode(auth.ErrUserCancelled, exitcode.GeneralError)
	}

	// From here on the outcome is owned by the capture goroutine.
	oc := captureOutcome(outcomeChan, done)

	// Register the TUI's Done channel first (before checking for an
	// in-flight paste request) so a request racing this check always
	// either drives the TUI down cleanly or finds the screen already
	// released -- it can never print under a TUI that launches afterward,
	// nor wait on a tuiDown that only a TUI exit would close.
	handoff.markTuiUp(done)
	defer handoff.markTuiDown()

	// A paste request that arrived during the wait owns the plain screen
	// (its prompt renders on its own); never launch the TUI on top of it.
	if handoff.engaged() {
		handoff.markTuiDown()
		return oc.wait(ctx)
	}

	authURL := prompt.verificationURI
	data := map[string]any{
		"title": fmt.Sprintf("Sign in to %s", handler.DisplayName()),
		"url":   authURL,
	}
	fields := []tui.StatusFieldDisplay{{Label: "URL", Field: "url"}}
	copyAction := tui.StatusActionConfig{
		Label: "Copy URL",
		Type:  "copy-value",
		Field: "url",
		Keys:  tui.StatusKeyBindings{Vim: "c", Emacs: "alt+c", Function: "f2"},
	}
	if prompt.userCode != "" {
		data["code"] = prompt.userCode
		fields = append(fields, tui.StatusFieldDisplay{Label: "Code", Field: "code"})
		copyAction.Label = "Copy code"
		copyAction.Field = "code"
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

	teaOpts := tui.WithIO(ioStreams.In, ioStreams.Out)
	if runErr := runStatusScreen(data, cfg, teaOpts...); runErr != nil {
		if ctx.Err() != nil {
			return nil, exitcode.WithCode(auth.ErrUserCancelled, exitcode.GeneralError)
		}
		// The status TUI could not even start (an unusable terminal type,
		// say). The login never needed it to succeed: keep going with the
		// unified plain block, where the paste prompt stays fully
		// functional.
		if !handoff.engaged() {
			handoff.printURLBlockFallback(w, authURL, prompt.userCode)
		}
		handoff.markTuiDown()
		return oc.wait(ctx)
	}

	if oc.closed() {
		// The outcome drove the TUI exit (success or error flash).
		return oc.wait(ctx)
	}

	// The TUI exited without the login finishing. Either a paste request
	// drove the exit (the prompt takes over the plain screen), or the user
	// quit the box: fall back to the plain block so the URL stays visible
	// and keep waiting in plain text. A cancelled login prints nothing
	// more -- the cancellation notice already went to stderr.
	if !handoff.engaged() && ctx.Err() == nil {
		handoff.printURLBlockFallback(w, authURL, prompt.userCode)
	}
	handoff.markTuiDown()

	return oc.wait(ctx)
}

// newPasteBackPrompt builds the PasteBackFunc installed into interactive
// logins: it renders the host-authored paste instructions and reads the
// pasted redirect URL with a cancellation-safe terminal read (the RPC
// context cancels when the plugin's own localhost callback arrives first,
// and the read leaves no reader behind to steal the next input). The pasted
// value is never written anywhere: it carries an authorization code, so the
// read does not echo it (like a password prompt) and it is never logged.
func newPasteBackPrompt(w *writer.Writer, ioStreams *terminal.IOStreams, handoff *pasteHandoff) auth.PasteBackFunc {
	return func(ctx context.Context, authorizationURL, _ string) (string, error) {
		// Make the screen plain before anything prints or reads: a
		// raw-mode status TUI owns stdin exclusively (#877).
		handoff.requestPaste()
		select {
		case <-handoff.tuiDown:
			// Screen is plain: safe to print and read.
		case <-ctx.Done():
			// The callback arrived (or the login was canceled): the
			// prompt goes away and the half-typed line is discarded.
			return "", ctx.Err()
		}
		handoff.announcePaste(w, authorizationURL)
		line, err := input.ReadInteractiveLine(ctx, ioStreams.In, io.Discard)
		// No echo means the terminal never saw a newline: end the prompt line.
		w.PlainStderr("")
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", fmt.Errorf("reading pasted address: %w", err)
		}
		return strings.TrimSpace(line), nil
	}
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
