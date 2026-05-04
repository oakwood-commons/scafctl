// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package oauth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStartCallbackServer(t *testing.T) {
	ctx := context.Background()
	cs, err := StartCallbackServer(ctx, 0, "")
	require.NoError(t, err)
	defer cs.Close()

	assert.Contains(t, cs.RedirectURI, "http://localhost:")
}

func TestCallbackServer_ReceivesCode(t *testing.T) {
	ctx := context.Background()
	cs, err := StartCallbackServer(ctx, 0, "")
	require.NoError(t, err)
	defer cs.Close()

	// Simulate the OAuth redirect
	resp, err := http.Get(cs.RedirectURI + "/?code=test-auth-code") //nolint:noctx // test code
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	select {
	case result := <-cs.ResultChan():
		assert.NoError(t, result.Err)
		assert.Equal(t, "test-auth-code", result.Code)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}
}

func TestCallbackServer_ReceivesError(t *testing.T) {
	ctx := context.Background()
	cs, err := StartCallbackServer(ctx, 0, "")
	require.NoError(t, err)
	defer cs.Close()

	// Simulate an OAuth error redirect
	resp, err := http.Get(cs.RedirectURI + "/?error=access_denied&error_description=user+cancelled") //nolint:noctx // test code
	require.NoError(t, err)
	defer resp.Body.Close()

	select {
	case result := <-cs.ResultChan():
		assert.Error(t, result.Err)
		assert.Contains(t, result.Err.Error(), "access_denied")
		assert.Contains(t, result.Err.Error(), "user cancelled")
		assert.Empty(t, result.Code)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}
}

func TestCallbackServer_NoCode(t *testing.T) {
	ctx := context.Background()
	cs, err := StartCallbackServer(ctx, 0, "")
	require.NoError(t, err)
	defer cs.Close()

	// No code or error params
	resp, err := http.Get(cs.RedirectURI + "/") //nolint:noctx // test code
	require.NoError(t, err)
	defer resp.Body.Close()

	select {
	case result := <-cs.ResultChan():
		assert.Error(t, result.Err)
		assert.Contains(t, result.Err.Error(), "no authorization code received")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}
}

func TestCallbackServer_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := StartCallbackServer(ctx, 0, "")
	// Should fail because the context is already cancelled
	assert.Error(t, err)
}

func TestCallbackServer_Close(t *testing.T) {
	ctx := context.Background()
	cs, err := StartCallbackServer(ctx, 0, "")
	require.NoError(t, err)

	err = cs.Close()
	assert.NoError(t, err)

	// Verify the server is no longer accepting connections
	resp, err := http.Get(cs.RedirectURI + "/") //nolint:noctx // test code
	if resp != nil {
		resp.Body.Close()
	}
	assert.Error(t, err, "request should fail after server is closed")
}

