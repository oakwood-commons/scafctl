// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package entra

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContextWithClaimsChallenge_RoundTrip(t *testing.T) {
	claims := `{"access_token":{"acrs":{"essential":true,"value":"c1"}}}`
	ctx := ContextWithClaimsChallenge(context.Background(), claims)
	got := claimsChallengeFromContext(ctx)
	assert.Equal(t, claims, got)
}

func TestClaimsChallengeFromContext_Empty(t *testing.T) {
	got := claimsChallengeFromContext(context.Background())
	assert.Empty(t, got)
}

func TestClaimsChallengeFromContext_EmptyString(t *testing.T) {
	ctx := ContextWithClaimsChallenge(context.Background(), "")
	got := claimsChallengeFromContext(ctx)
	assert.Empty(t, got)
}
