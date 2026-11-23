// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package authpage

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultRenderer_BlueprintDesignInvariants(t *testing.T) {
	tests := []struct {
		name          string
		data          Data
		wantContains  []string
		wantExcludes  []string
		wantLogoImg   bool
		wantBodyClass string
	}{
		{
			name: "scafctl success renders Blueprint with embedded logo and favicon",
			data: Data{BinaryName: settings.CliBinaryName, State: StateSuccess},
			wantContains: []string{
				"<title>scafctl | Authentication complete</title>",
				"CONFIGURATION / CONNECTED",
				"Authentication complete",
				"You&#39;re<br>connected.",
				"Return to your terminal to continue.",
				"You can close this window.",
				"DECLARATIVE BY DESIGN.",
				"READY TO CONTINUE / 01",
				"M205 100L208 103L213 97",
				`<svg class="logo"`,
				"data:image/png;base64,",
				"prefers-reduced-motion:reduce",
			},
			wantExcludes: []string{
				"preview-controls",
				"DESIGN PREVIEW ONLY",
				"<script",
				"http://",
				"https://",
				"OFL",
				"Inter",
				`class="failure"`,
			},
			wantLogoImg: true,
		},
		{
			name: "empty binary name defaults to scafctl",
			data: Data{BinaryName: "", State: StateSuccess},
			wantContains: []string{
				"<title>scafctl | Authentication complete</title>",
				`<svg class="logo"`,
			},
			wantLogoImg: true,
		},
		{
			name: "scafctl failure renders amber failure state without leaking details",
			data: Data{BinaryName: settings.CliBinaryName, State: StateFailure},
			wantContains: []string{
				"<title>scafctl | Authentication unsuccessful</title>",
				`<body class="failure">`,
				"CONFIGURATION / CONNECT",
				"Authentication unsuccessful",
				"Couldn&#39;t<br>connect.",
				"Return to your terminal for details and try signing in again.",
				"RETURN TO TERMINAL / 01",
				"M206 97L212 103M212 97L206 103",
			},
			wantExcludes: []string{
				"preview-controls",
				"<script",
				"http://",
				"https://",
				"READY TO CONTINUE / 01",
			},
			wantLogoImg: true,
		},
		{
			name: "alternate binary name renders plain-text wordmark without scafctl logo",
			data: Data{BinaryName: "mycli", State: StateSuccess},
			wantContains: []string{
				"<title>mycli | Authentication complete</title>",
				`<span class="brand-wordmark">mycli</span>`,
				"You&#39;re<br>connected.",
			},
			wantExcludes: []string{
				"scafctl",
				"data:image/svg+xml;base64,",
				"data:image/png;base64,",
				"<script",
			},
			wantLogoImg: false,
		},
		{
			name: "binary name with HTML metacharacters is escaped",
			data: Data{BinaryName: `<script>alert(1)</script>`, State: StateFailure},
			wantContains: []string{
				"&lt;script&gt;alert(1)&lt;/script&gt;",
			},
			wantExcludes: []string{
				"<script>alert(1)</script>",
			},
			wantLogoImg: false,
		},
		{
			name: "unknown state fails closed to failure page",
			data: Data{BinaryName: "mycli", State: State("unexpected")},
			wantContains: []string{
				`<body class="failure">`,
				"Authentication unsuccessful",
			},
			wantExcludes: []string{
				"You&#39;re<br>connected.",
			},
			wantLogoImg: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := DefaultRenderer(context.Background(), tt.data)
			require.NoError(t, err)
			htmlStr := string(doc)
			assert.LessOrEqual(t, len(doc), MaxDocumentBytes)
			for _, sub := range tt.wantContains {
				assert.Contains(t, htmlStr, sub)
			}
			for _, sub := range tt.wantExcludes {
				assert.NotContains(t, htmlStr, sub)
			}
			if tt.wantLogoImg {
				assert.Contains(t, htmlStr, `<svg class="logo"`)
			} else {
				assert.NotContains(t, htmlStr, `<svg class="logo"`)
			}
		})
	}
}

func TestRenderSafe_CustomAndFallbackPaths(t *testing.T) {
	oversized := bytes.Repeat([]byte("x"), MaxDocumentBytes+1)

	tests := []struct {
		name       string
		renderer   Renderer
		data       Data
		wantSubstr string
		wantNoSub  string
	}{
		{
			name: "whole-document custom renderer overrides default completely",
			renderer: func(_ context.Context, d Data) ([]byte, error) {
				return []byte("<!doctype html><html><body>CUSTOM:" + d.BinaryName + ":" + string(d.State) + "</body></html>"), nil
			},
			data:       Data{BinaryName: "acme-cli", State: StateSuccess},
			wantSubstr: "CUSTOM:acme-cli:success",
			wantNoSub:  "DECLARATIVE BY DESIGN",
		},
		{
			name: "renderer error on success preserves success semantics via fallback",
			renderer: func(_ context.Context, _ Data) ([]byte, error) {
				return nil, errors.New("boom")
			},
			data:       Data{BinaryName: "acme-cli", State: StateSuccess},
			wantSubstr: "You're connected.",
			wantNoSub:  "Couldn't connect.",
		},
		{
			name: "renderer error on failure preserves failure semantics via fallback",
			renderer: func(_ context.Context, _ Data) ([]byte, error) {
				return nil, errors.New("boom")
			},
			data:       Data{BinaryName: "acme-cli", State: StateFailure},
			wantSubstr: "Couldn't connect.",
			wantNoSub:  "You're connected.",
		},
		{
			name: "whitespace-only output falls back safely",
			renderer: func(_ context.Context, _ Data) ([]byte, error) {
				return []byte("   \n\t "), nil
			},
			data:       Data{BinaryName: "acme-cli", State: StateFailure},
			wantSubstr: "Couldn't connect.",
		},
		{
			name: "oversized document falls back safely within MaxDocumentBytes",
			renderer: func(_ context.Context, _ Data) ([]byte, error) {
				return oversized, nil
			},
			data:       Data{BinaryName: "acme-cli", State: StateSuccess},
			wantSubstr: "You're connected.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := RenderSafe(context.Background(), tt.renderer, tt.data)
			require.NotEmpty(t, out)
			assert.LessOrEqual(t, len(out), MaxDocumentBytes)
			assert.Contains(t, string(out), tt.wantSubstr)
			if tt.wantNoSub != "" {
				assert.NotContains(t, string(out), tt.wantNoSub)
			}
		})
	}
}

func TestRenderBundle_ProducesDistinctSuccessAndFailurePages(t *testing.T) {
	bundle := RenderBundle(context.Background(), nil, "mycli")
	require.NotEmpty(t, bundle.SuccessHTML)
	require.NotEmpty(t, bundle.FailureHTML)
	assert.NotEqual(t, bundle.SuccessHTML, bundle.FailureHTML)
	assert.True(t, strings.Contains(string(bundle.SuccessHTML), "Authentication complete"))
	assert.True(t, strings.Contains(string(bundle.FailureHTML), "Authentication unsuccessful"))
}

func TestDefaultRenderer_ConcurrentRaceSafety(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			bin := settings.CliBinaryName
			if idx%2 == 1 {
				bin = "mycli"
			}
			b := RenderBundle(context.Background(), nil, bin)
			assert.NotEmpty(t, b.SuccessHTML)
			assert.NotEmpty(t, b.FailureHTML)
		}(i)
	}
	wg.Wait()
}
