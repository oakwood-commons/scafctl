// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package authdelegation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	httpc "github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/logger"
)

const (
	grantTypeJWTBearer   = "urn:ietf:params:oauth:grant-type:jwt-bearer" //nolint:gosec // G101 -- OAuth grant_type parameter value, not a credential
	grantTypeClientCreds = "client_credentials"
	requestedTokenUseOBO = "on_behalf_of"
	contentTypeForm      = "application/x-www-form-urlencoded"
)

// FlowFn executes a token delegation flow.
type FlowFn func(ctx context.Context, params FlowParams) (TokenResult, error)

// FlowParams holds the per-request inputs a flow needs to build its request.
type FlowParams struct {
	CallerToken string // the inbound bearer token (assertion for OBO)
	Scope       string // desired downstream scope
	ClientID    string // optional client ID override (for multi-tenant delegation)
}

// oboFlow returns a FlowFn that performs the On-Behalf-Of flow.
func oboFlow(tokenURL string, cred ServerCredential, client *httpc.Client) FlowFn {
	return func(ctx context.Context, params FlowParams) (TokenResult, error) {
		v := url.Values{
			"grant_type":          {grantTypeJWTBearer},
			"client_id":           {params.ClientID},
			"assertion":           {params.CallerToken},
			"scope":               {params.Scope},
			"requested_token_use": {requestedTokenUseOBO},
		}
		if err := cred.Apply(v); err != nil {
			return TokenResult{}, fmt.Errorf("applying server credential: %w", err)
		}
		return executeTokenRequest(ctx, client, tokenURL, v)
	}
}

func clientCredentialFlow(tokenURL string, cred ServerCredential, client *httpc.Client) FlowFn {
	return func(ctx context.Context, params FlowParams) (TokenResult, error) {
		v := url.Values{
			"grant_type": {grantTypeClientCreds},
			"client_id":  {params.ClientID},
			"scope":      {params.Scope},
		}
		if err := cred.Apply(v); err != nil {
			return TokenResult{}, fmt.Errorf("applying server credential: %w", err)
		}
		return executeTokenRequest(ctx, client, tokenURL, v)
	}
}

func executeTokenRequest(ctx context.Context, client *httpc.Client, tokenURL string, params url.Values) (TokenResult, error) {
	log := logger.FromContext(ctx)
	start := time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(params.Encode()))
	if err != nil {
		return TokenResult{}, fmt.Errorf("building token request: %w", err)
	}
	req.Header.Set("Content-Type", contentTypeForm)

	log.V(2).Info("token endpoint request", "url", tokenURL, "grantType", params.Get("grant_type"), "scope", params.Get("scope"))

	resp, err := client.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return TokenResult{}, fmt.Errorf("token endpoint request failed: %w", err)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return TokenResult{}, fmt.Errorf("reading token response: %w", err)
	}

	elapsed := time.Since(start)

	if resp.StatusCode != http.StatusOK {
		log.Info("token endpoint error", "status", resp.StatusCode, "elapsed", elapsed)
		detail := string(body)
		if len(detail) > 512 {
			detail = detail[:512] + "...(truncated)"
		}
		return TokenResult{}, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, detail)
	}

	var response map[string]any
	if err := json.Unmarshal(body, &response); err != nil {
		return TokenResult{}, fmt.Errorf("parsing token response: %w", err)
	}

	accessToken, _ := response["access_token"].(string)
	if accessToken == "" {
		return TokenResult{}, fmt.Errorf("token response missing access_token")
	}

	var expiresIn int64
	if exp, ok := response["expires_in"].(float64); ok {
		expiresIn = int64(exp)
	}

	log.V(1).Info("token endpoint success", "elapsed", elapsed, "expiresIn", expiresIn)

	return TokenResult{
		AccessToken: accessToken,
		ExpiresIn:   expiresIn,
	}, nil
}

// FlowRegistry holds the permitted delegation flows by name.
// Only flows explicitly registered can be executed.
//
// FlowRegistry is NOT safe for concurrent use. All Register calls must
// complete before any Select or Has calls (write-at-init, read-at-request).
type FlowRegistry struct {
	flows map[string]FlowFn
}

// NewFlowRegistry creates an empty FlowRegistry.
func NewFlowRegistry() *FlowRegistry {
	return &FlowRegistry{flows: make(map[string]FlowFn)}
}

// Register adds a flow function under the given name.
func (r *FlowRegistry) Register(name string, fn FlowFn) {
	r.flows[name] = fn
}

// Select returns the flow function for the given caller type.
// Returns an error if the resolved flow is not registered.
func (r *FlowRegistry) Select(callerType string) (FlowFn, error) {
	name := FlowNameForCaller(callerType)
	fn, ok := r.flows[name]
	if !ok {
		return nil, fmt.Errorf("delegation flow %q is not permitted", name)
	}
	return fn, nil
}

// Has reports whether the named flow is registered.
func (r *FlowRegistry) Has(name string) bool {
	_, ok := r.flows[name]
	return ok
}

// FlowNameForCaller maps a caller type to its delegation flow name.
func FlowNameForCaller(callerType string) string {
	if callerType == "user" {
		return "obo"
	}
	return "client_credentials"
}
