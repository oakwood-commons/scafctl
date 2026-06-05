package entra

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/serveridentity"
	"github.com/oakwood-commons/scafctl/pkg/tokenprovider"
	"go.opentelemetry.io/otel/trace"
)

const (
	grantTypeJWTBearer   = "urn:ietf:params:oauth:grant-type:jwt-bearer" //nolint:gosec // G101 -- OAuth grant_type parameter value, not a credential
	grantTypeClientCreds = "client_credentials"
	requestedTokenUseOBO = "on_behalf_of"
	contentTypeForm      = "application/x-www-form-urlencoded"
)

type FlowParams struct {
	CallerToken string // the inbound bearer token (assertion for OBO)
	Scope       string // desired downstream scope
	ClientID    string // optional client ID override (for multi-tenant delegation)
}

type FlowFn func(ctx context.Context, params FlowParams) (tokenprovider.Token, error)

func oboFlow(tokenURL string, cred ServerCredential, client *httpc.Client) FlowFn {
	return func(ctx context.Context, params FlowParams) (tokenprovider.Token, error) {
		v := url.Values{
			"grant_type":          {grantTypeJWTBearer},
			"client_id":           {params.ClientID},
			"assertion":           {params.CallerToken},
			"scope":               {params.Scope},
			"requested_token_use": {requestedTokenUseOBO},
		}
		if err := cred.Apply(v); err != nil {
			return tokenprovider.Token{}, fmt.Errorf("applying server credential: %w", err)
		}
		return executeTokenRequest(ctx, client, tokenURL, v)
	}
}

func clientCredentialFlow(tokenURL string, cred ServerCredential, client *httpc.Client) FlowFn {
	return func(ctx context.Context, params FlowParams) (tokenprovider.Token, error) {
		v := url.Values{
			"grant_type": {grantTypeClientCreds},
			"client_id":  {params.ClientID},
			"scope":      {params.Scope},
		}
		if err := cred.Apply(v); err != nil {
			return tokenprovider.Token{}, fmt.Errorf("applying server credential: %w", err)
		}
		return executeTokenRequest(ctx, client, tokenURL, v)
	}
}

func executeTokenRequest(ctx context.Context, client *httpc.Client, tokenURL string, params url.Values) (tokenprovider.Token, error) {
	log := logger.FromContext(ctx)
	start := time.Now()
	span := trace.SpanFromContext(ctx)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(params.Encode()))
	if err != nil {
		serveridentity.SpanError(span, err, "building token request failed")
		return tokenprovider.Token{}, fmt.Errorf("building token request: %w", err)
	}
	req.Header.Set("Content-Type", contentTypeForm)

	if log.V(2).Enabled() {
		log.V(2).Info("token endpoint request", "url", tokenURL, "grantType", params.Get("grant_type"), "scope", params.Get("scope"))
	}

	resp, err := client.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		serveridentity.SpanError(span, err, "token endpoint request failed")
		if log.V(1).Enabled() {
			log.V(1).Error(err, "token endpoint request failed", "url", tokenURL)
		}
		return tokenprovider.Token{}, fmt.Errorf("token endpoint request failed: %w", err)
	}

	serveridentity.SpanSetStatusCode(span, resp.StatusCode)

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		serveridentity.SpanError(span, err, "reading token response failed")
		if log.V(1).Enabled() {
			log.V(1).Error(err, "reading token response failed")
		}
		return tokenprovider.Token{}, fmt.Errorf("reading token response: %w", err)
	}

	elapsed := time.Since(start)
	if resp.StatusCode != http.StatusOK {
		serveridentity.SpanError(span, nil, fmt.Sprintf("token endpoint returned %d", resp.StatusCode))
		if log.V(1).Enabled() {
			log.V(1).Info("token endpoint error", "status", resp.StatusCode, "elapsed", elapsed)
		}
		detail := string(body)
		if len(detail) > 512 {
			detail = detail[:512] + "...(truncated)"
		}
		return tokenprovider.Token{}, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, detail)
	}

	var response map[string]any
	if err := json.Unmarshal(body, &response); err != nil {
		serveridentity.SpanError(span, err, "parsing token response failed")
		return tokenprovider.Token{}, fmt.Errorf("parsing token response: %w", err)
	}

	accessToken, _ := response["access_token"].(string)
	if accessToken == "" {
		serveridentity.SpanError(span, nil, "token response missing access_token")
		if log.V(1).Enabled() {
			log.V(1).Info("token response missing access_token", "elapsed", elapsed)
		}
		return tokenprovider.Token{}, fmt.Errorf("token response missing access_token")
	}

	var expiresIn int64
	if exp, ok := response["expires_in"].(float64); ok {
		expiresIn = int64(exp)
	}

	if log.V(1).Enabled() {
		log.V(1).Info("token endpoint success", "elapsed", elapsed, "expiresIn", expiresIn)
	}
	return tokenprovider.Token{
		AccessToken: accessToken,
		ExpiresAt:   time.Now().Add(time.Duration(expiresIn) * time.Second),
		TokenType:   "Bearer",
	}, nil
}