func TestCallbackServer_HTMLEscapesErrors(t *testing.T) {
	ctx := context.Background()
	cs, err := StartCallbackServer(ctx, 0, "")
	require.NoError(t, err)
	defer cs.Close()

	// Send an error with HTML characters
	resp, err := http.Get(fmt.Sprintf("%s/?error=%s", cs.RedirectURI, "<script>alert('xss')</script>")) //nolint:noctx // test code
	require.NoError(t, err)
	defer resp.Body.Close()

	// The error channel should still receive the result
	select {
	case result := <-cs.ResultChan():
		assert.Error(t, result.Err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}
}

func TestStartCallbackServer_FixedPort(t *testing.T) {
	ctx := context.Background()

	// Use a fixed port; pick a high port unlikely to collide.
	cs, err := StartCallbackServer(ctx, 18947, "")
	require.NoError(t, err)
	defer cs.Close()

	assert.Equal(t, "http://localhost:18947", cs.RedirectURI)

	// Verify it actually serves on that port.
	resp, err := http.Get(cs.RedirectURI + "/?code=fixed-port-code") //nolint:noctx // test code
	require.NoError(t, err)
	defer resp.Body.Close()

	select {
	case result := <-cs.ResultChan():
		assert.NoError(t, result.Err)
		assert.Equal(t, "fixed-port-code", result.Code)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}
}

func TestStartCallbackServer_FixedPortAlreadyInUse(t *testing.T) {
	ctx := context.Background()

	// Bind a port first.
	cs1, err := StartCallbackServer(ctx, 18948, "")
	require.NoError(t, err)
	defer cs1.Close()

	// Second attempt on the same port should fail.
	_, err = StartCallbackServer(ctx, 18948, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "18948")
}

func TestCallbackServer_StateValidation_Matches(t *testing.T) {
	ctx := context.Background()
	cs, err := StartCallbackServer(ctx, 0, "random-state-123")
	require.NoError(t, err)
	defer cs.Close()

	resp, err := http.Get(cs.RedirectURI + "/?code=test-code&state=random-state-123") //nolint:noctx // test code
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	select {
	case result := <-cs.ResultChan():
		assert.NoError(t, result.Err)
		assert.Equal(t, "test-code", result.Code)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}
}

func TestCallbackServer_StateValidation_Mismatch(t *testing.T) {
	ctx := context.Background()
	cs, err := StartCallbackServer(ctx, 0, "expected-state")
	require.NoError(t, err)
	defer cs.Close()

	resp, err := http.Get(cs.RedirectURI + "/?code=test-code&state=wrong-state") //nolint:noctx // test code
	require.NoError(t, err)
	defer resp.Body.Close()

	select {
	case result := <-cs.ResultChan():
		assert.Error(t, result.Err)
		assert.Contains(t, result.Err.Error(), "state parameter mismatch")
		assert.Empty(t, result.Code)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}
}

func TestCallbackServer_StateValidation_Missing(t *testing.T) {
	ctx := context.Background()
	cs, err := StartCallbackServer(ctx, 0, "expected-state")
	require.NoError(t, err)
	defer cs.Close()

	// Code present but state missing from callback
	resp, err := http.Get(cs.RedirectURI + "/?code=test-code") //nolint:noctx // test code
	require.NoError(t, err)
	defer resp.Body.Close()

	select {
	case result := <-cs.ResultChan():
		assert.Error(t, result.Err)
		assert.Contains(t, result.Err.Error(), "state parameter mismatch")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}
}

func TestCallbackServer_NoExpectedState_SkipsValidation(t *testing.T) {
	ctx := context.Background()
	cs, err := StartCallbackServer(ctx, 0, "")
	require.NoError(t, err)
	defer cs.Close()

	// Empty state skips validation — backward-compatible behavior
	resp, err := http.Get(cs.RedirectURI + "/?code=test-code") //nolint:noctx // test code
	require.NoError(t, err)
	defer resp.Body.Close()

	select {
	case result := <-cs.ResultChan():
		assert.NoError(t, result.Err)
		assert.Equal(t, "test-code", result.Code)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}
}

// ---------- implicit grant callback tests ----------

func TestStartImplicitCallbackServer(t *testing.T) {
	ctx := context.Background()
	cs, err := StartImplicitCallbackServer(ctx, 0, "")
	require.NoError(t, err)
	defer cs.Close()

	assert.Contains(t, cs.RedirectURI, "http://localhost:")
}

func TestImplicitCallbackServer_ServesHTMLPage(t *testing.T) {
	ctx := context.Background()
	cs, err := StartImplicitCallbackServer(ctx, 0, "")
	require.NoError(t, err)
	defer cs.Close()

	resp, err := http.Get(cs.RedirectURI + "/") //nolint:noctx // test code
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, string(body), "window.location.hash")
	assert.Contains(t, string(body), "/token")
}

func TestImplicitCallbackServer_ReceivesToken(t *testing.T) {
	ctx := context.Background()
	cs, err := StartImplicitCallbackServer(ctx, 0, "")
	require.NoError(t, err)
	defer cs.Close()

	// Simulate the JavaScript POSTing the fragment data
	tokenData := "access_token=test-implicit-token&token_type=Bearer&expires_in=3600"
	resp, err := http.Post(cs.RedirectURI+"/token", "application/x-www-form-urlencoded", strings.NewReader(tokenData)) //nolint:noctx // test code
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	select {
	case result := <-cs.ResultChan():
		assert.NoError(t, result.Err)
		assert.Equal(t, "test-implicit-token", result.AccessToken)
		assert.Equal(t, "Bearer", result.TokenType)
		assert.Equal(t, "3600", result.ExpiresIn)
		assert.Empty(t, result.Code)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}
}

