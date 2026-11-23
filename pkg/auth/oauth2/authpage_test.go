// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package oauth2

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/authpage"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandler_Login_DeferredCallbackWaitsForTokenExchange(t *testing.T) {
	t.Run("holds browser response until post-callback TokenExchange succeeds", func(t *testing.T) {
		exchangeEntered := make(chan struct{})
		releaseExchange := make(chan struct{})

		mux := http.NewServeMux()
		mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(tokenResponse{
				AccessToken: "primary-tok",
				TokenType:   "Bearer",
				ExpiresIn:   3600,
			})
		})
		mux.HandleFunc("/exchange", func(w http.ResponseWriter, _ *http.Request) {
			close(exchangeEntered)
			<-releaseExchange
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "derived-tok"})
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()

		store := secrets.NewMockStore()
		h, err := New(config.CustomOAuth2Config{
			Name:         "test-svc",
			ClientID:     "cid",
			AuthorizeURL: srv.URL + "/authorize",
			TokenURL:     srv.URL + "/token",
			TokenExchange: &config.TokenExchangeConfig{
				URL:           srv.URL + "/exchange",
				TokenJSONPath: "token",
			},
		},
			WithSecretStore(store),
			WithHTTPClient(srv.Client()),
			WithBinaryName("mycli"),
		)
		require.NoError(t, err)

		type browserResult struct {
			status int
			body   string
			csp    string
		}
		browserCh := make(chan browserResult, 1)
		h.openBrowser = func(_ context.Context, rawAuthURL string) error {
			u, parseErr := url.Parse(rawAuthURL)
			if !assert.NoError(t, parseErr) {
				return parseErr
			}
			redirectURI := u.Query().Get("redirect_uri")
			state := u.Query().Get("state")
			go func() {
				resp, getErr := http.Get(redirectURI + "/?code=code-1&state=" + url.QueryEscape(state)) //nolint:noctx
				if !assert.NoError(t, getErr) {
					return
				}
				defer func() { _ = resp.Body.Close() }()
				b, _ := io.ReadAll(resp.Body)
				browserCh <- browserResult{
					status: resp.StatusCode,
					body:   string(b),
					csp:    resp.Header.Get("Content-Security-Policy"),
				}
			}()
			return nil
		}

		loginDone := make(chan error, 1)
		go func() {
			_, lErr := h.Login(context.Background(), auth.LoginOptions{Flow: auth.FlowInteractive})
			loginDone <- lErr
		}()

		// Wait until primary code exchange already completed and post-callback TokenExchange is blocked.
		<-exchangeEntered

		// Browser response MUST NOT have completed yet!
		select {
		case premature := <-browserCh:
			t.Fatalf("browser received response before TokenExchange finished: %+v", premature)
		case <-time.After(60 * time.Millisecond):
		}

		close(releaseExchange)
		require.NoError(t, <-loginDone)

		br := <-browserCh
		assert.Equal(t, http.StatusOK, br.status)
		assert.Contains(t, br.body, `<span class="brand-wordmark">mycli</span>`)
		assert.Contains(t, br.body, "Authentication complete")
		assert.NotContains(t, br.body, "primary-tok")
		assert.NotContains(t, br.body, "derived-tok")
		assert.Contains(t, br.csp, "default-src 'none'")
	})

	t.Run("post-callback TokenExchange failure serves safe failure page with 503", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(tokenResponse{
				AccessToken: "primary-tok",
				TokenType:   "Bearer",
				ExpiresIn:   3600,
			})
		})
		mux.HandleFunc("/exchange", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"error":"secret_upstream_failure"}`, http.StatusBadGateway)
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()

		h, err := New(config.CustomOAuth2Config{
			Name:         "test-svc",
			ClientID:     "cid",
			AuthorizeURL: srv.URL + "/authorize",
			TokenURL:     srv.URL + "/token",
			TokenExchange: &config.TokenExchangeConfig{
				URL:           srv.URL + "/exchange",
				TokenJSONPath: "token",
			},
		},
			WithSecretStore(secrets.NewMockStore()),
			WithHTTPClient(srv.Client()),
			WithAuthPageRenderer(func(_ context.Context, d authpage.Data) ([]byte, error) {
				return []byte("<!doctype html><html><body>CUSTOM-" + string(d.State) + ":" + d.BinaryName + "</body></html>"), nil
			}),
			WithBinaryName("acme"),
		)
		require.NoError(t, err)

		browserCh := make(chan *http.Response, 1)
		bodyCh := make(chan string, 1)
		h.openBrowser = func(_ context.Context, rawAuthURL string) error {
			u, _ := url.Parse(rawAuthURL)
			redirectURI := u.Query().Get("redirect_uri")
			state := u.Query().Get("state")
			go func() {
				resp, getErr := http.Get(redirectURI + "/?code=code-2&state=" + url.QueryEscape(state)) //nolint:noctx
				if getErr == nil {
					b, _ := io.ReadAll(resp.Body)
					_ = resp.Body.Close()
					bodyCh <- string(b)
					browserCh <- resp
				}
			}()
			return nil
		}

		_, loginErr := h.Login(context.Background(), auth.LoginOptions{Flow: auth.FlowInteractive})
		require.Error(t, loginErr)

		resp := <-browserCh
		body := <-bodyCh
		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
		assert.Equal(t, "<!doctype html><html><body>CUSTOM-failure:acme</body></html>", body)
		assert.NotContains(t, body, "secret_upstream_failure")
	})

	t.Run("implicit grant Login serves custom success page on /token POST", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		defer srv.Close()

		h, err := New(config.CustomOAuth2Config{
			Name:         "imp-svc",
			ClientID:     "cid",
			AuthorizeURL: srv.URL + "/authorize",
			ResponseType: "token",
		},
			WithSecretStore(secrets.NewMockStore()),
			WithHTTPClient(srv.Client()),
		)
		require.NoError(t, err)

		postBodyCh := make(chan string, 1)
		postStatusCh := make(chan int, 1)
		h.openBrowser = func(ctx context.Context, rawAuthURL string) error {
			u, _ := url.Parse(rawAuthURL)
			redirectURI := u.Query().Get("redirect_uri")
			state := u.Query().Get("state")
			go func() {
				req, _ := http.NewRequestWithContext(ctx, http.MethodPost, redirectURI+"/token",
					strings.NewReader("access_token=imp-secret-tok&token_type=Bearer&expires_in=1200&state="+url.QueryEscape(state)))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.Header.Set("Origin", redirectURI)
				resp, doErr := http.DefaultClient.Do(req)
				if doErr == nil {
					b, _ := io.ReadAll(resp.Body)
					_ = resp.Body.Close()
					postStatusCh <- resp.StatusCode
					postBodyCh <- string(b)
				}
			}()
			return nil
		}

		_, loginErr := h.Login(context.Background(), auth.LoginOptions{Flow: auth.FlowInteractive})
		require.NoError(t, loginErr)
		assert.Equal(t, http.StatusOK, <-postStatusCh)
		page := <-postBodyCh
		assert.Contains(t, page, "Authentication complete")
		assert.Contains(t, page, `<svg class="logo"`)
		assert.NotContains(t, page, "imp-secret-tok")
	})

	t.Run("direct callback OAuth error drains 400 failure page cleanly without socket reset", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		defer srv.Close()

		h, err := New(config.CustomOAuth2Config{
			Name:         "err-svc",
			ClientID:     "cid",
			AuthorizeURL: srv.URL + "/authorize",
			TokenURL:     srv.URL + "/token",
		},
			WithSecretStore(secrets.NewMockStore()),
			WithHTTPClient(srv.Client()),
			WithBinaryName("mycli"),
			WithAuthPageRenderer(func(_ context.Context, d authpage.Data) ([]byte, error) {
				return []byte("<!doctype html><html><body>DIRECT-ERR:" + d.BinaryName + ":" + string(d.State) + "</body></html>"), nil
			}),
		)
		require.NoError(t, err)

		type browserOutcome struct {
			status int
			body   string
			err    error
		}
		browserCh := make(chan browserOutcome, 1)
		h.openBrowser = func(_ context.Context, rawAuthURL string) error {
			u, _ := url.Parse(rawAuthURL)
			redirectURI := u.Query().Get("redirect_uri")
			state := u.Query().Get("state")
			go func() {
				resp, getErr := http.Get(redirectURI + "/?error=access_denied&error_description=confidential-consent-reason&state=" + url.QueryEscape(state)) //nolint:noctx
				if getErr != nil {
					browserCh <- browserOutcome{err: getErr}
					return
				}
				b, readErr := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				browserCh <- browserOutcome{status: resp.StatusCode, body: string(b), err: readErr}
			}()
			return nil
		}

		_, loginErr := h.Login(context.Background(), auth.LoginOptions{Flow: auth.FlowInteractive})
		require.Error(t, loginErr)

		out := <-browserCh
		require.NoError(t, out.err)
		assert.Equal(t, http.StatusBadRequest, out.status)
		assert.Equal(t, "<!doctype html><html><body>DIRECT-ERR:mycli:failure</body></html>", out.body)
		assert.NotContains(t, out.body, "confidential-consent-reason")
	})

	t.Run("blank WithBinaryName falls back to default binary name and interactive timeout closes server immediately", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		defer srv.Close()

		h, err := New(config.CustomOAuth2Config{
			Name:         "timeout-svc",
			ClientID:     "cid",
			AuthorizeURL: srv.URL + "/authorize",
			TokenURL:     srv.URL + "/token",
		},
			WithSecretStore(secrets.NewMockStore()),
			WithHTTPClient(srv.Client()),
			WithBinaryName("   "),
		)
		require.NoError(t, err)
		assert.Equal(t, "scafctl", h.effectiveBinaryName())

		ctx, cancel := context.WithCancel(context.Background())
		h.openBrowser = func(_ context.Context, _ string) error {
			// Cancel without ever hitting the localhost callback URL (!callbackHit).
			cancel()
			return nil
		}

		_, loginErr := h.Login(ctx, auth.LoginOptions{Flow: auth.FlowInteractive})
		require.ErrorIs(t, loginErr, auth.ErrTimeout)
	})

	t.Run("implicit grant post-callback TokenExchange failure serves 503 custom failure page", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/exchange", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"error":"secret_implicit_exchange_err"}`, http.StatusBadGateway)
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()

		h, err := New(config.CustomOAuth2Config{
			Name:         "imp-fail-svc",
			ClientID:     "cid",
			AuthorizeURL: srv.URL + "/authorize",
			ResponseType: "token",
			TokenExchange: &config.TokenExchangeConfig{
				URL:           srv.URL + "/exchange",
				TokenJSONPath: "token",
			},
		},
			WithSecretStore(secrets.NewMockStore()),
			WithHTTPClient(srv.Client()),
			WithBinaryName("acme"),
			WithAuthPageRenderer(func(_ context.Context, d authpage.Data) ([]byte, error) {
				return []byte("<!doctype html><html><body>IMP-EXCHANGE:" + d.BinaryName + ":" + string(d.State) + "</body></html>"), nil
			}),
		)
		require.NoError(t, err)

		type postResult struct {
			status int
			body   string
			err    error
		}
		postCh := make(chan postResult, 1)
		h.openBrowser = func(_ context.Context, rawAuthURL string) error {
			u, _ := url.Parse(rawAuthURL)
			redirectURI := u.Query().Get("redirect_uri")
			state := u.Query().Get("state")
			go func() {
				reqCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				req, _ := http.NewRequestWithContext(reqCtx, http.MethodPost, redirectURI+"/token",
					strings.NewReader("access_token=imp-tok&token_type=Bearer&expires_in=1200&state="+url.QueryEscape(state)))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.Header.Set("Origin", redirectURI)
				resp, doErr := http.DefaultClient.Do(req)
				if doErr != nil {
					postCh <- postResult{err: doErr}
					return
				}
				b, readErr := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				postCh <- postResult{status: resp.StatusCode, body: string(b), err: readErr}
			}()
			return nil
		}

		_, loginErr := h.Login(context.Background(), auth.LoginOptions{Flow: auth.FlowInteractive})
		require.Error(t, loginErr)
		got := <-postCh
		require.NoError(t, got.err)
		assert.Equal(t, http.StatusServiceUnavailable, got.status)
		assert.Equal(t, "<!doctype html><html><body>IMP-EXCHANGE:acme:failure</body></html>", got.body)
		assert.NotContains(t, got.body, "secret_implicit_exchange_err")
	})

	t.Run("implicit grant /token OAuth error drains 400 safe failure page", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		defer srv.Close()

		h, err := New(config.CustomOAuth2Config{
			Name:         "imp-err-svc",
			ClientID:     "cid",
			AuthorizeURL: srv.URL + "/authorize",
			ResponseType: "token",
		},
			WithSecretStore(secrets.NewMockStore()),
			WithHTTPClient(srv.Client()),
			WithBinaryName("acme"),
			WithAuthPageRenderer(func(_ context.Context, d authpage.Data) ([]byte, error) {
				return []byte("<!doctype html><html><body>IMP-ERR:" + d.BinaryName + ":" + string(d.State) + "</body></html>"), nil
			}),
		)
		require.NoError(t, err)

		type postResult struct {
			status int
			body   string
			err    error
		}
		postCh := make(chan postResult, 1)
		h.openBrowser = func(_ context.Context, rawAuthURL string) error {
			u, _ := url.Parse(rawAuthURL)
			redirectURI := u.Query().Get("redirect_uri")
			state := u.Query().Get("state")
			go func() {
				reqCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				req, _ := http.NewRequestWithContext(reqCtx, http.MethodPost, redirectURI+"/token",
					strings.NewReader("error=access_denied&error_description=secret-implicit-desc&state="+url.QueryEscape(state)))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.Header.Set("Origin", redirectURI)
				resp, doErr := http.DefaultClient.Do(req)
				if doErr != nil {
					postCh <- postResult{err: doErr}
					return
				}
				b, readErr := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				postCh <- postResult{status: resp.StatusCode, body: string(b), err: readErr}
			}()
			return nil
		}

		_, loginErr := h.Login(context.Background(), auth.LoginOptions{Flow: auth.FlowInteractive})
		require.Error(t, loginErr)
		got := <-postCh
		require.NoError(t, got.err)
		assert.Equal(t, http.StatusBadRequest, got.status)
		assert.Equal(t, "<!doctype html><html><body>IMP-ERR:acme:failure</body></html>", got.body)
		assert.NotContains(t, got.body, "secret-implicit-desc")
	})
}
