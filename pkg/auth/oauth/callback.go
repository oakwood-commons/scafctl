// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package oauth

import (
	"context"
	"fmt"
	"html"
	"net"
	"net/http"
	"time"
)

// CallbackResult holds the outcome of a local OAuth callback.
type CallbackResult struct {
	// Code is the authorization code received from the identity provider.
	Code string
	// Err is set if an error was received instead of a code.
	Err error
}

// CallbackServer manages a local HTTP server that listens for an OAuth redirect
// and captures the authorization code.
type CallbackServer struct {
	// RedirectURI is the localhost URI the identity provider should redirect to
	// (e.g. "http://localhost:54321").
	RedirectURI string

	listener net.Listener
	server   *http.Server
	resultCh chan CallbackResult
}

// StartCallbackServer creates and starts a local HTTP server on a localhost
// port. When port is 0 an ephemeral port is chosen by the OS; when port > 0
// the server binds to that specific port so the redirect URI is predictable
// (useful when the app registration only allows specific redirect URIs).
//
// The server waits for a single OAuth redirect, extracts the authorization
// code (or error), and sends it on the channel returned by ResultChan().
//
// The caller is responsible for calling Close() when done.
func StartCallbackServer(ctx context.Context, port int) (*CallbackServer, error) {
	addr := "localhost:0"
	if port > 0 {
		addr = fmt.Sprintf("localhost:%d", port)
	}

	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("starting local redirect server on %s: %w", addr, err)
	}

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		listener.Close()
		return nil, fmt.Errorf("unexpected listener address type: %T", listener.Addr())
	}

	cs := &CallbackServer{
		RedirectURI: fmt.Sprintf("http://localhost:%d", tcpAddr.Port),
		listener:    listener,
		resultCh:    make(chan CallbackResult, 1),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", cs.handleCallback)

	cs.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 30 * time.Second,
	}

	go func() {
		if sErr := cs.server.Serve(listener); sErr != nil && sErr != http.ErrServerClosed {
			cs.resultCh <- CallbackResult{Err: fmt.Errorf("redirect server error: %w", sErr)}
		}
	}()

	return cs, nil
}

// ResultChan returns the channel that will receive exactly one CallbackResult
// once the OAuth redirect arrives (or an error occurs).
func (cs *CallbackServer) ResultChan() <-chan CallbackResult {
	return cs.resultCh
}

// Close shuts down the callback server and its listener.
func (cs *CallbackServer) Close() error {
	return cs.server.Close()
}

func (cs *CallbackServer) handleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		errMsg := r.URL.Query().Get("error")
		errDesc := r.URL.Query().Get("error_description")
		if errMsg == "" {
			errMsg = "no authorization code received"
		}
		if errDesc != "" {
			errMsg = fmt.Sprintf("%s: %s", errMsg, errDesc)
		}
		cs.resultCh <- CallbackResult{Err: fmt.Errorf("OAuth error: %s", errMsg)}
		fmt.Fprintf(w, "<html><body><h1>Authentication Failed</h1><p>%s</p><p>You can close this window.</p></body></html>", html.EscapeString(errMsg)) //nolint:gosec // input is escaped
		return
	}

	cs.resultCh <- CallbackResult{Code: code}
	fmt.Fprint(w, "<html><body><h1>Authentication Successful</h1><p>You can close this window and return to the terminal.</p></body></html>")
}
