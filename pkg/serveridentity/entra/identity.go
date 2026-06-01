package entra

import (
	"context"
	"fmt"
	"sync"
	"time"

	manager "github.com/oakwood-commons/go-flight/cache"
	"github.com/oakwood-commons/scafctl/pkg/api/middleware"
	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/serveridentity"
	"github.com/oakwood-commons/scafctl/pkg/tokenprovider"
	"github.com/oakwood-commons/scafctl/pkg/tokenprovider/callerscope"
	"go.opentelemetry.io/otel/attribute"
)

var (
	defaultHTTPClientOnce sync.Once
	defaultHTTPClientVal  *httpc.Client
)

var (
	ErrEntraNoTenantID            = fmt.Errorf("tenant ID is required")
	ErrEntraNoClientID            = fmt.Errorf("client ID is required")
	ErrEntraNoCredential          = fmt.Errorf("at least one credential is required (federatedTokenFile or clientSecret)")
	ErrEntraInvalidCredentialType = fmt.Errorf("credentialType must be %q or %q", CredentialTypeWIF, CredentialTypeSecret)
	ErrEntraWIFMissingTokenFile   = fmt.Errorf("federatedTokenFile is required when credentialType is %q", CredentialTypeWIF)
	ErrEntraSecretMissing         = fmt.Errorf("clientSecret is required when credentialType is %q", CredentialTypeSecret)
	ErrNoCallerToken              = fmt.Errorf("no caller token in context")
	ErrNoScope                    = fmt.Errorf("scope is required for token delegation")
)
var _ tokenprovider.TokenProvider = (*Entra)(nil)

type Entra struct {
	TokenURL   string
	ClientID   string
	TenantID   string
	Credential ServerCredential
	manager    *manager.Manager[string, tokenprovider.Token]
	httpClient *httpc.Client
	strategies map[callerscope.CallerScope]func(ctx context.Context, scope string) (tokenprovider.Token, error)
}

type Config struct {
	TenantID           string
	ClientID           string
	CredentialType     CredentialType // required: "wif" or "secret"
	FederatedTokenFile string         // required when CredentialType is "wif"
	ClientSecret       string         // required when CredentialType is "secret"
}

type Option func(*Entra)

func WithManager(mgr *manager.Manager[string, tokenprovider.Token]) Option {
	return func(e *Entra) { e.manager = mgr }
}

// WithHTTPClient overrides the default HTTP client used for token endpoint requests.
func WithHTTPClient(client *httpc.Client) Option {
	return func(e *Entra) { e.httpClient = client }
}

func (e *Entra) Name() string {
	return "entra"
}

func (e *Entra) GetToken(ctx context.Context, opts tokenprovider.RequestOptions) (tokenprovider.Token, error) {
	obs := serveridentity.NewInstrumentation(ctx)
	if obs.Verbose {
		obs.LogDebug("entra token request", "caller", opts.Caller, "scope", opts.Scope)
	}
	obs.AddEvent("entra.GetToken", attribute.String("caller", string(opts.Caller)), attribute.String("scope", opts.Scope))

	strategy, ok := e.strategies[opts.Caller]
	if !ok {
		return tokenprovider.Token{}, fmt.Errorf("no strategy found for caller: %v", opts.Caller)
	}
	return strategy(ctx, opts.Scope)
}

