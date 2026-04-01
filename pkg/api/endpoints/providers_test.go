// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package endpoints

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/api"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
)

// testProvider is a minimal provider for endpoint test registration.
type testProvider struct {
	desc *provider.Descriptor
}

func (tp *testProvider) Descriptor() *provider.Descriptor { return tp.desc }
func (tp *testProvider) Execute(_ context.Context, _ any) (*provider.Output, error) {
	return &provider.Output{}, nil
}

// newRegistryWithProvider creates a provider registry with a single test provider.
func newRegistryWithProvider(t *testing.T, name string) *provider.Registry {
	t.Helper()
	reg := provider.NewRegistry()
	p := &testProvider{
		desc: &provider.Descriptor{
			Name:         name,
			DisplayName:  name,
			Description:  "A test provider for unit tests",
			APIVersion:   "v1",
			Version:      semver.MustParse("1.2.3"),
			Capabilities: []provider.Capability{provider.CapabilityFrom},
			Category:     "test",
			Schema:       schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{"input": schemahelper.StringProp("Input")}),
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{"output": schemahelper.StringProp("Output")}),
			},
		},
	}
	require.NoError(t, reg.Register(p))
	return reg
}

func TestRegisterProviderEndpoints_ListEmpty(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterProviderEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Get("/v1/providers")
	require.Equal(t, http.StatusOK, resp.Code)
	// When registry is nil, returns empty response
	assert.Contains(t, resp.Body.String(), "items")
}

func TestRegisterProviderEndpoints_DetailNotFound(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterProviderEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Get("/v1/providers/nonexistent")
	assert.Equal(t, http.StatusNotFound, resp.Code)
}

func TestRegisterProviderEndpoints_SchemaNotFound(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterProviderEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Get("/v1/providers/nonexistent/schema")
	assert.Equal(t, http.StatusNotFound, resp.Code)
}

func BenchmarkProviderListEndpoint(b *testing.B) {
	_, testAPI := humatest.New(b)
	var shutting int32
	hctx := &api.HandlerContext{
		Config:         &config.Config{},
		IsShuttingDown: &shutting,
		StartTime:      time.Now(),
	}
	RegisterProviderEndpoints(testAPI, hctx, "/v1")
	for b.Loop() {
		testAPI.Get("/v1/providers")
	}
}

func TestBuildProviderList(t *testing.T) {
	reg := newRegistryWithProvider(t, "test-provider")
	items := buildProviderList(reg)
	require.Len(t, items, 1)
	assert.Equal(t, "test-provider", items[0].Name)
	assert.Equal(t, "1.2.3", items[0].Version)
	assert.Equal(t, []string{"from"}, items[0].Capabilities)
	assert.Equal(t, "test", items[0].Category)
}

func TestCapabilityStrings_NonEmpty(t *testing.T) {
	caps := []provider.Capability{provider.CapabilityFrom, provider.CapabilityAction}
	result := capabilityStrings(caps)
	assert.Equal(t, []string{"from", "action"}, result)
}

func TestCapabilityStrings_Empty(t *testing.T) {
	result := capabilityStrings(nil)
	assert.Nil(t, result)

	result = capabilityStrings([]provider.Capability{})
	assert.Nil(t, result)
}

func TestVersionString_NonNil(t *testing.T) {
	v := semver.MustParse("2.3.4")
	assert.Equal(t, "2.3.4", versionString(v))
}

func TestVersionString_Nil(t *testing.T) {
	assert.Equal(t, "", versionString(nil))
}

func TestRegisterProviderEndpoints_WithRegistry(t *testing.T) {
	_, testAPI := humatest.New(t)
	var shutting int32
	reg := newRegistryWithProvider(t, "my-provider")
	hctx := &api.HandlerContext{
		Config:           &config.Config{},
		IsShuttingDown:   &shutting,
		StartTime:        time.Now(),
		ProviderRegistry: reg,
	}
	RegisterProviderEndpoints(testAPI, hctx, "/v1")

	// List returns the registered provider.
	resp := testAPI.Get("/v1/providers")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), "my-provider")

	// Get detail by name.
	resp = testAPI.Get("/v1/providers/my-provider")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), "my-provider")

	// Get schema by name.
	resp = testAPI.Get("/v1/providers/my-provider/schema")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), "my-provider")
}

func TestRegisterProviderEndpoints_WithRegistryFilter(t *testing.T) {
	_, testAPI := humatest.New(t)
	var shutting int32
	reg := newRegistryWithProvider(t, "filter-provider")
	hctx := &api.HandlerContext{
		Config:           &config.Config{},
		IsShuttingDown:   &shutting,
		StartTime:        time.Now(),
		ProviderRegistry: reg,
	}
	RegisterProviderEndpoints(testAPI, hctx, "/v1")

	// "true" is a valid CEL boolean literal that returns all items.
	resp := testAPI.Get("/v1/providers?filter=true")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), "filter-provider")
}

func TestRegisterProviderEndpoints_FilterInvalid(t *testing.T) {
	_, testAPI := humatest.New(t)
	var shutting int32
	reg := newRegistryWithProvider(t, "prov")
	hctx := &api.HandlerContext{
		Config:           &config.Config{},
		IsShuttingDown:   &shutting,
		StartTime:        time.Now(),
		ProviderRegistry: reg,
	}
	RegisterProviderEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Get("/v1/providers?filter=!!invalid!!")
	assert.Equal(t, http.StatusBadRequest, resp.Code)
}

func BenchmarkBuildProviderList(b *testing.B) {
	reg := provider.NewRegistry()
	p := &testProvider{
		desc: &provider.Descriptor{
			Name:         "bench-provider",
			DisplayName:  "bench-provider",
			Description:  "Provider used in benchmarks only",
			APIVersion:   "v1",
			Version:      semver.MustParse("1.0.0"),
			Capabilities: []provider.Capability{provider.CapabilityFrom},
			Category:     "bench",
			Schema:       schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{}),
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{}),
			},
		},
	}
	_ = reg.Register(p)
	for b.Loop() {
		buildProviderList(reg)
	}
}