func TestImplicitCallbackServer_MissingToken(t *testing.T) {
	ctx := context.Background()
	cs, err := StartImplicitCallbackServer(ctx, 0, "")
	require.NoError(t, err)
	defer cs.Close()

	// POST with no access_token
	resp, err := http.Post(cs.RedirectURI+"/token", "application/x-www-form-urlencoded", strings.NewReader("token_type=Bearer")) //nolint:noctx // test code
	require.NoError(t, err)
	defer resp.Body.Close()

	select {
	case result := <-cs.ResultChan():
		assert.Error(t, result.Err)
		assert.Contains(t, result.Err.Error(), "no access_token")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}
}

func TestImplicitCallbackServer_OAuthError(t *testing.T) {
	ctx := context.Background()
	cs, err := StartImplicitCallbackServer(ctx, 0, "")
	require.NoError(t, err)
	defer cs.Close()

	errorData := "error=access_denied&error_description=user+cancelled"
	resp, err := http.Post(cs.RedirectURI+"/token", "application/x-www-form-urlencoded", strings.NewReader(errorData)) //nolint:noctx // test code
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	select {
	case result := <-cs.ResultChan():
		assert.Error(t, result.Err)
		assert.Contains(t, result.Err.Error(), "access_denied")
		assert.Contains(t, result.Err.Error(), "user cancelled")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}
}

func TestImplicitCallbackServer_StateValidation_Matches(t *testing.T) {
	ctx := context.Background()
	cs, err := StartImplicitCallbackServer(ctx, 0, "my-state-123")
	require.NoError(t, err)
	defer cs.Close()

	tokenData := "access_token=tok&token_type=Bearer&state=my-state-123"
	resp, err := http.Post(cs.RedirectURI+"/token", "application/x-www-form-urlencoded", strings.NewReader(tokenData)) //nolint:noctx // test code
	require.NoError(t, err)
	defer resp.Body.Close()

	select {
	case result := <-cs.ResultChan():
		assert.NoError(t, result.Err)
		assert.Equal(t, "tok", result.AccessToken)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}
}

func TestImplicitCallbackServer_StateValidation_Mismatch(t *testing.T) {
	ctx := context.Background()
	cs, err := StartImplicitCallbackServer(ctx, 0, "expected-state")
	require.NoError(t, err)
	defer cs.Close()

	tokenData := "access_token=tok&token_type=Bearer&state=wrong-state"
	resp, err := http.Post(cs.RedirectURI+"/token", "application/x-www-form-urlencoded", strings.NewReader(tokenData)) //nolint:noctx // test code
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	select {
	case result := <-cs.ResultChan():
		assert.Error(t, result.Err)
		assert.Contains(t, result.Err.Error(), "state parameter mismatch")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}
}

func TestImplicitCallbackServer_StateOmittedByServer(t *testing.T) {
	ctx := context.Background()
	cs, err := StartImplicitCallbackServer(ctx, 0, "my-state")
	require.NoError(t, err)
	defer cs.Close()

	// Server omits state from fragment — should be accepted per RFC 6749 §4.2.2
	tokenData := "access_token=tok&token_type=Bearer"
	resp, err := http.Post(cs.RedirectURI+"/token", "application/x-www-form-urlencoded", strings.NewReader(tokenData)) //nolint:noctx // test code
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	select {
	case result := <-cs.ResultChan():
		assert.NoError(t, result.Err)
		assert.Equal(t, "tok", result.AccessToken)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}
}

