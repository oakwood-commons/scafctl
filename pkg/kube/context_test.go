// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolverFromContext_RoundTrip(t *testing.T) {
	t.Parallel()

	resolver := &MockResolver{}
	ctx := WithResolver(context.Background(), resolver)

	got := ResolverFromContext(ctx)
	assert.Same(t, resolver, got)
}

func TestResolverFromContext_Absent(t *testing.T) {
	t.Parallel()

	assert.Nil(t, ResolverFromContext(context.Background()))
}
