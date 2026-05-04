// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package oauth

import (
	"context"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// CallbackResult holds the outcome of a local OAuth callback.
type CallbackResult struct {
	// Code is the authorization code received from the identity provider.
	Code string
	// AccessToken is the token received directly via implicit grant flow.
	AccessToken string //nolint:gosec // struct field, not a hardcoded credential
	// TokenType is the token type from an implicit grant response (e.g. "Bearer").
	TokenType string
	// ExpiresIn is the token lifetime in seconds from an implicit grant response.
	ExpiresIn string
	// Err is set if an error was received instead of a code.
	Err error
}

// implicitTokenPath is the path used by the implicit grant flow to receive
// POSTed token data from the browser. It is reserved and cannot be used as
// a custom callback path.
const implicitTokenPath = "/token"

// allowedCallbackHosts is the set of hosts permitted for the OAuth callback
// server. Only loopback addresses are allowed to prevent binding on external
// interfaces.
var allowedCallbackHosts = map[string]bool{
	"localhost": true,
	"127.0.0.1": true,
	"::1":       true,
}

// ValidateCallbackHostPath validates callback host and path values
// without starting a server. This is intended for config-time validation.
func ValidateCallbackHostPath(host, path string) error {
	cfg := &callbackConfig{
		host: host,
		path: path,
	}
	if host == "" {
		cfg.host = "localhost"
	}
	if path == "" {
		cfg.path = "/"
	}
	return validateCallbackConfig(cfg)
}

// CallbackOption configures optional parameters for the callback server.
type CallbackOption func(*callbackConfig)

type callbackConfig struct {
	host string
	path string
}

// WithCallbackHost sets the host used in the redirect URI (default: "localhost").
// Only loopback addresses are allowed: "localhost", "127.0.0.1", or "::1".
func WithCallbackHost(host string) CallbackOption {
	return func(c *callbackConfig) {
		c.host = host
	}
}

// WithCallbackPath sets the path component of the redirect URI (default: "/").
// The path must start with "/" and must not contain ".." segments.
func WithCallbackPath(path string) CallbackOption {
	return func(c *callbackConfig) {
		c.path = path
	}
}

// validateCallbackConfig validates the callback configuration.
func validateCallbackConfig(cfg *callbackConfig) error {
	if !allowedCallbackHosts[cfg.host] {
		return fmt.Errorf("callback host %q is not allowed: must be localhost, 127.0.0.1, or ::1", cfg.host)
	}
	if cfg.path != "" && cfg.path != "/" {
		if !strings.HasPrefix(cfg.path, "/") {
			return fmt.Errorf("callback path %q must start with /", cfg.path)
		}
		if strings.Contains(cfg.path, "..") {
			return fmt.Errorf("callback path %q must not contain '..' segments", cfg.path)
		}
		if strings.ContainsAny(cfg.path, "?#") {
			return fmt.Errorf("callback path %q must not contain query or fragment characters", cfg.path)
		}
		if cfg.path == implicitTokenPath {
			return fmt.Errorf("callback path %q is reserved for internal use by the implicit grant flow", cfg.path)
		}
	}
	return nil
}

// newCallbackServer creates and starts a listener, returning a CallbackServer
// with the RedirectURI set (including the configured path). The caller is
// responsible for registering HTTP handlers and calling Serve.
func newCallbackServer(ctx context.Context, port int, expectedState string, opts ...CallbackOption) (*CallbackServer, *callbackConfig, error) {
	cfg := callbackConfig{
		host: "localhost",
		path: "/",
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	if err := validateCallbackConfig(&cfg); err != nil {
		return nil, nil, err
	}

	// Format the host for use in addresses and URIs.
	// IPv6 addresses need brackets in host:port notation and URLs.
	listenHost := cfg.host
	uriHost := cfg.host
	if strings.Contains(cfg.host, ":") {
		listenHost = "[" + cfg.host + "]"
		uriHost = "[" + cfg.host + "]"
	}

	addr := listenHost + ":0"
	if port > 0 {
		addr = fmt.Sprintf("%s:%d", listenHost, port)
	}

	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return nil, nil, fmt.Errorf("starting local redirect server on %s: %w", addr, err)
	}

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		listener.Close()
		return nil, nil, fmt.Errorf("unexpected listener address type: %T", listener.Addr())
	}

	// Build the origin (scheme://host:port) separately for CORS checks.
	origin := fmt.Sprintf("http://%s:%d", uriHost, tcpAddr.Port)

	// Build the full redirect URI including the path.
	// When path is the default "/", omit it to preserve the traditional
	// redirect URI format (http://host:port) expected by existing OAuth
	// providers that do exact string matching.
	redirectURI := origin
	if cfg.path != "" && cfg.path != "/" {
		redirectURI += cfg.path
	}

	cs := &CallbackServer{
		RedirectURI:   redirectURI,
		origin:        origin,
		listener:      listener,
		resultCh:      make(chan CallbackResult, 1),
		expectedState: expectedState,
	}

	return cs, &cfg, nil
}

