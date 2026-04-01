// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/oakwood-commons/scafctl/pkg/settings"
)

func TestBuildHumaConfig(t *testing.T) {
	cfg := BuildHumaConfig(settings.DefaultAPIVersion)

	assert.NotNil(t, cfg.Info)
	assert.Equal(t, "scafctl API", cfg.Info.Title)
	assert.Contains(t, cfg.DocsPath, settings.DefaultAPIVersion)
	assert.Contains(t, cfg.OpenAPIPath, settings.DefaultAPIVersion)
}

func TestConfigureSecuritySchemes(t *testing.T) {
	cfg := BuildHumaConfig("v1")
	assert.NotNil(t, cfg.Components)
	assert.NotNil(t, cfg.Components.SecuritySchemes)
	assert.Contains(t, cfg.Components.SecuritySchemes, "oauth2")
}

func TestConfigureOpenAPIServers(t *testing.T) {
	cfg := BuildHumaConfig("v1")
	assert.NotEmpty(t, cfg.Servers)
	assert.Contains(t, cfg.Servers[0].URL, fmt.Sprintf("%d", settings.DefaultAPIPort))
}

func TestServer_InitAPI_SetsAPI(t *testing.T) {
	srv, err := NewServer()
	assert.NoError(t, err)

	srv.InitAPI()
	assert.NotNil(t, srv.API())
}

func TestServer_API_PanicsBeforeInitAPI(t *testing.T) {
	srv, err := NewServer()
	assert.NoError(t, err)
	assert.Panics(t, func() { srv.API() })
}

func BenchmarkBuildHumaConfig(b *testing.B) {
	for b.Loop() {
		BuildHumaConfig("v1")
	}
}
