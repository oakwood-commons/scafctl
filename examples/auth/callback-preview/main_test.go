// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/authpage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreviewGallery_IndexAndErrorPaths(t *testing.T) {
	var logBuf bytes.Buffer
	var flowGate taskGate
	defer flowGate.closeAndWait()
	srv := httptest.NewServer(newPreviewMux(context.Background(), 25*time.Millisecond, &logBuf, &flowGate))
	defer srv.Close()

	t.Run("gallery index lists all 8 scenarios and hold presets", func(t *testing.T) {
		resp, err := srv.Client().Get(srv.URL + "/?hold=1500ms") //nolint:noctx
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		bodyBytes, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		body := string(bodyBytes)

		for _, s := range previewScenarios() {
			assert.Contains(t, body, "scenario="+s.ID)
			assert.Contains(t, body, s.Title)
		}
		assert.Contains(t, body, "1.5s")
	})

	t.Run("non-root path returns 404", func(t *testing.T) {
		resp, err := srv.Client().Get(srv.URL + "/unknown-path") //nolint:noctx
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("unknown scenario returns 400", func(t *testing.T) {
		resp, err := srv.Client().Get(srv.URL + "/run?scenario=does-not-exist") //nolint:noctx
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})
}

func TestPreviewGallery_AllEightScenariosEndToEnd(t *testing.T) {
	tests := []struct {
		scenario       string
		wantStatus     int
		wantSubstrings []string
		wantExcluded   []string
	}{
		{
			scenario:   "scafctl-success",
			wantStatus: http.StatusOK,
			wantSubstrings: []string{
				"<title>scafctl | Authentication complete</title>",
				`<svg class="logo"`,
				"DECLARATIVE BY DESIGN.",
			},
			wantExcluded: []string{"PREVIEW-AUTH-CODE"},
		},
		{
			scenario:   "scafctl-failure",
			wantStatus: http.StatusServiceUnavailable,
			wantSubstrings: []string{
				"<title>scafctl | Authentication unsuccessful</title>",
				`<body class="failure">`,
			},
			wantExcluded: []string{"PREVIEW-AUTH-CODE"},
		},
		{
			scenario:   "acme-success",
			wantStatus: http.StatusOK,
			wantSubstrings: []string{
				"<title>acme-cli | Authentication complete</title>",
				`<span class="brand-wordmark">acme-cli</span>`,
			},
			wantExcluded: []string{"scafctl", `<svg class="logo"`},
		},
		{
			scenario:   "acme-failure",
			wantStatus: http.StatusServiceUnavailable,
			wantSubstrings: []string{
				"<title>acme-cli | Authentication unsuccessful</title>",
				`<span class="brand-wordmark">acme-cli</span>`,
				`<body class="failure">`,
			},
			wantExcluded: []string{"scafctl", `<svg class="logo"`},
		},
		{
			scenario:   "custom-success",
			wantStatus: http.StatusOK,
			wantSubstrings: []string{
				"<title>acme-cli - Custom Embedder Auth</title>",
				"Signed in to acme-cli",
				"Embedder Override (success)",
			},
			wantExcluded: []string{"DECLARATIVE BY DESIGN."},
		},
		{
			scenario:   "custom-failure",
			wantStatus: http.StatusServiceUnavailable,
			wantSubstrings: []string{
				"<title>acme-cli - Custom Embedder Auth</title>",
				"Could not sign in to acme-cli",
				"Embedder Override (failure)",
			},
			wantExcluded: []string{"DECLARATIVE BY DESIGN."},
		},
		{
			scenario:   "helper-fallback-success",
			wantStatus: http.StatusOK,
			wantSubstrings: []string{
				"<title>Authentication Successful</title>",
				"#05152a",
			},
			wantExcluded: []string{"DECLARATIVE BY DESIGN.", "scafctl"},
		},
		{
			scenario:   "helper-fallback-failure",
			wantStatus: http.StatusServiceUnavailable,
			wantSubstrings: []string{
				"<title>Authentication Failed</title>",
				"#05152a",
			},
			wantExcluded: []string{"DECLARATIVE BY DESIGN.", "scafctl"},
		},
	}

	var logBuf bytes.Buffer
	var flowGate taskGate
	defer flowGate.closeAndWait()
	srv := httptest.NewServer(newPreviewMux(context.Background(), 0, &logBuf, &flowGate))
	defer srv.Close()

	for _, tt := range tests {
		t.Run(tt.scenario, func(t *testing.T) {
			resp, err := srv.Client().Get(srv.URL + "/run?scenario=" + tt.scenario + "&hold=5ms") //nolint:noctx
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()

			assert.Equal(t, tt.wantStatus, resp.StatusCode)
			assert.Contains(t, resp.Header.Get("Content-Security-Policy"), "default-src 'none'")
			assert.Equal(t, "no-store", resp.Header.Get("Cache-Control"))

			bodyBytes, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			htmlStr := string(bodyBytes)

			for _, sub := range tt.wantSubstrings {
				assert.Contains(t, htmlStr, sub)
			}
			for _, ex := range tt.wantExcluded {
				assert.NotContains(t, htmlStr, ex)
			}
		})
	}

	t.Run("concurrent scenario requests do not race on log writer", func(t *testing.T) {
		var reqWG sync.WaitGroup
		for _, sc := range previewScenarios() {
			reqWG.Add(1)
			go func(id string) {
				defer reqWG.Done()
				resp, err := srv.Client().Get(srv.URL + "/run?scenario=" + id + "&hold=10ms") //nolint:noctx
				if assert.NoError(t, err) {
					_, _ = io.ReadAll(resp.Body)
					_ = resp.Body.Close()
				}
			}(sc.ID)
		}
		reqWG.Wait()
	})
}

func TestParseHoldOverride(t *testing.T) {
	fallback := 1500 * time.Millisecond
	assert.Equal(t, fallback, parseHoldOverride("", fallback))
	assert.Equal(t, fallback, parseHoldOverride("not-a-duration", fallback))
	assert.Equal(t, fallback, parseHoldOverride("-5s", fallback))
	assert.Equal(t, time.Duration(0), parseHoldOverride("0s", fallback))
	assert.Equal(t, 250*time.Millisecond, parseHoldOverride("250ms", fallback))
	assert.Equal(t, maxExchangeHold, parseHoldOverride("99s", fallback))
}

func TestRun_LifecycleAndFlagErrors(t *testing.T) {
	t.Run("starts on ephemeral port, invokes browser hook, and shuts down cleanly on context cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		openedCh := make(chan string, 1)
		var out bytes.Buffer
		errCh := make(chan error, 1)
		go func() {
			errCh <- run(ctx, []string{"-port", "0", "-hold", "0s", "-open=true"}, &out, func(_ context.Context, targetURL string) error {
				openedCh <- targetURL
				return nil
			})
		}()

		galleryURL := <-openedCh
		require.True(t, strings.HasPrefix(galleryURL, "http://localhost:"))

		resp, err := http.Get(galleryURL + "/") //nolint:noctx
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		cancel()
		require.NoError(t, <-errCh)
		assert.Contains(t, out.String(), "OAuth Callback Preview Gallery running at:")
	})

	t.Run("invalid flag returns error", func(t *testing.T) {
		var out bytes.Buffer
		err := run(context.Background(), []string{"-definitely-unknown-flag"}, &out, nil)
		require.Error(t, err)
	})

	t.Run("drains in-flight /run handler past shutdown timeout without launching post-cancel flow", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		enteredRenderer := make(chan struct{})
		releaseRenderer := make(chan struct{})
		var handlerFinished atomic.Bool

		slowScenario := scenarioSpec{
			ID:         "slow-render",
			Badge:      "TEST",
			BadgeClass: "ok",
			Title:      "Slow Renderer",
			BinaryName: "scafctl",
			Succeed:    true,
			Renderer: func(_ context.Context, d authpage.Data) ([]byte, error) {
				if d.State == authpage.StateSuccess {
					close(enteredRenderer)
					<-releaseRenderer
					handlerFinished.Store(true)
				}
				return []byte("<!doctype html><html><body>ok</body></html>"), nil
			},
		}

		openedCh := make(chan string, 1)
		var out bytes.Buffer
		runDone := make(chan error, 1)
		go func() {
			runDone <- runWithConfig(
				ctx,
				[]string{"-port", "0", "-hold", "0s", "-open=true"},
				&out,
				func(_ context.Context, targetURL string) error {
					openedCh <- targetURL
					return nil
				},
				runConfig{
					scenarios:       []scenarioSpec{slowScenario},
					shutdownTimeout: 15 * time.Millisecond,
				},
			)
		}()

		galleryURL := <-openedCh
		noRedirectClient := &http.Client{
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}

		reqStatusCh := make(chan int, 1)
		go func() {
			resp, reqErr := noRedirectClient.Get(galleryURL + "/run?scenario=slow-render") //nolint:noctx
			if reqErr == nil {
				_ = resp.Body.Close()
				reqStatusCh <- resp.StatusCode
			} else {
				close(reqStatusCh)
			}
		}()

		<-enteredRenderer
		// Cancel ctx and wait longer than shutdownTimeout (15ms) while the handler is
		// still blocked inside Renderer before flowGate.enter().
		cancel()
		select {
		case err := <-runDone:
			t.Fatalf("runWithConfig returned before in-flight /run handler drained: %v", err)
		case <-time.After(50 * time.Millisecond):
		}

		close(releaseRenderer)
		if code, ok := <-reqStatusCh; ok {
			assert.Equal(t, http.StatusServiceUnavailable, code)
		}

		require.NoError(t, <-runDone)
		assert.True(t, handlerFinished.Load())
		assert.NotContains(t, out.String(), "[slow-render] started oauth-helpers CallbackServer")
	})
}