// CallbackServer manages a local HTTP server that listens for an OAuth redirect
// and captures the authorization code.
type CallbackServer struct {
	// RedirectURI is the localhost URI the identity provider should redirect to
	// (e.g. "http://localhost:54321/auth/callback").
	RedirectURI string

	// origin is the scheme://host:port portion of the redirect URI, used for
	// CORS Origin header validation in the implicit grant flow.
	origin string

	listener net.Listener
	server   *http.Server
	resultCh chan CallbackResult

	expectedState string
	resultOnce    sync.Once
}

// StartCallbackServer creates and starts a local HTTP server on a localhost
// port. When port is 0 an ephemeral port is chosen by the OS; when port > 0
// the server binds to that specific port so the redirect URI is predictable
// (useful when the app registration only allows specific redirect URIs).
//
// expectedState is the OAuth state parameter that must match the value returned
// in the callback. If non-empty, the callback handler rejects responses whose
// state does not match, preventing CSRF attacks. The state is set before the
// server begins accepting connections to close any race window.
//
// Options allow configuring the callback host (default "localhost") and path
// (default "/") for OAuth2 servers that validate the exact redirect URI.
//
// The server waits for a single OAuth redirect, extracts the authorization
// code (or error), and sends it on the channel returned by ResultChan().
//
// The caller is responsible for calling Close() when done.
func StartCallbackServer(ctx context.Context, port int, expectedState string, opts ...CallbackOption) (*CallbackServer, error) {
	cs, cfg, err := newCallbackServer(ctx, port, expectedState, opts...)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc(cfg.path, cs.handleCallback)

	cs.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 30 * time.Second,
	}

	go func() {
		if sErr := cs.server.Serve(cs.listener); sErr != nil && sErr != http.ErrServerClosed {
			cs.sendResult(CallbackResult{Err: fmt.Errorf("redirect server error: %w", sErr)})
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

// sendResult delivers exactly one result to the channel. Subsequent calls are
// no-ops, preventing goroutine leaks when duplicate requests or server errors
// arrive after the first result has already been consumed.
func (cs *CallbackServer) sendResult(r CallbackResult) {
	cs.resultOnce.Do(func() {
		cs.resultCh <- r
	})
}

// authErrorHTML returns an HTML error page with the error message and
// troubleshooting advice. The errMsg must already be HTML-escaped.
func authErrorHTML(escapedErrMsg string) string {
	return fmt.Sprintf(`<html><body>
<h1>Authentication Failed</h1>
<p>%s</p>
<h2>Troubleshooting</h2>
<ul>
<li>Open a browser and log in to your identity provider (e.g. Azure, GitHub, Google) before retrying the CLI login.</li>
<li>Ensure your account has the required permissions for the requested scopes.</li>
<li>If you need additional scopes (e.g. for GitHub), re-run login with the --scope flag: <code>auth login &lt;handler&gt; --scope &lt;scope&gt;</code></li>
<li>Check that cookies and JavaScript are enabled in the browser that opened this page.</li>
</ul>
<p>You can close this window.</p>
</body></html>`, escapedErrMsg)
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
		cs.sendResult(CallbackResult{Err: fmt.Errorf("OAuth error: %s", errMsg)})
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, authErrorHTML(html.EscapeString(errMsg)))
		return
	}

	// Validate state parameter to prevent CSRF attacks.
	// expectedState is set at construction before Serve() begins, so no
	// synchronization is required.
	if cs.expectedState != "" {
		if r.URL.Query().Get("state") != cs.expectedState {
			errMsg := "state parameter mismatch (possible CSRF attack)"
			cs.sendResult(CallbackResult{Err: fmt.Errorf("OAuth error: %s", errMsg)})
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, authErrorHTML(html.EscapeString(errMsg)))
			return
		}
	}

	cs.sendResult(CallbackResult{Code: code})
	fmt.Fprint(w, "<html><body><h1>Authentication Successful</h1><p>You can close this window and return to the terminal.</p></body></html>")
}

// StartImplicitCallbackServer creates a local HTTP server for the OAuth2
// implicit grant flow (response_type=token). The identity provider redirects
// to the callback URL with the access token in the URL fragment
// (#access_token=...). Because browsers do not send fragments to the server,
// the server serves an HTML page with JavaScript that extracts the fragment
// and POSTs the token back to a /token endpoint on the same server.
//
// If expectedState is non-empty, a provided state value must match it. Unlike
// StartCallbackServer, the implicit flow accepts a missing state value and
// only rejects explicit mismatches.
func StartImplicitCallbackServer(ctx context.Context, port int, expectedState string, opts ...CallbackOption) (*CallbackServer, error) {
	cs, cfg, err := newCallbackServer(ctx, port, expectedState, opts...)
	if err != nil {
		return nil, err
	}

	// For implicit flow, the configured path serves the HTML page that extracts
	// the fragment, and /token receives the POSTed token data.
	mux := http.NewServeMux()
	mux.HandleFunc(cfg.path, cs.handleImplicitCallback)
	mux.HandleFunc(implicitTokenPath, cs.handleImplicitTokenPost)

	cs.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 30 * time.Second,
	}

	go func() {
		if sErr := cs.server.Serve(cs.listener); sErr != nil && sErr != http.ErrServerClosed {
			cs.sendResult(CallbackResult{Err: fmt.Errorf("redirect server error: %w", sErr)})
		}
	}()

	return cs, nil
}

