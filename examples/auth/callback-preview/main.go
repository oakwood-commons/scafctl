// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Command callback-preview launches a local 8-scenario browser gallery for
// previewing OAuth callback completion pages with simulated post-callback
// token exchange delay.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"html"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	oauth "github.com/oakwood-commons/oauth-helpers"
	"github.com/oakwood-commons/scafctl/pkg/authpage"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

const (
	defaultPreviewPort     = 8787
	defaultExchangeHold    = 1500 * time.Millisecond
	maxExchangeHold        = 15 * time.Second
	callbackFlowTimeout    = 30 * time.Second
	callbackDrainTimeout   = 5 * time.Second
	previewReadHdrTimeout  = 10 * time.Second
	previewShutdownTimeout = 3 * time.Second
	embedderBinaryName     = "acme-cli"
)

type scenarioSpec struct {
	ID                string
	Badge             string
	BadgeClass        string
	Title             string
	Description       string
	BinaryName        string
	Renderer          authpage.Renderer
	UseHelperFallback bool
	Succeed           bool
}

func customEmbedderRenderer(_ context.Context, d authpage.Data) ([]byte, error) {
	brand := html.EscapeString(d.BinaryName)
	accent := "#10b981"
	heading := "Signed in to " + brand
	detail := "Custom whole-document embedder page rendered via RootOptions.AuthPageRenderer."
	if d.State == authpage.StateFailure {
		accent = "#f59e0b"
		heading = "Could not sign in to " + brand
		detail = "Post-callback step failed safely (HTTP 503). Return to your terminal for diagnostics."
	}
	doc := fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>%s - Custom Embedder Auth</title>
  <style>
    :root { color-scheme: dark; }
    * { box-sizing: border-box; }
    body { margin: 0; min-height: 100vh; display: grid; place-items: center; padding: 1.5rem; background: #0f172a; color: #f8fafc; font-family: system-ui, -apple-system, sans-serif; }
    .card { max-width: 30rem; width: 100%%; padding: 2.5rem; border-radius: 1rem; background: #1e293b; border-top: 4px solid %s; box-shadow: 0 20px 40px rgba(0,0,0,.45); }
    .pill { display: inline-block; font-size: .75rem; font-weight: 700; letter-spacing: .08em; text-transform: uppercase; color: %s; margin-bottom: .75rem; }
    h1 { margin: 0 0 .75rem; font-size: 1.6rem; }
    p { margin: 0; color: #cbd5e1; line-height: 1.6; }
  </style>
</head>
<body>
  <main class="card">
    <div class="pill">Embedder Override (%s)</div>
    <h1>%s</h1>
    <p>%s</p>
  </main>
</body>
</html>`, brand, accent, accent, html.EscapeString(string(d.State)), heading, detail)
	return []byte(doc), nil
}

func previewScenarios() []scenarioSpec {
	return []scenarioSpec{
		{
			ID:          "scafctl-success",
			Badge:       "200 OK / Default scafctl",
			BadgeClass:  "ok",
			Title:       "1. scafctl Blueprint (Success)",
			Description: "Holds during simulated token exchange and keyring save, then settles to HTTP 200 with the Blueprint page, embedded SVG lockup, and favicon.",
			BinaryName:  settings.CliBinaryName,
			Succeed:     true,
		},
		{
			ID:          "scafctl-failure",
			Badge:       "503 Service Unavailable / Default scafctl",
			BadgeClass:  "err",
			Title:       "2. scafctl Blueprint (Failure)",
			Description: "Simulates a post-callback token exchange failure: raw error stays in terminal logs while the browser receives the amber Blueprint failure page with HTTP 503.",
			BinaryName:  settings.CliBinaryName,
			Succeed:     false,
		},
		{
			ID:          "acme-success",
			Badge:       "200 OK / BinaryName: acme-cli",
			BadgeClass:  "alt",
			Title:       "3. Alternate Binary Brand (Success)",
			Description: "Embedder sets RootOptions.BinaryName to \"acme-cli\" with nil AuthPageRenderer: renders Blueprint with a plain-text wordmark and zero scafctl artwork.",
			BinaryName:  embedderBinaryName,
			Succeed:     true,
		},
		{
			ID:          "acme-failure",
			Badge:       "503 Service Unavailable / BinaryName: acme-cli",
			BadgeClass:  "err",
			Title:       "4. Alternate Binary Brand (Failure)",
			Description: "Embedder sets RootOptions.BinaryName to \"acme-cli\" and post-callback exchange fails: renders the amber Blueprint failure state with plain-text wordmark.",
			BinaryName:  embedderBinaryName,
			Succeed:     false,
		},
		{
			ID:          "custom-success",
			Badge:       "200 OK / Custom AuthPageRenderer",
			BadgeClass:  "ok",
			Title:       "5. Custom Embedder Renderer (Success)",
			Description: "Replaces the entire HTML document via RootOptions.AuthPageRenderer for full embedder brand control under the strict callback CSP.",
			BinaryName:  embedderBinaryName,
			Renderer:    customEmbedderRenderer,
			Succeed:     true,
		},
		{
			ID:          "custom-failure",
			Badge:       "503 Service Unavailable / Custom AuthPageRenderer",
			BadgeClass:  "err",
			Title:       "6. Custom Embedder Renderer (Failure)",
			Description: "Serves the embedder's custom whole-document failure page while preserving HTTP 503 Service Unavailable semantics.",
			BinaryName:  embedderBinaryName,
			Renderer:    customEmbedderRenderer,
			Succeed:     false,
		},
		{
			ID:                "helper-fallback-success",
			Badge:             "200 OK / oauth-helpers #05152a Fallback",
			BadgeClass:        "alt",
			Title:             "7. oauth-helpers Dark Fallback (Success)",
			Description:       "Starts StartCallbackServer with WithDeferredCompletion() and no CallbackPages bundle, serving oauth-helpers' built-in #05152a dark card.",
			UseHelperFallback: true,
			Succeed:           true,
		},
		{
			ID:                "helper-fallback-failure",
			Badge:             "503 Service Unavailable / oauth-helpers #05152a Fallback",
			BadgeClass:        "err",
			Title:             "8. oauth-helpers Dark Fallback (Failure)",
			Description:       "Starts StartCallbackServer with WithDeferredCompletion() and no CallbackPages bundle, settling to oauth-helpers' built-in #05152a failure card.",
			UseHelperFallback: true,
			Succeed:           false,
		},
	}
}

type holdPreset struct {
	Label  string
	Value  string
	Active bool
}

type galleryCardView struct {
	RunHref     string
	Badge       string
	BadgeClass  string
	Title       string
	Description string
}

type galleryView struct {
	HoldDisplay string
	Presets     []holdPreset
	Cards       []galleryCardView
}

var galleryTmpl = template.Must(template.New("gallery").Parse(launcherHTML))

func parseHoldOverride(raw string, fallback time.Duration) time.Duration {
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		return fallback
	}
	if d > maxExchangeHold {
		return maxExchangeHold
	}
	return d
}

type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (sw *syncWriter) Write(p []byte) (int, error) {
	if sw == nil || sw.w == nil {
		return len(p), nil
	}
	sw.mu.Lock()
	defer sw.mu.Unlock()
	return sw.w.Write(p)
}

func newPreviewMux(parentCtx context.Context, defaultHold time.Duration, logOut io.Writer, wg *sync.WaitGroup) http.Handler {
	safeOut := &syncWriter{w: logOut}
	scenarios := previewScenarios()
	byID := make(map[string]scenarioSpec, len(scenarios))
	for _, s := range scenarios {
		byID[s.ID] = s
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		hold := parseHoldOverride(r.URL.Query().Get("hold"), defaultHold)
		holdParam := hold.String()

		presets := []holdPreset{
			{Label: "Instant (0s)", Value: "0s", Active: hold == 0},
			{Label: "1.5s Exchange Hold", Value: "1500ms", Active: hold == 1500*time.Millisecond},
			{Label: "4s Slow Exchange", Value: "4s", Active: hold == 4*time.Second},
		}
		cards := make([]galleryCardView, 0, len(scenarios))
		for _, s := range scenarios {
			q := url.Values{}
			q.Set("scenario", s.ID)
			q.Set("hold", holdParam)
			cards = append(cards, galleryCardView{
				RunHref:     "/run?" + q.Encode(),
				Badge:       s.Badge,
				BadgeClass:  s.BadgeClass,
				Title:       s.Title,
				Description: s.Description,
			})
		}

		var buf bytes.Buffer
		if err := galleryTmpl.Execute(&buf, galleryView{
			HoldDisplay: hold.String(),
			Presets:     presets,
			Cards:       cards,
		}); err != nil {
			http.Error(w, "failed to render gallery", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(buf.Bytes())
	})

	mux.HandleFunc("/run", func(w http.ResponseWriter, r *http.Request) {
		spec, ok := byID[r.URL.Query().Get("scenario")]
		if !ok {
			http.Error(w, "unknown scenario", http.StatusBadRequest)
			return
		}
		hold := parseHoldOverride(r.URL.Query().Get("hold"), defaultHold)

		state, err := oauth.GenerateCodeVerifier()
		if err != nil {
			http.Error(w, "failed to generate state", http.StatusInternalServerError)
			return
		}

		opts := []oauth.CallbackOption{oauth.WithDeferredCompletion()}
		if !spec.UseHelperFallback {
			bundle := authpage.RenderBundle(r.Context(), spec.Renderer, spec.BinaryName)
			opts = append(opts, oauth.WithCallbackPages(oauth.CallbackPages{
				SuccessHTML: bundle.SuccessHTML,
				FailureHTML: bundle.FailureHTML,
			}))
		}

		flowCtx, flowCancel := context.WithTimeout(parentCtx, callbackFlowTimeout)
		cs, err := oauth.StartCallbackServer(flowCtx, 0, state, opts...)
		if err != nil {
			flowCancel()
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		_, _ = fmt.Fprintf(safeOut, "[%s] started oauth-helpers CallbackServer at %s\n", spec.ID, cs.RedirectURI)

		if wg != nil {
			wg.Add(1)
		}
		go func() {
			if wg != nil {
				defer wg.Done()
			}
			settlePreviewFlow(flowCtx, flowCancel, cs, spec, hold, safeOut)
		}()

		target := fmt.Sprintf("%s/?code=PREVIEW-AUTH-CODE&state=%s", cs.RedirectURI, url.QueryEscape(state))
		http.Redirect(w, r, target, http.StatusFound)
	})

	return mux
}

func settlePreviewFlow(
	flowCtx context.Context,
	flowCancel context.CancelFunc,
	cs *oauth.CallbackServer,
	spec scenarioSpec,
	hold time.Duration,
	logOut io.Writer,
) {
	defer flowCancel()

	select {
	case <-flowCtx.Done():
		_ = cs.Close()
		return
	case res := <-cs.ResultChan():
		if res.Err != nil {
			_, _ = fmt.Fprintf(logOut, "[%s] callback error: %v\n", spec.ID, res.Err)
			drainCallbackServer(cs)
			return
		}
	}

	if hold > 0 {
		_, _ = fmt.Fprintf(logOut, "[%s] callback received; holding browser response for %s (simulating token exchange + keyring write)...\n", spec.ID, hold)
		timer := time.NewTimer(hold)
		select {
		case <-flowCtx.Done():
			timer.Stop()
			_ = cs.Close()
			return
		case <-timer.C:
		}
	}

	if spec.Succeed {
		_, _ = fmt.Fprintf(logOut, "[%s] post-callback steps succeeded -> Complete(true) (HTTP 200)\n", spec.ID)
	} else {
		_, _ = fmt.Fprintf(logOut, "[%s] post-callback step failed -> Complete(false) (HTTP 503)\n", spec.ID)
	}
	cs.Complete(spec.Succeed)
	drainCallbackServer(cs)
}

func drainCallbackServer(cs *oauth.CallbackServer) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), callbackDrainTimeout)
	defer cancel()
	_ = cs.Shutdown(shutdownCtx)
}

func run(ctx context.Context, args []string, out io.Writer, openFn func(context.Context, string) error) error {
	safeOut := &syncWriter{w: out}
	fs := flag.NewFlagSet("callback-preview", flag.ContinueOnError)
	fs.SetOutput(safeOut)
	port := fs.Int("port", defaultPreviewPort, "Loopback port for the local preview gallery (0 chooses an ephemeral port)")
	hold := fs.Duration("hold", defaultExchangeHold, "Simulated post-callback token exchange hold duration")
	openBrowser := fs.Bool("open", true, "Automatically open the preview gallery in the default browser")
	if err := fs.Parse(args); err != nil {
		return err
	}

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		return fmt.Errorf("listen on 127.0.0.1:%d: %w", *port, err)
	}

	tcpAddr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		_ = ln.Close()
		return fmt.Errorf("unexpected listener address type: %T", ln.Addr())
	}
	demoURL := fmt.Sprintf("http://localhost:%d", tcpAddr.Port)

	flowRootCtx, cancelFlows := context.WithCancel(ctx)
	var flowWG sync.WaitGroup
	defer func() {
		cancelFlows()
		flowWG.Wait()
	}()

	srv := &http.Server{
		Handler:           newPreviewMux(flowRootCtx, *hold, safeOut, &flowWG),
		ReadHeaderTimeout: previewReadHdrTimeout,
	}

	shutdownBaseCtx := context.WithoutCancel(ctx)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(shutdownBaseCtx, previewShutdownTimeout)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	_, _ = fmt.Fprintf(safeOut, "OAuth Callback Preview Gallery running at: %s\n", demoURL)
	_, _ = fmt.Fprintf(safeOut, "Default post-callback hold duration:       %s\n", hold.String())
	_, _ = fmt.Fprintln(safeOut, "Press Ctrl+C to stop.")

	if *openBrowser && openFn != nil {
		_ = openFn(ctx, demoURL)
	}

	if serveErr := srv.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return fmt.Errorf("serve preview gallery: %w", serveErr)
	}
	return nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Stdout, oauth.OpenBrowser)
	stop()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "callback-preview: %v\n", err)
		os.Exit(1)
	}
}

const launcherHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>scafctl OAuth Callback Page Preview Gallery</title>
  <style>
    :root { color-scheme: dark; }
    * { box-sizing: border-box; }
    body { margin: 0; padding: 3rem 1.5rem; font-family: system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; background: #05152a; color: #eff7ff; }
    main { max-width: 60rem; margin: 0 auto; }
    .eyebrow { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; font-size: .75rem; letter-spacing: .14em; text-transform: uppercase; color: #a4f6c5; margin-bottom: .6rem; }
    h1 { margin: 0 0 .65rem; font-size: 1.9rem; letter-spacing: -.02em; }
    p.lead { margin: 0 0 1.5rem; color: #9bafc4; line-height: 1.65; max-width: 50rem; }
    .toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: .6rem; margin-bottom: 2rem; padding: .85rem 1rem; border-radius: .75rem; background: #071e3d; border: 1px solid rgba(120, 148, 175, .28); }
    .toolbar-label { font-size: .85rem; color: #cbdae8; margin-right: .35rem; }
    a.pill { font-size: .8rem; font-weight: 600; padding: .35rem .75rem; border-radius: 999px; text-decoration: none; color: #cbdae8; background: rgba(155, 175, 196, .12); border: 1px solid transparent; }
    a.pill:hover { border-color: #00e9f2; color: #fff; }
    a.pill.active { background: rgba(164, 246, 197, .16); border-color: rgba(164, 246, 197, .55); color: #a4f6c5; }
    .grid { display: grid; gap: 1rem; grid-template-columns: repeat(auto-fit, minmax(24rem, 1fr)); }
    a.card { display: block; padding: 1.35rem 1.5rem; border-radius: .85rem; background: #071e3d; border: 1px solid rgba(120, 148, 175, .28); text-decoration: none; color: inherit; transition: border-color .15s, transform .15s; }
    a.card:hover { border-color: #00e9f2; transform: translateY(-1px); }
    .tag { display: inline-block; font-family: ui-monospace, SFMono-Regular, Consolas, monospace; font-size: .72rem; font-weight: 600; letter-spacing: .04em; padding: .22rem .55rem; border-radius: .35rem; margin-bottom: .7rem; }
    .ok { background: rgba(164, 246, 197, .14); color: #a4f6c5; }
    .err { background: rgba(255, 194, 142, .14); color: #ffc28e; }
    .alt { background: rgba(0, 233, 242, .12); color: #7cebf2; }
    h2 { margin: 0 0 .45rem; font-size: 1.08rem; color: #fff; }
    p.desc { margin: 0; font-size: .9rem; color: #9bafc4; line-height: 1.55; }
    code { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; background: rgba(155, 175, 196, .14); padding: .12rem .35rem; border-radius: .25rem; font-size: .85em; color: #eff7ff; }
  </style>
</head>
<body>
  <main>
    <div class="eyebrow">Interactive Loopback Harness</div>
    <h1>OAuth Callback Page Preview Gallery</h1>
    <p class="lead">
      Every card below starts a real ephemeral <code>oauth.StartCallbackServer</code> with
      <code>WithDeferredCompletion()</code>, redirects a new browser tab to it, holds the HTTP response
      open for <strong>{{.HoldDisplay}}</strong> while simulating post-callback token exchange and keyring
      storage, and then settles the final HTML document with production CSP headers and status codes.
    </p>
    <div class="toolbar">
      <span class="toolbar-label">Simulated post-callback delay:</span>
      {{range .Presets}}<a class="pill{{if .Active}} active{{end}}" href="/?hold={{.Value}}">{{.Label}}</a>{{end}}
    </div>
    <div class="grid">
      {{range .Cards}}
      <a class="card" href="{{.RunHref}}" target="_blank" rel="noopener">
        <span class="tag {{.BadgeClass}}">{{.Badge}}</span>
        <h2>{{.Title}}</h2>
        <p class="desc">{{.Description}}</p>
      </a>
      {{end}}
    </div>
  </main>
</body>
</html>
`
