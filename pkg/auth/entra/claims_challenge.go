// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package entra

import "context"

// claimsChallengeKey is the context key for passing a claims challenge string
// through the login flow.  This avoids extending the SDK LoginOptions type.
type claimsChallengeKey struct{}

// ContextWithClaimsChallenge returns a child context carrying the raw claims
// challenge JSON so that the authorization code flow can append it to the
// authorize URL.
func ContextWithClaimsChallenge(ctx context.Context, claims string) context.Context {
	return context.WithValue(ctx, claimsChallengeKey{}, claims)
}

// claimsChallengeFromContext extracts the claims challenge string from ctx.
// Returns "" when no challenge is present.
func claimsChallengeFromContext(ctx context.Context) string {
	v, _ := ctx.Value(claimsChallengeKey{}).(string)
	return v
}