// handleImplicitCallback serves the HTML page that extracts the access token
// from the URL fragment and POSTs it back to the local server.
func (cs *CallbackServer) handleImplicitCallback(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The JavaScript extracts fragment parameters and POSTs them as form data
	// to /token on the same origin. This runs entirely on localhost.
	fmt.Fprint(w, implicitCallbackHTML)
}

const maxImplicitTokenBody = 8192

// handleImplicitTokenPost receives the token POSTed by the JavaScript on the
// implicit grant callback page.
func (cs *CallbackServer) handleImplicitTokenPost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Reject cross-origin requests. A simple POST with application/x-www-form-urlencoded
	// Content-Type is not subject to CORS preflight, so a malicious page could inject
	// a forged token. When Origin is present it must match the local callback server's
	// origin (scheme://host:port), not the full redirect URI which may include a path.
	if origin := r.Header.Get("Origin"); origin != "" && origin != cs.origin {
		http.Error(w, "cross-origin request rejected", http.StatusForbidden)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxImplicitTokenBody))
	if err != nil {
		cs.sendResult(CallbackResult{Err: fmt.Errorf("read implicit token POST: %w", err)})
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	params, err := url.ParseQuery(string(body))
	if err != nil {
		cs.sendResult(CallbackResult{Err: fmt.Errorf("parse implicit token POST: %w", err)})
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Validate state parameter first to reject CSRF before processing any payload.
	// Some OAuth2 servers omit state from the implicit grant fragment.
	// Per RFC 6749 §4.2.2, state is only REQUIRED in the response if it was present
	// in the request, but not all servers comply. Accept missing state (server didn't
	// echo it back); reject only when the server returns a *different* state value.
	if cs.expectedState != "" {
		returnedState := params.Get("state")
		if returnedState != "" && returnedState != cs.expectedState {
			errMsg := "state parameter mismatch (possible CSRF attack)"
			cs.sendResult(CallbackResult{Err: fmt.Errorf("OAuth error: %s", errMsg)})
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, authErrorHTML(html.EscapeString(errMsg)))
			return
		}
	}

	// Check for OAuth error in fragment
	if errCode := params.Get("error"); errCode != "" {
		errDesc := params.Get("error_description")
		errMsg := errCode
		if errDesc != "" {
			errMsg = fmt.Sprintf("%s: %s", errCode, errDesc)
		}
		cs.sendResult(CallbackResult{Err: fmt.Errorf("OAuth error: %s", errMsg)})
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, authErrorHTML(html.EscapeString(errMsg)))
		return
	}

	accessToken := params.Get("access_token")
	if accessToken == "" {
		cs.sendResult(CallbackResult{Err: fmt.Errorf("OAuth error: no access_token in redirect fragment")})
		http.Error(w, "missing access_token", http.StatusBadRequest)
		return
	}

	cs.sendResult(CallbackResult{
		AccessToken: accessToken,
		TokenType:   params.Get("token_type"),
		ExpiresIn:   params.Get("expires_in"),
	})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, "<html><body><h1>Authentication Successful</h1><p>You can close this window and return to the terminal.</p></body></html>")
}

// implicitCallbackHTML is the HTML page served by the implicit grant callback
// server. It extracts the access token from the URL fragment (#access_token=...)
// and POSTs it to /token on the same localhost server.
const implicitCallbackHTML = `<!DOCTYPE html>
<html><head><title>Authentication</title></head>
<body>
<h1>Completing authentication...</h1>
<p id="status">Processing...</p>
<script>
(function() {
  var hash = window.location.hash.substring(1);
  if (!hash) {
    document.getElementById("status").textContent = "No token received.";
    return;
  }
  var xhr = new XMLHttpRequest();
  xhr.open("POST", "` + implicitTokenPath + `", true);
  xhr.setRequestHeader("Content-Type", "application/x-www-form-urlencoded");
  xhr.onload = function() {
    if (xhr.status === 200) {
      document.getElementById("status").textContent = "Authentication successful. You can close this window.";
    } else {
      document.getElementById("status").textContent = "Authentication failed: " + xhr.statusText;
    }
  };
  xhr.onerror = function() {
    document.getElementById("status").textContent = "Failed to communicate with local server.";
  };
  xhr.send(hash);
  history.replaceState(null, "", window.location.pathname);
})();
</script>
</body></html>`