func TestImplicitCallbackServer_RejectsGET(t *testing.T) {
	ctx := context.Background()
	cs, err := StartImplicitCallbackServer(ctx, 0, "")
	require.NoError(t, err)
	defer cs.Close()

	resp, err := http.Get(cs.RedirectURI + "/token?access_token=tok") //nolint:noctx // test code
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestImplicitCallbackServer_RejectsCrossOrigin(t *testing.T) {
	ctx := context.Background()
	cs, err := StartImplicitCallbackServer(ctx, 0, "")
	require.NoError(t, err)
	defer cs.Close()

	tokenData := "access_token=tok&token_type=Bearer"
	req, err := http.NewRequest(http.MethodPost, cs.origin+"/token", strings.NewReader(tokenData)) //nolint:noctx // test code
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://evil.example.com")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestImplicitCallbackServer_AllowsSameOrigin(t *testing.T) {
	ctx := context.Background()
	cs, err := StartImplicitCallbackServer(ctx, 0, "")
	require.NoError(t, err)
	defer cs.Close()

	tokenData := "access_token=tok&token_type=Bearer"
	req, err := http.NewRequest(http.MethodPost, cs.origin+"/token", strings.NewReader(tokenData)) //nolint:noctx // test code
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", cs.origin)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	select {
	case result := <-cs.ResultChan():
		assert.NoError(t, result.Err)
		assert.Equal(t, "tok", result.AccessToken)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}
}

func TestAuthErrorHTML_ContainsTroubleshooting(t *testing.T) {
	page := authErrorHTML("something went wrong")
	assert.Contains(t, page, "Authentication Failed")
	assert.Contains(t, page, "something went wrong")
	assert.Contains(t, page, "Troubleshooting")
	assert.Contains(t, page, "log in to your identity provider")
}

func TestCallbackServer_ErrorPageIncludesTroubleshooting(t *testing.T) {
	ctx := context.Background()
	cs, err := StartCallbackServer(ctx, 0, "")
	require.NoError(t, err)
	defer cs.Close()

	resp, err := http.Get(fmt.Sprintf("%s/?error=access_denied&error_description=user+not+found", cs.RedirectURI)) //nolint:noctx // test code
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "Troubleshooting")
	assert.Contains(t, string(body), "log in to your identity provider")

	select {
	case result := <-cs.ResultChan():
		assert.Error(t, result.Err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}
}

func TestStartCallbackServer_WithCallbackHost(t *testing.T) {
	ctx := context.Background()
	cs, err := StartCallbackServer(ctx, 0, "", WithCallbackHost("127.0.0.1"))
	require.NoError(t, err)
	defer cs.Close()

	assert.Contains(t, cs.RedirectURI, "http://127.0.0.1:")
	assert.NotContains(t, cs.RedirectURI, "localhost")

	resp, err := http.Get(cs.RedirectURI + "/?code=host-test") //nolint:noctx // test code
	require.NoError(t, err)
	defer resp.Body.Close()

	select {
	case result := <-cs.ResultChan():
		assert.NoError(t, result.Err)
		assert.Equal(t, "host-test", result.Code)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}
}

func TestStartCallbackServer_WithCallbackPath(t *testing.T) {
	ctx := context.Background()
	cs, err := StartCallbackServer(ctx, 0, "", WithCallbackPath("/auth/callback"))
	require.NoError(t, err)
	defer cs.Close()

	assert.Contains(t, cs.RedirectURI, "/auth/callback")

	resp, err := http.Get(cs.RedirectURI + "?code=path-test") //nolint:noctx // test code
	require.NoError(t, err)
	defer resp.Body.Close()

	select {
	case result := <-cs.ResultChan():
		assert.NoError(t, result.Err)
		assert.Equal(t, "path-test", result.Code)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}
}

func TestStartCallbackServer_WithBothHostAndPath(t *testing.T) {
	ctx := context.Background()
	cs, err := StartCallbackServer(ctx, 18949, "",
		WithCallbackHost("127.0.0.1"),
		WithCallbackPath("/oauth/redirect"),
	)
	require.NoError(t, err)
	defer cs.Close()

	assert.Equal(t, "http://127.0.0.1:18949/oauth/redirect", cs.RedirectURI)

	resp, err := http.Get(cs.RedirectURI + "?code=both-test") //nolint:noctx // test code
	require.NoError(t, err)
	defer resp.Body.Close()

	select {
	case result := <-cs.ResultChan():
		assert.NoError(t, result.Err)
		assert.Equal(t, "both-test", result.Code)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}
}

