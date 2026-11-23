// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoreContext(t *testing.T) {
	assert.Nil(t, FromContext(nil)) //nolint:staticcheck // intentional nil ctx check
	assert.Nil(t, FromContext(context.Background()))
	assert.Equal(t, context.Background(), WithStore(context.Background(), nil))

	mock := NewMockStore()
	ctx := WithStore(context.Background(), mock)
	got := FromContext(ctx)
	require.NotNil(t, got)
	assert.Same(t, mock, got)
}
