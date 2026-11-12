// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package endpoints

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/api"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

func TestRegisterAdminEndpoints_Info(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterAdminEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Get("/v1/admin/info")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), "uptime")
	assert.Contains(t, resp.Body.String(), "startTime")
}

func TestRegisterAdminEndpoints_Info_NilProviderRegistry(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterAdminEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Get("/v1/admin/info")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), `"providers":0`)
}

// statsPool is an api.PluginPool that reports fixed stats.
type statsPool struct {
	stats plugin.PoolStats
}

func (p *statsPool) EnsureAndAcquire(context.Context, []solution.PluginDependency) (func(), error) {
	return func() {}, nil
}

func (p *statsPool) Stats() plugin.PoolStats { return p.stats }

func (p *statsPool) Shutdown() {}

func TestRegisterAdminEndpoints_Info_PluginPool(t *testing.T) {
	tests := []struct {
		name string
		pool api.PluginPool
		want *AdminPluginPoolStats
	}{
		{
			name: "no pool omits plugins",
		},
		{
			name: "pool stats are reported",
			pool: &statsPool{stats: plugin.PoolStats{Active: 1, Idle: 2, Dead: 3, Total: 6, Evicted: 7}},
			want: &AdminPluginPoolStats{Active: 1, Idle: 2, Dead: 3, Total: 6, Evicted: 7},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, testAPI := humatest.New(t)
			hctx := newTestHandlerContext(t)
			hctx.PluginPool = tt.pool
			RegisterAdminEndpoints(testAPI, hctx, "/v1")

			resp := testAPI.Get("/v1/admin/info")
			require.Equal(t, http.StatusOK, resp.Code)

			var body struct {
				Plugins *AdminPluginPoolStats `json:"plugins"`
			}
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
			assert.Equal(t, tt.want, body.Plugins)
			if tt.want == nil {
				assert.NotContains(t, resp.Body.String(), `"plugins"`)
			}
		})
	}
}

func TestRegisterAdminEndpoints_ReloadConfig(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterAdminEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Post("/v1/admin/reload-config")
	require.Equal(t, http.StatusNotImplemented, resp.Code)
}

func TestRegisterAdminEndpoints_ClearCache(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterAdminEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Post("/v1/admin/clear-cache")
	require.Equal(t, http.StatusNotImplemented, resp.Code)
}

func BenchmarkAdminInfoEndpoint(b *testing.B) {
	_, testAPI := humatest.New(b)
	var shutting int32
	hctx := &api.HandlerContext{
		Config:         &config.Config{},
		IsShuttingDown: &shutting,
		StartTime:      time.Now(),
	}
	RegisterAdminEndpoints(testAPI, hctx, "/v1")
	for b.Loop() {
		testAPI.Get("/v1/admin/info")
	}
}
