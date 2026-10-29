// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/solution/load"
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

func TestServer_prepareSolution_RejectsStdin(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	_, err = srv.prepareSolution(context.Background(), "-")
	require.ErrorIs(t, err, load.ErrSourceNotEnabled)
}

func TestLocalSolution(t *testing.T) {
	dir := t.TempDir()
	solFile := filepath.Join(dir, "solution.yaml")
	require.NoError(t, os.WriteFile(solFile, []byte("kind: Solution\n"), 0o600))

	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)

	ctx := provider.WithWorkingDirectory(context.Background(), dir)

	t.Run("relative path resolves against the working directory", func(t *testing.T) {
		local, errResult := localSolution(ctx, "solution.yaml")
		require.Nil(t, errResult)
		t.Cleanup(func() { _ = local.Close() })
		assert.Equal(t, solFile, local.Path)
		assert.False(t, local.Fetched())
	})

	tests := []struct {
		name string
		ref  string
		code string
	}{
		{name: "missing path", ref: "./missing.yaml", code: ErrCodeNotFound},
		{name: "stdin", ref: "-", code: ErrCodeInvalidInput},
		{name: "fetch failure", ref: srv.URL + "/solution.yaml", code: ErrCodeLoadFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			local, errResult := localSolution(ctx, tt.ref)
			assert.Nil(t, local)
			require.NotNil(t, errResult)
			assert.True(t, errResult.IsError)

			var te ToolError
			require.NoError(t, json.Unmarshal([]byte(errResult.Content[0].(mcp.TextContent).Text), &te))
			assert.Equal(t, tt.code, te.Code, te.Message)
			assert.Equal(t, "path", te.Field)
		})
	}
}
