// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package oauth2

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandler_Login_InteractiveBrowserFallbackPrintsURLAndWarns(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()

	// Empty PATH so both the bogus browser and the OS default launcher fail deterministically.
	t.Setenv("PATH", t.TempDir())
	t.Setenv("WSL_DISTRO_NAME", "")
	t.Setenv("WSL_INTEROP", "")

	store := secrets.NewMockStore()
	h, err := New(config.CustomOAuth2Config{
		Name:         "test-browser-pref",
		ClientID:     "cid",
		AuthorizeURL: srv.URL + "/authorize",
		TokenURL:     srv.URL + "/token",
	}, WithSecretStore(store), WithHTTPClient(srv.Client()))
	require.NoError(t, err)

	var outBuf, errBuf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &outBuf, &errBuf, false)
	w := writer.New(ioStreams, settings.NewCliParams())
	ctx := writer.WithWriter(context.Background(), w)

	urlReady := make(chan string, 1)
	loginDone := make(chan error, 1)
	go func() {
		_, lErr := h.Login(ctx, auth.LoginOptions{
			Flow:    auth.FlowInteractive,
			Browser: "'missing-browser-xyz'",
			Timeout: 5 * time.Second,
			DeviceCodeCallback: func(_, verificationURI, _ string) {
				select {
				case urlReady <- verificationURI:
				default:
				}
			},
		})
		loginDone <- lErr
	}()

	var authURL string
	select {
	case authURL = <-urlReady:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for manual auth URL fallback callback")
	}

	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	redirectURI := parsed.Query().Get("redirect_uri")
	state := parsed.Query().Get("state")

	resp, err := http.Get(redirectURI + "/?code=ok-code&state=" + url.QueryEscape(state)) //nolint:noctx
	require.NoError(t, err)
	_ = resp.Body.Close()

	require.NoError(t, <-loginDone)
	assert.Contains(t, outBuf.String(), `configured browser "missing-browser-xyz" could not be launched`)
}

func TestHandler_NotifyManualAuthURL_PlainWriterFallback(t *testing.T) {
	t.Parallel()

	srv := newTestOAuthServer(t)
	defer srv.Close()

	store := secrets.NewMockStore()
	h, err := New(config.CustomOAuth2Config{
		Name:         "test-implicit-manual",
		ClientID:     "cid",
		AuthorizeURL: srv.URL + "/authorize",
		ResponseType: "token",
	}, WithSecretStore(store), WithHTTPClient(srv.Client()))
	require.NoError(t, err)

	h.openBrowser = func(_ context.Context, rawURL string) error {
		parsed, parseErr := url.Parse(rawURL)
		require.NoError(t, parseErr)
		redirectURI := parsed.Query().Get("redirect_uri")
		state := parsed.Query().Get("state")
		go func() {
			form := url.Values{
				"access_token": {"implicit-tok"},
				"token_type":   {"Bearer"},
				"expires_in":   {"3600"},
				"state":        {state},
			}
			origin := strings.TrimRight(redirectURI, "/")
			req, reqErr := http.NewRequest(http.MethodPost, origin+"/token", strings.NewReader(form.Encode())) //nolint:noctx
			if reqErr != nil {
				return
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Origin", origin)
			r, postErr := http.DefaultClient.Do(req)
			if postErr == nil {
				_ = r.Body.Close()
			}
		}()
		return errors.New("os browser unavailable")
	}

	var outBuf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &outBuf, &outBuf, false)
	w := writer.New(ioStreams, settings.NewCliParams())
	ctx := writer.WithWriter(context.Background(), w)

	_, err = h.Login(ctx, auth.LoginOptions{Flow: auth.FlowInteractive, Timeout: 5 * time.Second})
	require.NoError(t, err)
	assert.Contains(t, outBuf.String(), "To sign in, use a web browser to open the page:")
	assert.Contains(t, outBuf.String(), srv.URL+"/authorize")
}

func TestHandler_Login_InvalidBrowserFailsClosedAndCancelledSuppressesManualURL(t *testing.T) {
	t.Parallel()

	srv := newTestOAuthServer(t)
	defer srv.Close()

	store := secrets.NewMockStore()
	h, err := New(config.CustomOAuth2Config{
		Name:         "test-fail-closed",
		ClientID:     "cid",
		AuthorizeURL: srv.URL + "/authorize",
		TokenURL:     srv.URL + "/token",
	}, WithSecretStore(store), WithHTTPClient(srv.Client()))
	require.NoError(t, err)

	// Invalid browser preferences (including trailing control chars, whitespace-only,
	// and overlong padded values) fail closed before normalization or callback server startup.
	for _, bad := range []string{
		"--headless",
		"msedge\n",
		"   ",
		"  " + strings.Repeat("a", config.MaxAuthBrowserLength),
	} {
		_, err = h.Login(context.Background(), auth.LoginOptions{
			Flow:    auth.FlowInteractive,
			Browser: bad,
		})
		require.Error(t, err, "expected Login to reject %q", bad)
		assert.Contains(t, err.Error(), "invalid browser preference")
	}

	// Cancelled context during launchBrowser must not emit manual URL or DeviceCodeCallback.
	ctx, cancel := context.WithCancel(context.Background())
	var outBuf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &outBuf, &outBuf, false)
	w := writer.New(ioStreams, settings.NewCliParams())
	ctx = writer.WithWriter(ctx, w)

	callbackCalled := false
	h.openBrowser = func(_ context.Context, _ string) error {
		cancel()
		return context.Canceled
	}

	_, err = h.Login(ctx, auth.LoginOptions{
		Flow: auth.FlowInteractive,
		DeviceCodeCallback: func(_, _, _ string) {
			callbackCalled = true
		},
	})
	require.ErrorIs(t, err, auth.ErrTimeout)
	assert.False(t, callbackCalled, "cancelled browser launch must not fire DeviceCodeCallback")
	assert.Empty(t, outBuf.String(), "cancelled browser launch must not print manual sign-in URL")
}
