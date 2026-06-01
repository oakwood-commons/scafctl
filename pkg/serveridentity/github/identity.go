// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"context"
	"crypto/rsa"
	"fmt"
	"sync"
	"time"

	manager "github.com/oakwood-commons/go-flight/cache"
	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/serveridentity"
	"github.com/oakwood-commons/scafctl/pkg/tokenprovider"
	"github.com/oakwood-commons/scafctl/pkg/tokenprovider/callerscope"
	"go.opentelemetry.io/otel/attribute"
)

var _ tokenprovider.TokenProvider = (*App)(nil)

var (
	defaultHTTPClientOnce sync.Once
	defaultHTTPClientVal  *httpc.Client
)

// App implements tokenprovider.TokenProvider using App App installation tokens.
type App struct {
	ClientID       string
	InstallationID int64
	PrivateKey     *rsa.PrivateKey
	Hostname       string
	tokenURL       string
	manager        *manager.Manager[string, tokenprovider.Token]
	httpClient     *httpc.Client
	strategies     map[callerscope.CallerScope]func(ctx context.Context, opts tokenprovider.RequestOptions) (tokenprovider.Token, error)
}

// Option configures a GitHub identity.
type Option func(*App)

// WithManager overrides the cache manager.
func WithManager(mgr *manager.Manager[string, tokenprovider.Token]) Option {
	return func(g *App) { g.manager = mgr }
}

// WithHTTPClient overrides the default HTTP client.
func WithHTTPClient(client *httpc.Client) Option {
	return func(g *App) { g.httpClient = client }
}

// WithTokenURL overrides the computed token endpoint URL (for testing).
func WithTokenURL(url string) Option {
	return func(g *App) { g.tokenURL = url }
}

// NewGitHubAppIdentity constructs a GitHub identity from resolved values.
func NewGitHubAppIdentity(clientID string, installationID int64, key *rsa.PrivateKey, hostname string, opts ...Option) (*App, error) {
	if clientID == "" {
		return nil, fmt.Errorf("clientID is required")
	}
	if installationID == 0 {
		return nil, fmt.Errorf("installationID is required")
	}
	if key == nil {
		return nil, fmt.Errorf("private key is required")
	}
	if hostname == "" {
		hostname = "github.com"
	}

	g := &App{
		ClientID:       clientID,
		InstallationID: installationID,
		PrivateKey:     key,
		Hostname:       hostname,
		tokenURL:       fmt.Sprintf("%s/app/installations/%d/access_tokens", apiBaseURL(hostname), installationID),
		httpClient:     defaultHTTPClient(),
		strategies:     make(map[callerscope.CallerScope]func(ctx context.Context, opts tokenprovider.RequestOptions) (tokenprovider.Token, error)),
	}

	for _, opt := range opts {
		opt(g)
	}

	g.strategies[callerscope.ServerCaller] = g.serverToken

	return g, nil
}

func (g *App) Name() string {
	return "github"
}

func (g *App) GetToken(ctx context.Context, opts tokenprovider.RequestOptions) (tokenprovider.Token, error) {
	obs := serveridentity.NewInstrumentation(ctx)
	if obs.Verbose {
		obs.LogDebug("github token request", "caller", opts.Caller)
	}
	obs.AddEvent("github.GetToken", attribute.String("caller", string(opts.Caller)))

	strategy, ok := g.strategies[opts.Caller]
	if !ok {
		return tokenprovider.Token{}, fmt.Errorf("unsupported caller: %s", opts.Caller)
	}
	return strategy(ctx, opts)
}

func (g *App) serverToken(ctx context.Context, _ tokenprovider.RequestOptions) (tokenprovider.Token, error) {
	obs := serveridentity.NewInstrumentation(ctx)

	if g.manager == nil {
		if obs.Verbose {
			obs.LogDebug("github server token", "installationID", g.InstallationID, "cached", false)
		}
		obs.AddEvent("github.serverToken", attribute.Int64("installationID", g.InstallationID))
		return g.fetchInstallationToken(ctx)
	}

	if obs.Verbose {
		obs.LogDebug("github server token", "installationID", g.InstallationID, "cached", true)
	}
	obs.AddEvent("github.serverToken", attribute.Int64("installationID", g.InstallationID))

	cacheKey := fmt.Sprintf("installation:%d", g.InstallationID)

	return g.manager.Do(ctx, cacheKey, func(ctx context.Context) (manager.FetchResult[tokenprovider.Token], error) {
		token, err := g.fetchInstallationToken(ctx)
		if err != nil {
			return manager.FetchResult[tokenprovider.Token]{}, err
		}
		return manager.FetchResult[tokenprovider.Token]{
			Value:  token,
			TTL:    time.Until(token.ExpiresAt),
			Policy: manager.CacheWithTTL,
		}, nil
	}, obs.ManagerHooks())
}

func (g *App) fetchInstallationToken(ctx context.Context) (tokenprovider.Token, error) {
	jwt, err := buildJWT(g.ClientID, g.PrivateKey)
	if err != nil {
		return tokenprovider.Token{}, fmt.Errorf("building JWT: %w", err)
	}

	return exchangeForInstallationToken(ctx, jwt, g.tokenURL, g.httpClient)
}

func defaultHTTPClient() *httpc.Client {
	defaultHTTPClientOnce.Do(func() {
		defaultHTTPClientVal = httpc.NewClient(
			&httpc.ClientConfig{
				Timeout:     10 * time.Second,
				EnableCache: false,
				RetryMax:    2,
			},
		)
	})
	return defaultHTTPClientVal
}
