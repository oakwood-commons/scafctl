// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package authpage renders self-contained HTML completion pages for interactive
// localhost OAuth authentication flows and exposes whole-document customization
// for CLI embedders.
package authpage

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"fmt"
	"html"
	"html/template"
	"strings"
	"sync"

	sdkplugin "github.com/oakwood-commons/scafctl-plugin-sdk/plugin"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// MaxDocumentBytes is the maximum size in bytes permitted for a rendered HTML
// authentication page before RenderSafe falls back to a minimal safe document.
// It matches the plugin SDK configure boundary limit so pages accepted here
// always survive host-to-plugin transport intact.
const MaxDocumentBytes = sdkplugin.MaxAuthCallbackPageBytes

// State identifies the settled outcome of an interactive login flow.
type State string

const (
	// StateSuccess indicates that all fatal login steps (token exchange,
	// credential storage, and claims extraction) succeeded.
	StateSuccess State = "success"
	// StateFailure indicates that authentication could not be completed.
	StateFailure State = "failure"
)

// Data contains the strictly non-sensitive presentation inputs passed to an
// authentication page Renderer. Tokens, authorization codes, OAuth state,
// redirect URIs, identity claims, and raw provider errors are never included.
type Data struct {
	// BinaryName is the configured CLI binary name (defaults to settings.CliBinaryName).
	BinaryName string `json:"binaryName" yaml:"binaryName" doc:"CLI binary name displayed on the page" maxLength:"64" example:"scafctl"`
	// State is the final authentication outcome (StateSuccess or StateFailure).
	State State `json:"state" yaml:"state" doc:"Final authentication outcome" maxLength:"16" example:"success"`
}

// Renderer produces a complete UTF-8 HTML document for the given authentication
// outcome. Implementations must return a self-contained document with no
// external network dependencies or scripts (documents are served under a strict
// script-free Content-Security-Policy).
type Renderer func(ctx context.Context, data Data) ([]byte, error)

// Bundle holds pre-rendered HTML documents for both authentication outcomes.
type Bundle struct {
	SuccessHTML []byte
	FailureHTML []byte
}

//go:embed assets/logo.svg
var embeddedLogoSVG []byte

//go:embed assets/favicon.png
var embeddedFaviconPNG []byte

type templateView struct {
	BinaryName   string
	UseLogo      bool
	InlineLogo   template.HTML
	FaviconURI   template.URL
	IsFailure    bool
	PageTitle    string
	Edition      string
	StatusText   string
	HeadingLine1 string
	HeadingLine2 string
	Description  string
	Guidance     string
	StatusMarkD  string
	FooterState  string
}

type cachedAssets struct {
	tmpl       *template.Template
	err        error
	inlineLogo template.HTML
	faviconURI template.URL
}

func loadAssets() cachedAssets {
	return getAssets()
}

func prepareInlineLogo(rawSVG []byte) template.HTML {
	s := strings.TrimSpace(string(rawSVG))
	if idx := strings.Index(s, "<svg"); idx >= 0 {
		s = s[idx:]
	}
	s = strings.Replace(s, `xmlns="http://www.w3.org/2000/svg" `, "", 1)
	s = strings.Replace(s, `<svg `, `<svg class="logo" `, 1)
	return template.HTML(s) //nolint:gosec // compile-time embedded brand SVG with no user input
}

var getAssets = sync.OnceValue(func() cachedAssets {
	tmpl, err := template.New("blueprint").Parse(blueprintTemplateHTML)
	return cachedAssets{
		tmpl:       tmpl,
		err:        err,
		inlineLogo: prepareInlineLogo(embeddedLogoSVG),
		faviconURI: template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(embeddedFaviconPNG)), //nolint:gosec // compile-time embedded favicon PNG
	}
})

func normalizeData(d Data) Data {
	name := strings.TrimSpace(d.BinaryName)
	if name == "" {
		name = settings.CliBinaryName
	}
	state := d.State
	if state != StateSuccess && state != StateFailure {
		// Fail closed: an unrecognized state must never claim authentication succeeded.
		state = StateFailure
	}
	return Data{
		BinaryName: name,
		State:      state,
	}
}

