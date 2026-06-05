// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"context"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/tokenprovider"
	"github.com/oakwood-commons/scafctl/pkg/tokenprovider/callerscope"
)

var _ tokenprovider.TokenProvider = (*PAT)(nil)

// PAT implements tokenprovider.TokenProvider using a static Personal Access Token.
// PATs do not expire from the API's perspective (they have user-configured lifetimes),
// so no caching or refresh logic is needed.
type PAT struct {
	token      string
	strategies map[callerscope.CallerScope]func(ctx context.Context, opts tokenprovider.RequestOptions) (tokenprovider.Token, error)
}

// NewPATIdentity constructs a GitHub PAT identity from a resolved token string.
func NewPATIdentity(token string) (*PAT, error) {
	if token == "" {
		return nil, fmt.Errorf("token is required")
	}
	p := &PAT{
		token:      token,
		strategies: make(map[callerscope.CallerScope]func(ctx context.Context, opts tokenprovider.RequestOptions) (tokenprovider.Token, error)),
	}
	p.strategies[callerscope.ServerCaller] = p.serverToken
	return p, nil
}

func (p *PAT) Name() string {
	return "github"
}

func (p *PAT) GetToken(ctx context.Context, opts tokenprovider.RequestOptions) (tokenprovider.Token, error) {
	strategy, ok := p.strategies[opts.Caller]
	if !ok {
		return tokenprovider.Token{}, fmt.Errorf("unsupported caller: %s", opts.Caller)
	}
	return strategy(ctx, opts)
}

func (p *PAT) serverToken(_ context.Context, _ tokenprovider.RequestOptions) (tokenprovider.Token, error) {
	return tokenprovider.Token{
		AccessToken: p.token,
		TokenType:   "Bearer",
	}, nil
}
