// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/solution/prepare"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServer_prepareOptions(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	t.Run("baseline always includes the registry option", func(t *testing.T) {
		opts := srv.prepareOptions(context.Background())
		assert.NotEmpty(t, opts, "expected at least the registry option")
	})

	t.Run("official registry adds an option", func(t *testing.T) {
		base := srv.prepareOptions(context.Background())
		ctx := official.WithRegistry(context.Background(), official.NewRegistry())
		withOfficial := srv.prepareOptions(ctx)
		assert.GreaterOrEqual(t, len(withOfficial), len(base)+1,
			"official registry in context should add a prepare option")
	})

	t.Run("auth registry adds an option", func(t *testing.T) {
		base := srv.prepareOptions(context.Background())
		ctx := auth.WithRegistry(context.Background(), auth.NewRegistry())
		withAuth := srv.prepareOptions(ctx)
		assert.GreaterOrEqual(t, len(withAuth), len(base)+1,
			"auth registry in context should add a prepare option")
	})

	t.Run("extra options are appended", func(t *testing.T) {
		base := srv.prepareOptions(context.Background())
		withExtra := srv.prepareOptions(context.Background(), prepare.WithStrict(true))
		assert.Len(t, withExtra, len(base)+1,
			"extra options should be appended to the base set")
	})
}