// DefaultRenderer renders the approved Blueprint authentication page. When
// Data.BinaryName matches settings.CliBinaryName, the embedded horizontal
// Configuration Flow logo and navy favicon are used; for alternate embedder
// binary names, a clean plain-text wordmark is rendered instead.
func DefaultRenderer(_ context.Context, data Data) ([]byte, error) {
	norm := normalizeData(data)
	assets := loadAssets()
	if assets.err != nil {
		return nil, fmt.Errorf("parse blueprint auth template: %w", assets.err)
	}

	isDefaultBrand := norm.BinaryName == settings.CliBinaryName
	view := templateView{
		BinaryName:   norm.BinaryName,
		UseLogo:      isDefaultBrand,
		InlineLogo:   assets.inlineLogo,
		FaviconURI:   assets.faviconURI,
		IsFailure:    norm.State == StateFailure,
		PageTitle:    norm.BinaryName + " | Authentication complete",
		Edition:      "CONFIGURATION / CONNECTED",
		StatusText:   "Authentication complete",
		HeadingLine1: "You're",
		HeadingLine2: "connected.",
		Description:  "Return to your terminal to continue.",
		Guidance:     "You can close this window.",
		StatusMarkD:  "M205 100L208 103L213 97",
		FooterState:  "READY TO CONTINUE / 01",
	}

	if norm.State == StateFailure {
		view.PageTitle = norm.BinaryName + " | Authentication unsuccessful"
		view.Edition = "CONFIGURATION / CONNECT"
		view.StatusText = "Authentication unsuccessful"
		view.HeadingLine1 = "Couldn't"
		view.HeadingLine2 = "connect."
		view.Description = "Return to your terminal for details and try signing in again."
		view.StatusMarkD = "M206 97L212 103M212 97L206 103"
		view.FooterState = "RETURN TO TERMINAL / 01"
	}

	var buf bytes.Buffer
	buf.Grow(16 * 1024)
	if err := assets.tmpl.Execute(&buf, view); err != nil {
		return nil, fmt.Errorf("execute blueprint auth template: %w", err)
	}
	return buf.Bytes(), nil
}

// FallbackHTML returns a minimal, dependency-free, HTML-escaped document that
// preserves the true authentication outcome when a custom renderer errors,
// returns empty output, or exceeds MaxDocumentBytes.
func FallbackHTML(data Data) []byte {
	norm := normalizeData(data)
	escapedBrand := html.EscapeString(norm.BinaryName)
	if norm.State == StateSuccess {
		return []byte(fmt.Sprintf(
			"<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><title>%s | Authentication complete</title></head><body><h1>You're connected.</h1><p>Return to your terminal to continue. You can close this window.</p></body></html>",
			escapedBrand,
		))
	}
	return []byte(fmt.Sprintf(
		"<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><title>%s | Authentication unsuccessful</title></head><body><h1>Couldn't connect.</h1><p>Return to your terminal for details and try signing in again. You can close this window.</p></body></html>",
		escapedBrand,
	))
}

// RenderSafe invokes r (or DefaultRenderer when r is nil) and guarantees a
// non-empty, size-bounded HTML document matching data.State. Presentation
// errors never invert success/failure semantics.
func RenderSafe(ctx context.Context, r Renderer, data Data) []byte {
	norm := normalizeData(data)
	if r == nil {
		r = DefaultRenderer
	}
	doc, err := r(ctx, norm)
	if err != nil || len(bytes.TrimSpace(doc)) == 0 || len(doc) > MaxDocumentBytes {
		return FallbackHTML(norm)
	}
	return bytes.Clone(doc)
}

// RenderBundle pre-renders both success and failure HTML documents for the
// given binary name and optional custom Renderer.
func RenderBundle(ctx context.Context, r Renderer, binaryName string) Bundle {
	return Bundle{
		SuccessHTML: RenderSafe(ctx, r, Data{BinaryName: binaryName, State: StateSuccess}),
		FailureHTML: RenderSafe(ctx, r, Data{BinaryName: binaryName, State: StateFailure}),
	}
}