func TestStartCallbackServer_InvalidHost(t *testing.T) {
	tests := []struct {
		name string
		host string
	}{
		{"external IP", "0.0.0.0"},
		{"remote host", "evil.com"},
		{"private IP", "192.168.1.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			_, err := StartCallbackServer(ctx, 0, "", WithCallbackHost(tt.host))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "not allowed")
		})
	}
}

func TestStartCallbackServer_InvalidPath(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"no leading slash", "auth/callback"},
		{"path traversal", "/auth/../secret"},
		{"double dot", "/.."},
		{"reserved /token path", "/token"},
		{"query string", "/callback?code=abc"},
		{"fragment", "/callback#section"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			_, err := StartCallbackServer(ctx, 0, "", WithCallbackPath(tt.path))
			require.Error(t, err)
		})
	}
}

func TestStartImplicitCallbackServer_InvalidHost(t *testing.T) {
	ctx := context.Background()
	_, err := StartImplicitCallbackServer(ctx, 0, "", WithCallbackHost("0.0.0.0"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not allowed")
}

func TestStartImplicitCallbackServer_WithPath(t *testing.T) {
	ctx := context.Background()
	cs, err := StartImplicitCallbackServer(ctx, 0, "",
		WithCallbackHost("127.0.0.1"),
		WithCallbackPath("/auth/callback"),
	)
	require.NoError(t, err)
	defer cs.Close()

	// Verify the RedirectURI includes the path
	assert.Contains(t, cs.RedirectURI, "http://127.0.0.1:")
	assert.Contains(t, cs.RedirectURI, "/auth/callback")

	// Verify that the origin field is scheme://host:port only (no path)
	assert.Equal(t, cs.origin, strings.TrimSuffix(cs.RedirectURI, "/auth/callback"))
}

func TestStartCallbackServer_IPv6Host(t *testing.T) {
	ctx := context.Background()
	cs, err := StartCallbackServer(ctx, 0, "", WithCallbackHost("::1"))
	require.NoError(t, err)
	defer cs.Close()

	// IPv6 address must be bracketed in the URI
	assert.Contains(t, cs.RedirectURI, "http://[::1]:")
}

func TestStartCallbackServer_DefaultPathOmitsSlash(t *testing.T) {
	ctx := context.Background()
	cs, err := StartCallbackServer(ctx, 0, "")
	require.NoError(t, err)
	defer cs.Close()

	// Default path "/" is omitted from the redirect URI to preserve the
	// traditional format expected by OAuth providers doing exact matching.
	assert.False(t, strings.HasSuffix(cs.RedirectURI, "/"),
		"expected default RedirectURI %q to not end with /", cs.RedirectURI)
}

func TestImplicitCallbackServer_OriginCheckWithPath(t *testing.T) {
	ctx := context.Background()
	cs, err := StartImplicitCallbackServer(ctx, 0, "",
		WithCallbackPath("/auth/callback"),
	)
	require.NoError(t, err)
	defer cs.Close()

	// POST to /token with the correct Origin (scheme://host:port, no path).
	// This should succeed -- the bug was that it compared Origin against the
	// full RedirectURI which includes the path, causing a 403.
	tokenURL := cs.origin + "/token"
	body := strings.NewReader("access_token=test-token&token_type=bearer")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", cs.origin)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	select {
	case result := <-cs.ResultChan():
		assert.NoError(t, result.Err)
		assert.Equal(t, "test-token", result.AccessToken)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for implicit callback result")
	}
}

func TestValidateCallbackHostPath(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		path    string
		wantErr bool
	}{
		{"empty defaults are valid", "", "", false},
		{"localhost valid", "localhost", "/", false},
		{"127.0.0.1 valid", "127.0.0.1", "/auth/callback", false},
		{"::1 valid", "::1", "/", false},
		{"external host rejected", "evil.com", "/", true},
		{"0.0.0.0 rejected", "0.0.0.0", "/", true},
		{"path traversal rejected", "localhost", "/auth/../secret", true},
		{"no leading slash rejected", "localhost", "auth/callback", true},
		{"/token reserved", "localhost", "/token", true},
		{"query string rejected", "localhost", "/callback?code=abc", true},
		{"fragment rejected", "localhost", "/callback#section", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCallbackHostPath(tt.host, tt.path)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
