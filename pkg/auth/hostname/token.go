// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package hostname

import (
	"context"

	"github.com/oakwood-commons/scafctl/pkg/tokenprovider"
	"github.com/oakwood-commons/scafctl/pkg/tokenprovider/callerscope"
)

// defaultToken retrieves a bearer token for the given auth provider using
// cached, non-interactive credentials only. It never triggers an interactive
// login: if no valid cached token exists, the underlying provider returns an
// error which the caller maps to ErrNoCredentials.
func defaultToken(ctx context.Context, provider, scope string) (string, error) {
	tok, err := tokenprovider.GetToken(ctx, provider, tokenprovider.RequestOptions{
		Scope:  scope,
		Caller: callerscope.ServerCaller,
	})
	if err != nil {
		return "", err
	}
	return tok.AccessToken, nil
}