func (e *Entra) ServerToken(ctx context.Context, scope string) (tokenprovider.Token, error) {
	obs := serveridentity.NewInstrumentation(ctx)
	if obs.Verbose {
		obs.LogDebug("server credential flow", "scope", scope)
	}
	obs.AddEvent("entra.ServerToken", attribute.String("scope", scope))

	flowParams := FlowParams{
		Scope:    scope,
		ClientID: e.ClientID,
	}
	fn := clientCredentialFlow(e.TokenURL, e.Credential, e.httpClient)

	if e.manager == nil {
		return fn(ctx, flowParams)
	}

	cacheKey, ok := ClientCredKeyGenerator(flowParams, nil)
	if !ok {
		return fn(ctx, flowParams)
	}

	return e.manager.Do(ctx, cacheKey, func(ctx context.Context) (manager.FetchResult[tokenprovider.Token], error) {
		token, err := fn(ctx, flowParams)
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

func (e *Entra) CallerToken(ctx context.Context, scope string) (tokenprovider.Token, error) {
	obs := serveridentity.NewInstrumentation(ctx)

	callerToken := middleware.AccessTokenFromContext(ctx)
	if callerToken == "" {
		return tokenprovider.Token{}, ErrNoCallerToken
	}

	if scope == "" {
		return tokenprovider.Token{}, ErrNoScope
	}

	var callerType string
	if claims := middleware.ClaimsFromContext(ctx); claims != nil {
		callerType = claims.CallerType()
	}

	obs.AddEvent("entra.CallerToken", attribute.String("scope", scope), attribute.String("callerType", callerType))

	var fn FlowFn

	if callerType == "app" {
		fn = clientCredentialFlow(e.TokenURL, e.Credential, e.httpClient)
	} else {
		fn = oboFlow(e.TokenURL, e.Credential, e.httpClient)
	}

	flowParams := FlowParams{
		CallerToken: callerToken,
		Scope:       scope,
		ClientID:    e.ClientID,
	}

	if e.manager == nil {
		if obs.Verbose {
			obs.LogDebug("caller token flow", "callerType", callerType, "scope", scope, "cached", false)
		}
		return fn(ctx, flowParams)
	}

	keyGenerator := GetKeyGenerator(callerType)
	cacheKey, ok := keyGenerator(flowParams, SHA256Hash)
	if !ok {
		if obs.Verbose {
			obs.LogDebug("caller token flow", "callerType", callerType, "scope", scope, "cached", false)
		}
		return fn(ctx, flowParams)
	}

	if obs.Verbose {
		obs.LogDebug("caller token flow", "callerType", callerType, "scope", scope, "cached", true)
	}

	return e.manager.Do(ctx, cacheKey, func(ctx context.Context) (manager.FetchResult[tokenprovider.Token], error) {
		token, err := fn(ctx, flowParams)
		if err != nil {
			return manager.FetchResult[tokenprovider.Token]{}, err
		}
		return manager.FetchResult[tokenprovider.Token]{
			Value:  token,
			TTL:    time.Duration(time.Until(token.ExpiresAt)), //nolint:gosec // ExpiresAt is a positive int from OAuth response
			Policy: manager.CacheWithTTL,
		}, nil
	}, obs.ManagerHooks())
}

func NewEntraIdentity(cfg Config, opts ...Option) (*Entra, error) {
	if cfg.TenantID == "" {
		return nil, ErrEntraNoTenantID
	}
	if cfg.ClientID == "" {
		return nil, ErrEntraNoClientID
	}

	var cred ServerCredential
	switch cfg.CredentialType {
	case CredentialTypeWIF:
		if cfg.FederatedTokenFile == "" {
			return nil, ErrEntraWIFMissingTokenFile
		}
		cred = &WIFCredential{
			TokenFile:           cfg.FederatedTokenFile,
			ClientAssertionType: "urn:ietf:params:oauth:client-assertion-type:jwt-bearer",
		}
	case CredentialTypeSecret:
		if cfg.ClientSecret == "" {
			return nil, ErrEntraSecretMissing
		}
		cred = &SecretCredential{Secret: cfg.ClientSecret}
	default:
		return nil, ErrEntraInvalidCredentialType
	}

	e := &Entra{
		TokenURL:   fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", cfg.TenantID),
		ClientID:   cfg.ClientID,
		TenantID:   cfg.TenantID,
		Credential: cred,
		httpClient: defaultHTTPClient(),
		strategies: make(map[callerscope.CallerScope]func(ctx context.Context, scope string) (tokenprovider.Token, error)),
	}

	for _, opt := range opts {
		opt(e)
	}

	e.strategies[callerscope.ServerCaller] = e.ServerToken
	e.strategies[callerscope.RequesterCaller] = e.CallerToken

	return e, nil
}

func defaultHTTPClient() *httpc.Client {
	defaultHTTPClientOnce.Do(func() {
		defaultHTTPClientVal = httpc.NewClient(
			&httpc.ClientConfig{
				Timeout:     5 * time.Second,
				EnableCache: false,
				RetryMax:    2,
			},
		)
	})
	return defaultHTTPClientVal
}
