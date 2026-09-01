// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/gavv/httpexpect/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/api"
	"github.com/oakwood-commons/scafctl/pkg/api/endpoints"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/plugin/identity"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/celprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/fileprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/messageprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/parameterprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/staticprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/validationprovider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// setupTestServer creates a test HTTP server with all endpoints registered.
func setupTestServer(t testing.TB) *httptest.Server {
	t.Helper()

	cfg := &config.Config{
		APIServer: config.APIServerConfig{
			APIVersion: settings.DefaultAPIVersion,
		},
	}

	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(messageprovider.NewMessageProvider()))
	require.NoError(t, reg.Register(fileprovider.NewFileProvider()))

	srv, err := api.NewServer(
		api.WithServerConfig(cfg),
		api.WithServerVersion("test-dev"),
		api.WithServerRegistry(reg),
		api.WithServerCompositeRegistry(provider.NewCompositeRegistryFromBase(reg)),
	)
	require.NoError(t, err)

	apiRouter, err := api.SetupMiddleware(t.Context(), srv.Router(), &cfg.APIServer, logr.Discard())
	require.NoError(t, err)
	srv.SetAPIRouter(apiRouter)
	srv.InitAPI()

	hctx := srv.HandlerCtx()
	endpoints.RegisterAll(srv.API(), srv.Router(), hctx)

	return httptest.NewServer(srv.Router())
}

// setupExpect creates an httpexpect instance bound to a test server.
func setupExpect(t *testing.T) (*httpexpect.Expect, *httptest.Server) {
	t.Helper()
	ts := setupTestServer(t)
	e := httpexpect.Default(t, ts.URL)
	return e, ts
}

// TestAPI_RootEndpoint verifies the root endpoint returns API info and links.
func TestAPI_RootEndpoint(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	obj := e.GET("/").
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	obj.HasValue("name", "scafctl API")
	obj.Value("version").String().NotEmpty()
	obj.Value("links").Array().NotEmpty()
}

// TestAPI_HealthEndpoint verifies the health endpoint.
func TestAPI_HealthEndpoint(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	obj := e.GET("/health").
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	obj.HasValue("status", "healthy")
	obj.Value("version").String().NotEmpty()
	obj.Value("uptime").String().NotEmpty()
	obj.Value("components").Array()
}

// TestAPI_LivenessProbe verifies lightweight liveness probe.
func TestAPI_LivenessProbe(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	e.GET("/health/live").
		Expect().
		Status(http.StatusOK).
		JSON().Object().
		HasValue("status", "ok")
}

// TestAPI_ReadinessProbe verifies readiness probe.
func TestAPI_ReadinessProbe(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	e.GET("/health/ready").
		Expect().
		Status(http.StatusOK).
		JSON().Object().
		HasValue("status", "ok")
}

// TestAPI_NotFound verifies custom 404 handler returns problem+json.
func TestAPI_NotFound(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	e.GET("/nonexistent").
		Expect().
		Status(http.StatusNotFound).
		HasContentType("application/problem+json").
		Body().Contains("Not Found")
}

// TestAPI_MethodNotAllowed verifies custom 405 handler.
func TestAPI_MethodNotAllowed(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	// Health is GET-only; POST should return 405
	e.POST("/health").
		Expect().
		Status(http.StatusMethodNotAllowed)
}

// TestAPI_ProvidersEndpoint verifies provider listing.
func TestAPI_ProvidersEndpoint(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	e.GET("/v1/providers").
		Expect().
		Status(http.StatusOK).
		JSON().Object().NotEmpty()
}

// TestAPI_ProvidersEndpoint_Pagination verifies provider pagination parameters.
func TestAPI_ProvidersEndpoint_Pagination(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	e.GET("/v1/providers").
		WithQuery("page", 1).
		WithQuery("per_page", 5).
		Expect().
		Status(http.StatusOK).
		JSON().Object().NotEmpty()
}

// TestAPI_ProvidersEndpoint_Filter verifies CEL filtering on providers.
func TestAPI_ProvidersEndpoint_Filter(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	obj := e.GET("/v1/providers").
		WithQuery("filter", `item.name=="file"`).
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	items := obj.Value("items").Array()
	items.Length().IsEqual(1)
	items.Value(0).Object().Value("name").IsEqual("file")
}

// TestAPI_AdminInfoEndpoint verifies admin info returns server metadata.
func TestAPI_AdminInfoEndpoint(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	obj := e.GET("/v1/admin/info").
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	obj.Value("version").String().NotEmpty()
	obj.Value("uptime").String().NotEmpty()
	obj.Value("startTime").String().NotEmpty()
	obj.Value("providers").Number().Ge(0)
}

// TestAPI_EvalCEL verifies CEL evaluation endpoint.
func TestAPI_EvalCEL(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	obj := e.POST("/v1/eval/cel").
		WithJSON(map[string]any{"expression": "1 + 2"}).
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	obj.Value("result").IsEqual(3)
	obj.Value("type").String().NotEmpty()
}

// TestAPI_EvalCEL_StringExpression verifies CEL evaluation with a string expression.
func TestAPI_EvalCEL_StringExpression(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	obj := e.POST("/v1/eval/cel").
		WithJSON(map[string]any{"expression": `"hello" + " world"`}).
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	obj.Value("result").IsEqual("hello world")
	obj.Value("type").String().NotEmpty()
}

// TestAPI_EvalCEL_InvalidExpression verifies CEL eval rejects bad expressions.
func TestAPI_EvalCEL_InvalidExpression(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	e.POST("/v1/eval/cel").
		WithJSON(map[string]any{"expression": "???invalid"}).
		Expect().
		Status(http.StatusBadRequest)
}

// TestAPI_EvalTemplate verifies Go template evaluation endpoint.
func TestAPI_EvalTemplate(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	obj := e.POST("/v1/eval/template").
		WithJSON(map[string]any{
			"template": "Hello, {{.name}}!",
			"data":     map[string]any{"name": "World"},
		}).
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	obj.HasValue("output", "Hello, World!")
}

// TestAPI_MetricsEndpoint verifies Prometheus metrics endpoint.
func TestAPI_MetricsEndpoint(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	body := e.GET("/metrics").
		Expect().
		Status(http.StatusOK).
		Body().Raw()

	// Prometheus metrics contain # HELP or # TYPE lines
	assert.True(t, strings.Contains(body, "# ") || strings.Contains(body, "go_"),
		"expected Prometheus-format metrics")
}

// TestAPI_ConfigEndpoint verifies config endpoint.
func TestAPI_ConfigEndpoint(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	e.GET("/v1/config").
		Expect().
		Status(http.StatusOK).
		JSON().Object().NotEmpty()
}

// TestAPI_OpenAPIEndpoint verifies the OpenAPI spec endpoint.
// Huma appends .json to the configured OpenAPIPath and serves
// with content type application/openapi+json.
func TestAPI_OpenAPIEndpoint(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	body := e.GET("/v1/openapi.json").
		Expect().
		Status(http.StatusOK).
		HasContentType("application/openapi+json").
		Body().Raw()

	assert.Contains(t, body, `"openapi"`)
	assert.Contains(t, body, `"scafctl API"`)
	assert.Contains(t, body, `"paths"`)
}

// TestAPI_ConcurrentRequests verifies the server handles concurrent requests.
func TestAPI_ConcurrentRequests(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	const numRequests = 50
	results := make(chan int, numRequests)

	for range numRequests {
		go func() {
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL+"/health/live", nil)
			if err != nil {
				results <- 0
				return
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				results <- 0
				return
			}
			defer resp.Body.Close()
			results <- resp.StatusCode
		}()
	}

	for range numRequests {
		status := <-results
		assert.Equal(t, http.StatusOK, status)
	}
}

// TestAPI_SetupMiddleware_RejectsInvalidOIDC verifies that misconfigured OIDC
// is caught at startup.
func TestAPI_SetupMiddleware_RejectsInvalidOIDC(t *testing.T) {
	router := chi.NewRouter()
	cfg := &config.APIServerConfig{
		Auth: config.APIAuthConfig{
			AzureOIDC: config.APIAzureOIDCConfig{
				Enabled:  true,
				TenantID: "",
				ClientID: "",
			},
		},
	}

	_, err := api.SetupMiddleware(t.Context(), router, cfg, logr.Discard())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tenantId or clientId is empty")
}

// ─── Provider Detail Endpoints ──────────────────────────────────────────────

// TestAPI_ProviderDetail_NotFound verifies 404 for a nonexistent provider.
func TestAPI_ProviderDetail_NotFound(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL+"/v1/providers/does-not-exist", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.Equal(t, "application/problem+json", resp.Header.Get("Content-Type"))

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "Not Found")
	assert.Contains(t, string(body), "does-not-exist")
}

// TestAPI_ProviderSchema_NotFound verifies 404 when requesting schema of a nonexistent provider.
func TestAPI_ProviderSchema_NotFound(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	e.GET("/v1/providers/nonexistent/schema").
		Expect().
		Status(http.StatusNotFound)
}

// TestAPI_ProvidersEndpoint_InvalidFilter verifies that an invalid CEL filter
// returns 400 when there are items to filter against.
func TestAPI_ProvidersEndpoint_InvalidFilter(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	e.GET("/v1/providers").
		WithQuery("filter", "???invalid-cel").
		Expect().
		Status(http.StatusBadRequest)
}

// TestAPI_ProvidersEndpoint_PaginationResponse verifies pagination metadata structure.
func TestAPI_ProvidersEndpoint_PaginationResponse(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	obj := e.GET("/v1/providers").
		WithQuery("page", 1).
		WithQuery("per_page", 2).
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	obj.ContainsKey("items")
	pag := obj.Value("pagination").Object()
	pag.Value("total_items").Number().Ge(0)
	pag.Value("total_pages").Number().Ge(0)
	pag.ContainsKey("has_more")
}

// TestAPI_ProvidersEndpoint_BeyondLastPage verifies empty items when requesting past the last page.
func TestAPI_ProvidersEndpoint_BeyondLastPage(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	obj := e.GET("/v1/providers").
		WithQuery("page", 9999).
		WithQuery("per_page", 10).
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	obj.Value("items").Array().IsEmpty()
	obj.Value("pagination").Object().Value("has_more").Boolean().IsFalse()
}

// ─── Eval Endpoint Extended Tests ───────────────────────────────────────────

// TestAPI_EvalCEL_WithData verifies CEL evaluation with data context.
// The data field is passed as the root variable "_" in the CEL environment.
func TestAPI_EvalCEL_WithData(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	obj := e.POST("/v1/eval/cel").
		WithJSON(map[string]any{
			"expression": "_.x + _.y",
			"data":       map[string]any{"x": 10, "y": 20},
		}).
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	obj.Value("result").IsEqual(30)
}

// TestAPI_EvalCEL_BoolExpression verifies CEL evaluation returning a boolean.
func TestAPI_EvalCEL_BoolExpression(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	obj := e.POST("/v1/eval/cel").
		WithJSON(map[string]any{"expression": "1 > 0"}).
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	obj.Value("result").IsEqual(true)
	obj.Value("type").String().NotEmpty()
}

// TestAPI_EvalCEL_ListExpression verifies CEL evaluation with lists.
func TestAPI_EvalCEL_ListExpression(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	obj := e.POST("/v1/eval/cel").
		WithJSON(map[string]any{"expression": "[1, 2, 3].size()"}).
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	obj.Value("result").IsEqual(3)
}

// TestAPI_EvalCEL_EmptyExpression verifies that an empty expression returns 400.
func TestAPI_EvalCEL_EmptyExpression(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	e.POST("/v1/eval/cel").
		WithJSON(map[string]any{"expression": ""}).
		Expect().
		Status(http.StatusBadRequest)
}

// TestAPI_EvalCEL_MissingBody verifies that a missing body returns an error.
func TestAPI_EvalCEL_MissingBody(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	e.POST("/v1/eval/cel").
		WithText("").
		Expect().
		Status(http.StatusBadRequest)
}

// TestAPI_EvalTemplate_InvalidSyntax verifies template evaluation rejects invalid templates.
func TestAPI_EvalTemplate_InvalidSyntax(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	e.POST("/v1/eval/template").
		WithJSON(map[string]any{
			"template": "{{.name",
		}).
		Expect().
		Status(http.StatusBadRequest)
}

// TestAPI_EvalTemplate_EmptyTemplate verifies that an empty template returns 400.
func TestAPI_EvalTemplate_EmptyTemplate(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	e.POST("/v1/eval/template").
		WithJSON(map[string]any{"template": ""}).
		Expect().
		Status(http.StatusBadRequest)
}

// TestAPI_EvalTemplate_NoData verifies template evaluation without data context.
func TestAPI_EvalTemplate_NoData(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	obj := e.POST("/v1/eval/template").
		WithJSON(map[string]any{
			"template": "Hello, static!",
		}).
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	obj.HasValue("output", "Hello, static!")
}

// TestAPI_EvalTemplate_CustomName verifies template with custom name field.
func TestAPI_EvalTemplate_CustomName(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	obj := e.POST("/v1/eval/template").
		WithJSON(map[string]any{
			"template": "Greetings from {{.place}}",
			"data":     map[string]any{"place": "integration test"},
			"name":     "custom-tmpl",
		}).
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	obj.HasValue("output", "Greetings from integration test")
}

// TestAPI_EvalTemplate_MissingKeyDefault verifies that missingKey=default renders
// missing keys as "<no value>" instead of returning an error.
func TestAPI_EvalTemplate_MissingKeyDefault(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	obj := e.POST("/v1/eval/template").
		WithJSON(map[string]any{
			"template":   "Hello {{ .missing }}!",
			"missingKey": "default",
		}).
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	obj.HasValue("output", "Hello <no value>!")
}

// TestAPI_EvalTemplate_MissingKeyError verifies that missing keys return a 400
// error by default (missingKey=error).
func TestAPI_EvalTemplate_MissingKeyError(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	e.POST("/v1/eval/template").
		WithJSON(map[string]any{
			"template": "Hello {{ .missing }}!",
			"data":     map[string]any{},
		}).
		Expect().
		Status(http.StatusBadRequest)
}

// ─── Schema Endpoints ───────────────────────────────────────────────────────

// TestAPI_SchemaList verifies the schema list endpoint returns available schemas.
func TestAPI_SchemaList(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	obj := e.GET("/v1/schemas").
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	items := obj.Value("items").Array()
	items.Length().Ge(1)
	items.Value(0).Object().HasValue("name", "solution")

	obj.Value("pagination").Object().Value("total_items").Number().Ge(1)
}

// TestAPI_SchemaGet_Solution verifies getting the solution schema.
func TestAPI_SchemaGet_Solution(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	obj := e.GET("/v1/schemas/solution").
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	obj.HasValue("name", "solution")
	obj.Value("schema").NotNull()
}

// TestAPI_SchemaGet_NotFound verifies 404 for an unknown schema.
func TestAPI_SchemaGet_NotFound(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	e.GET("/v1/schemas/nonexistent").
		Expect().
		Status(http.StatusNotFound)
}

// TestAPI_SchemaValidate_NullData verifies validation rejects null data.
func TestAPI_SchemaValidate_NullData(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	e.POST("/v1/schemas/validate").
		WithJSON(map[string]any{
			"schema": "solution",
			"data":   nil,
		}).
		Expect().
		Status(http.StatusBadRequest)
}

// TestAPI_SchemaValidate_InvalidData verifies validation returns violations for bad data.
func TestAPI_SchemaValidate_InvalidData(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	obj := e.POST("/v1/schemas/validate").
		WithJSON(map[string]any{
			"schema": "solution",
			"data":   map[string]any{"foo": "bar"},
		}).
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	obj.Value("valid").Boolean().IsFalse()
	obj.Value("violations").Array().NotEmpty()
}

// TestAPI_SchemaValidate_ValidData verifies validation accepts a minimal valid solution.
func TestAPI_SchemaValidate_ValidData(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	obj := e.POST("/v1/schemas/validate").
		WithJSON(map[string]any{
			"schema": "solution",
			"data": map[string]any{
				"apiVersion": "scafctl.io/v1",
				"kind":       "Solution",
				"metadata": map[string]any{
					"name":    "test-solution",
					"version": "1.0.0",
				},
			},
		}).
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	obj.Value("valid").Boolean().IsTrue()
}

// ─── Catalog Endpoints ──────────────────────────────────────────────────────

// TestAPI_CatalogList verifies the catalog list endpoint.
func TestAPI_CatalogList(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	obj := e.GET("/v1/catalogs").
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	obj.ContainsKey("items")
	obj.Value("pagination").Object().ContainsKey("total_items")
}

// TestAPI_CatalogList_WithPagination verifies catalog list with pagination.
func TestAPI_CatalogList_WithPagination(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	obj := e.GET("/v1/catalogs").
		WithQuery("page", 1).
		WithQuery("per_page", 5).
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	pag := obj.Value("pagination").Object()
	pag.Value("page").Number().IsEqual(1)
	pag.Value("per_page").Number().IsEqual(5)
}

// TestAPI_CatalogDetail_NotFound verifies 404 for a nonexistent catalog.
func TestAPI_CatalogDetail_NotFound(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	e.GET("/v1/catalogs/nonexistent-catalog").
		Expect().
		Status(http.StatusNotFound)
}

// TestAPI_CatalogList_InvalidFilter verifies that an invalid CEL filter
// returns 200 with empty items when the catalog list is empty (filter not evaluated).
func TestAPI_CatalogList_InvalidFilter(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	// With no catalogs configured, the filter is never evaluated.
	e.GET("/v1/catalogs").
		WithQuery("filter", "!!!bad-cel").
		Expect().
		Status(http.StatusOK)
}

// ─── Snapshot Endpoints ─────────────────────────────────────────────────────

// TestAPI_SnapshotList verifies the snapshot list endpoint.
func TestAPI_SnapshotList(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	obj := e.GET("/v1/snapshots").
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	obj.ContainsKey("items")
	obj.Value("pagination").Object().ContainsKey("total_items")
}

// TestAPI_SnapshotDetail_NotFound verifies 404 for a nonexistent snapshot.
func TestAPI_SnapshotDetail_NotFound(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	e.GET("/v1/snapshots/nonexistent-snap").
		Expect().
		Status(http.StatusNotFound)
}

// ─── Explain & Diff Endpoints ───────────────────────────────────────────────

// TestAPI_Explain_ValidSolution verifies the explain endpoint with a minimal solution.
func TestAPI_Explain_ValidSolution(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	solutionYAML := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-explain
  version: 1.0.0
  description: A test solution
  tags:
    - test
    - integration
spec:
  resolvers:
    myResolver:
      provider: write-new
      with:
        path: /tmp/test.txt
        content: hello
`

	obj := e.POST("/v1/explain").
		WithJSON(map[string]any{"solution": solutionYAML}).
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	obj.HasValue("name", "test-explain")
	obj.Value("description").String().IsEqual("A test solution")
	obj.Value("resolverCount").Number().IsEqual(1)
	obj.Value("resolvers").Array().ContainsAny("myResolver")
	obj.Value("tags").Array().ContainsAny("test", "integration")
}

// TestAPI_Explain_EmptySolution verifies explain rejects an empty solution string.
func TestAPI_Explain_EmptySolution(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	e.POST("/v1/explain").
		WithJSON(map[string]any{"solution": ""}).
		Expect().
		Status(http.StatusBadRequest)
}

// TestAPI_Explain_InvalidYAML verifies explain rejects malformed YAML.
func TestAPI_Explain_InvalidYAML(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	e.POST("/v1/explain").
		WithJSON(map[string]any{"solution": "{{not valid yaml}}"}).
		Expect().
		Status(http.StatusBadRequest)
}

// TestAPI_Diff_ValidSolutions verifies the diff endpoint with two different solutions.
func TestAPI_Diff_ValidSolutions(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	solA := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: sol-a
  version: 1.0.0
  description: First solution
`
	solB := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: sol-b
  version: 2.0.0
  description: Second solution
`

	obj := e.POST("/v1/diff").
		WithJSON(map[string]any{"solutionA": solA, "solutionB": solB}).
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	obj.Value("changes").Array()
	summary := obj.Value("summary").Object()
	summary.ContainsKey("total")
	summary.ContainsKey("added")
	summary.ContainsKey("removed")
	summary.ContainsKey("changed")
}

// TestAPI_Diff_IdenticalSolutions verifies diff returns no changes for identical solutions.
func TestAPI_Diff_IdenticalSolutions(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	sol := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: same-sol
  version: 1.0.0
`

	obj := e.POST("/v1/diff").
		WithJSON(map[string]any{"solutionA": sol, "solutionB": sol}).
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	obj.Value("summary").Object().Value("total").Number().IsEqual(0)
}

// TestAPI_Diff_MissingSolutionA verifies diff rejects when solutionA is empty.
func TestAPI_Diff_MissingSolutionA(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	e.POST("/v1/diff").
		WithJSON(map[string]any{
			"solutionA": "",
			"solutionB": "apiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: x\n  version: 1.0.0",
		}).
		Expect().
		Status(http.StatusBadRequest)
}

// TestAPI_Diff_MissingSolutionB verifies diff rejects when solutionB is empty.
func TestAPI_Diff_MissingSolutionB(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	e.POST("/v1/diff").
		WithJSON(map[string]any{
			"solutionA": "apiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: x\n  version: 1.0.0",
			"solutionB": "",
		}).
		Expect().
		Status(http.StatusBadRequest)
}

// TestAPI_Diff_InvalidYAML verifies diff rejects malformed YAML.
func TestAPI_Diff_InvalidYAML(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	e.POST("/v1/diff").
		WithJSON(map[string]any{
			"solutionA": "{{bad yaml}}",
			"solutionB": "apiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: x\n  version: 1.0.0",
		}).
		Expect().
		Status(http.StatusBadRequest)
}

// ─── Admin Endpoints ────────────────────────────────────────────────────────

// TestAPI_AdminReloadConfig verifies the admin reload-config endpoint returns 501 (not yet implemented).
func TestAPI_AdminReloadConfig(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	e.POST("/v1/admin/reload-config").
		Expect().
		Status(http.StatusNotImplemented)
}

// TestAPI_AdminClearCache verifies the admin clear-cache endpoint returns 501 (not yet implemented).
func TestAPI_AdminClearCache(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	e.POST("/v1/admin/clear-cache").
		Expect().
		Status(http.StatusNotImplemented)
}

// TestAPI_AdminInfo_ResponseFields verifies all fields in admin info response.
func TestAPI_AdminInfo_ResponseFields(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	obj := e.GET("/v1/admin/info").
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	obj.Value("version").String().NotEmpty()
	obj.Value("uptime").String().NotEmpty()
	obj.Value("startTime").String().NotEmpty()
	obj.Value("providers").Number().Ge(0)
}

// ─── Settings Endpoint ──────────────────────────────────────────────────────

// TestAPI_SettingsEndpoint verifies the settings endpoint returns runtime settings.
func TestAPI_SettingsEndpoint(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	obj := e.GET("/v1/settings").
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	obj.ContainsKey("noColor")
	obj.ContainsKey("quiet")
}

// ─── Error Response Structure ───────────────────────────────────────────────

// TestAPI_ErrorResponse_StructureOnNotFound verifies JSON error responses follow
// the standard RFC 7807 problem+json format.
func TestAPI_ErrorResponse_StructureOnNotFound(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL+"/nonexistent-path", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.Equal(t, "application/problem+json", resp.Header.Get("Content-Type"))

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `"status":404`)
	assert.Contains(t, bodyStr, `"title"`)
	assert.Contains(t, bodyStr, `"detail"`)
}

// TestAPI_ErrorResponse_StructureOnMethodNotAllowed verifies 405 responses.
func TestAPI_ErrorResponse_StructureOnMethodNotAllowed(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, ts.URL+"/health", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
	assert.Equal(t, "application/problem+json", resp.Header.Get("Content-Type"))

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `"status":405`)
	assert.Contains(t, bodyStr, `"title"`)
}

// TestAPI_ErrorResponse_StructureOnBadRequest verifies 400 error structure.
func TestAPI_ErrorResponse_StructureOnBadRequest(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, ts.URL+"/v1/eval/cel",
		strings.NewReader(`{"expression":"???invalid"}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, "application/problem+json", resp.Header.Get("Content-Type"))

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `"status":400`)
	assert.Contains(t, bodyStr, `"title"`)
	assert.Contains(t, bodyStr, `"detail"`)
}

// ─── Security Headers ───────────────────────────────────────────────────────

// TestAPI_SecurityHeaders verifies that security headers are set on versioned endpoints.
// The makeVersionedOnly middleware wrapper ensures headers are applied for /v1/* paths.
func TestAPI_SecurityHeaders(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL+"/v1/admin/info", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", resp.Header.Get("X-Frame-Options"))
	assert.NotEmpty(t, resp.Header.Get("Content-Security-Policy"))
}

// TestAPI_SecurityHeaders_NoHSTS_WhenTLSDisabled verifies HSTS is NOT set when TLS is off.
func TestAPI_SecurityHeaders_NoHSTS_WhenTLSDisabled(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL+"/v1/admin/info", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// TLS is not enabled in test server, so HSTS should be absent.
	assert.Empty(t, resp.Header.Get("Strict-Transport-Security"))
}

// ─── CORS Configuration ─────────────────────────────────────────────────────

// setupTestServerWithCORS creates a test server with CORS enabled.
func setupTestServerWithCORS(t *testing.T) *httptest.Server {
	t.Helper()

	cfg := &config.Config{
		APIServer: config.APIServerConfig{
			APIVersion: settings.DefaultAPIVersion,
			CORS: config.APICORSConfig{
				Enabled:        true,
				AllowedOrigins: []string{"https://example.com"},
				AllowedMethods: []string{"GET", "POST", "OPTIONS"},
				AllowedHeaders: []string{"Content-Type", "Authorization"},
				MaxAge:         3600,
			},
		},
	}

	srv, err := api.NewServer(
		api.WithServerConfig(cfg),
		api.WithServerVersion("test-cors"),
	)
	require.NoError(t, err)

	apiRouter, err := api.SetupMiddleware(t.Context(), srv.Router(), &cfg.APIServer, logr.Discard())
	require.NoError(t, err)
	srv.SetAPIRouter(apiRouter)
	srv.InitAPI()

	hctx := srv.HandlerCtx()
	endpoints.RegisterAll(srv.API(), srv.Router(), hctx)

	return httptest.NewServer(srv.Router())
}

// TestAPI_CORS_PreflightRequest verifies that CORS middleware sets Allow-Origin for permitted origins.
// The makeVersionedOnly wrapper ensures CORS middleware runs for /v1/* requests.
func TestAPI_CORS_PreflightRequest(t *testing.T) {
	ts := setupTestServerWithCORS(t)
	defer ts.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL+"/v1/admin/info", nil)
	require.NoError(t, err)
	req.Header.Set("Origin", "https://example.com")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "https://example.com", resp.Header.Get("Access-Control-Allow-Origin"))
}

// TestAPI_CORS_ServerRespondsDespiteDisallowedOrigin verifies that the server still
// returns a response for requests from unlisted origins. CORS does not block the
// server-side response; the browser enforces the policy via the absence of
// Access-Control-Allow-Origin on the response.
func TestAPI_CORS_ServerRespondsDespiteDisallowedOrigin(t *testing.T) {
	ts := setupTestServerWithCORS(t)
	defer ts.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL+"/v1/admin/info", nil)
	require.NoError(t, err)
	req.Header.Set("Origin", "https://evil.com")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Server responds successfully; browser-enforced CORS blocks client-side access.
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Empty(t, resp.Header.Get("Access-Control-Allow-Origin"))
}

// ─── Health Endpoint Deep Tests ─────────────────────────────────────────────

// TestAPI_HealthEndpoint_Components verifies health endpoint includes provider component status.
func TestAPI_HealthEndpoint_Components(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	obj := e.GET("/health").
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	components := obj.Value("components").Array()
	components.Length().Ge(1)

	// The first component should be "providers"
	components.Value(0).Object().Value("name").String().IsEqual("providers")
	components.Value(0).Object().Value("status").String().NotEmpty()
}

// TestAPI_RootEndpoint_Links verifies the root endpoint returns expected links.
func TestAPI_RootEndpoint_Links(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	obj := e.GET("/").
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	links := obj.Value("links").Array()
	links.Length().Ge(3)

	// Collect all link rels
	rels := make([]string, 0, int(links.Length().Raw()))
	for i := range links.Iter() {
		rels = append(rels, links.Value(i).Object().Value("rel").String().Raw())
	}
	assert.Contains(t, rels, "health")
	assert.Contains(t, rels, "providers")
	assert.Contains(t, rels, "catalogs")
}

// ─── Readiness During Shutdown ──────────────────────────────────────────────

// setupTestServerWithShutdown creates a test server in shutting-down state.
func setupTestServerWithShutdown(t *testing.T) *httptest.Server {
	t.Helper()

	cfg := &config.Config{
		APIServer: config.APIServerConfig{
			APIVersion: settings.DefaultAPIVersion,
		},
	}

	srv, err := api.NewServer(
		api.WithServerConfig(cfg),
		api.WithServerVersion("test-shutdown"),
	)
	require.NoError(t, err)

	apiRouter, err := api.SetupMiddleware(t.Context(), srv.Router(), &cfg.APIServer, logr.Discard())
	require.NoError(t, err)
	srv.SetAPIRouter(apiRouter)
	srv.InitAPI()

	hctx := srv.HandlerCtx()
	// Signal shutdown
	atomic.StoreInt32(hctx.IsShuttingDown, 1)

	endpoints.RegisterAll(srv.API(), srv.Router(), hctx)

	return httptest.NewServer(srv.Router())
}

// TestAPI_ReadinessProbe_DuringShutdown verifies readiness returns 503 during shutdown.
func TestAPI_ReadinessProbe_DuringShutdown(t *testing.T) {
	ts := setupTestServerWithShutdown(t)
	defer ts.Close()

	e := httpexpect.Default(t, ts.URL)

	e.GET("/health/ready").
		Expect().
		Status(http.StatusServiceUnavailable)
}

// TestAPI_HealthStatus_DuringShutdown verifies health reports shutting_down status.
func TestAPI_HealthStatus_DuringShutdown(t *testing.T) {
	ts := setupTestServerWithShutdown(t)
	defer ts.Close()

	e := httpexpect.Default(t, ts.URL)

	obj := e.GET("/health").
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	obj.HasValue("status", "shutting_down")
}

// TestAPI_LivenessProbe_DuringShutdown verifies liveness still returns 200 during shutdown.
func TestAPI_LivenessProbe_DuringShutdown(t *testing.T) {
	ts := setupTestServerWithShutdown(t)
	defer ts.Close()

	e := httpexpect.Default(t, ts.URL)

	e.GET("/health/live").
		Expect().
		Status(http.StatusOK).
		JSON().Object().
		HasValue("status", "ok")
}

// ─── Content-Type Validation ────────────────────────────────────────────────

// TestAPI_PostEndpoints_RequireJSON verifies POST endpoints reject non-JSON content.
func TestAPI_PostEndpoints_RequireJSON(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	paths := []struct {
		name string
		path string
	}{
		{"eval/cel", "/v1/eval/cel"},
		{"eval/template", "/v1/eval/template"},
		{"explain", "/v1/explain"},
		{"diff", "/v1/diff"},
		{"schemas/validate", "/v1/schemas/validate"},
		{"solutions/run", "/v1/solutions/run"},
		{"solutions/render", "/v1/solutions/render"},
		{"solutions/test", "/v1/solutions/test"},
		{"solutions/lint", "/v1/solutions/lint"},
		{"solutions/inspect", "/v1/solutions/inspect"},
		{"solutions/dryrun", "/v1/solutions/dryrun"},
	}

	for _, tt := range paths {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, ts.URL+tt.path, strings.NewReader("not json"))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "text/plain")

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			// Huma rejects non-JSON content types with 4xx
			assert.True(t, resp.StatusCode >= 400 && resp.StatusCode < 500,
				"expected 4xx for non-JSON content on %s, got %d", tt.path, resp.StatusCode)
		})
	}
}

// ─── Method Not Allowed for All Endpoints ───────────────────────────────────

// TestAPI_MethodNotAllowed_GETEndpoints verifies POST on GET-only endpoints
// returns an error. Chi root-level routes return 405, while Huma-registered
// routes on the API sub-router return 404 for unregistered method+path combos.
func TestAPI_MethodNotAllowed_GETEndpoints(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	tests := []struct {
		path string
		// Health endpoints are on root chi router → 405; API endpoints on Huma sub-router → 405 or 404
		expectedStatus int
	}{
		{"/health", http.StatusMethodNotAllowed},
		{"/health/live", http.StatusMethodNotAllowed},
		{"/health/ready", http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, ts.URL+tt.path, nil)
			require.NoError(t, err)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tt.expectedStatus, resp.StatusCode)
		})
	}
}

// TestAPI_MethodNotAllowed_POSTEndpoints verifies GET on POST-only endpoints
// returns an error (4xx). Huma sub-router returns 404 or 405 depending on routing.
func TestAPI_MethodNotAllowed_POSTEndpoints(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	postOnlyEndpoints := []string{
		"/v1/eval/cel",
		"/v1/eval/template",
		"/v1/explain",
		"/v1/diff",
		"/v1/schemas/validate",
		"/v1/admin/reload-config",
		"/v1/admin/clear-cache",
		"/v1/solutions/run",
		"/v1/solutions/render",
		"/v1/solutions/test",
		"/v1/solutions/lint",
		"/v1/solutions/inspect",
		"/v1/solutions/dryrun",
		"/v1/catalogs/sync",
	}

	for _, ep := range postOnlyEndpoints {
		t.Run(ep, func(t *testing.T) {
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL+ep, nil)
			require.NoError(t, err)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			// Huma returns 404 or 405 for wrong method on API sub-router
			assert.True(t, resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed,
				"expected 404 or 405 for GET on %s, got %d", ep, resp.StatusCode)
		})
	}
}

// ─── OpenAPI Endpoint Deep Tests ────────────────────────────────────────────

// TestAPI_OpenAPIEndpoint_ContainsPaths verifies OpenAPI spec includes expected paths.
func TestAPI_OpenAPIEndpoint_ContainsPaths(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	body := e.GET("/v1/openapi.json").
		Expect().
		Status(http.StatusOK).
		Body().Raw()

	expectedPaths := []string{
		"/v1/providers",
		"/v1/eval/cel",
		"/v1/eval/template",
		"/v1/schemas",
		"/v1/catalogs",
		"/v1/explain",
		"/v1/diff",
		"/v1/admin/info",
		"/v1/snapshots",
		"/v1/config",
		"/v1/settings",
		"/v1/solutions/run",
		"/v1/solutions/render",
		"/v1/solutions/lint",
		"/v1/solutions/inspect",
		"/v1/solutions/test",
		"/v1/solutions/dryrun",
		"/v1/catalogs/sync",
		"/health",
	}

	for _, p := range expectedPaths {
		assert.Contains(t, body, p, "OpenAPI spec should contain path %s", p)
	}
}

// TestAPI_OpenAPIEndpoint_Info verifies the OpenAPI spec info section.
func TestAPI_OpenAPIEndpoint_Info(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	body := e.GET("/v1/openapi.json").
		Expect().
		Status(http.StatusOK).
		Body().Raw()

	assert.Contains(t, body, `"openapi"`)
	assert.Contains(t, body, `"3.`)
	assert.Contains(t, body, `"scafctl API"`)
}

// ─── Config Endpoint ────────────────────────────────────────────────────────

// TestAPI_ConfigEndpoint_ResponseStructure verifies config response has expected sections.
func TestAPI_ConfigEndpoint_ResponseStructure(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	obj := e.GET("/v1/config").
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	obj.ContainsKey("apiServer")
}

// ─── Concurrent Request Stress Test ─────────────────────────────────────────

// TestAPI_ConcurrentRequests_MixedEndpoints verifies the server handles concurrent
// requests to different endpoints.
func TestAPI_ConcurrentRequests_MixedEndpoints(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	type req struct {
		method string
		path   string
		body   string
	}

	requests := []req{
		{http.MethodGet, "/health", ""},
		{http.MethodGet, "/health/live", ""},
		{http.MethodGet, "/health/ready", ""},
		{http.MethodGet, "/v1/providers", ""},
		{http.MethodGet, "/v1/schemas", ""},
		{http.MethodGet, "/v1/catalogs", ""},
		{http.MethodGet, "/v1/config", ""},
		{http.MethodGet, "/v1/admin/info", ""},
		{http.MethodPost, "/v1/eval/cel", `{"expression":"1+1"}`},
		{http.MethodPost, "/v1/eval/template", `{"template":"hello"}`},
	}

	const concurrentPerEndpoint = 5
	totalReqs := len(requests) * concurrentPerEndpoint
	results := make(chan int, totalReqs)

	for _, r := range requests {
		for range concurrentPerEndpoint {
			go func(r req) {
				var body *strings.Reader
				if r.body != "" {
					body = strings.NewReader(r.body)
				} else {
					body = strings.NewReader("")
				}
				httpReq, err := http.NewRequestWithContext(context.Background(), r.method, ts.URL+r.path, body)
				if err != nil {
					results <- 0
					return
				}
				if r.body != "" {
					httpReq.Header.Set("Content-Type", "application/json")
				}
				resp, err := http.DefaultClient.Do(httpReq)
				if err != nil {
					results <- 0
					return
				}
				defer resp.Body.Close()
				results <- resp.StatusCode
			}(r)
		}
	}

	for range totalReqs {
		status := <-results
		assert.True(t, status >= 200 && status < 500,
			"expected successful status code, got %d", status)
	}
}

// ─── Metrics Endpoint ───────────────────────────────────────────────────────

// TestAPI_MetricsEndpoint_ContainsGoMetrics verifies Prometheus output has Go runtime metrics.
func TestAPI_MetricsEndpoint_ContainsGoMetrics(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	body := e.GET("/metrics").
		Expect().
		Status(http.StatusOK).
		Body().Raw()

	assert.Contains(t, body, "go_goroutines")
	assert.Contains(t, body, "go_memstats")
}

// ─── Benchmark Tests ────────────────────────────────────────────────────────

// BenchmarkAPI_HealthLive benchmarks the liveness probe endpoint.
func BenchmarkAPI_HealthLive(b *testing.B) {
	ts := setupTestServer(b)
	defer ts.Close()

	client := &http.Client{}

	b.ResetTimer()
	for b.Loop() {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL+"/health/live", nil)
		if err != nil {
			b.Fatal(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			b.Fatal(err)
		}
		resp.Body.Close()
	}
}

// BenchmarkAPI_ProvidersGet benchmarks the provider list endpoint.
func BenchmarkAPI_ProvidersGet(b *testing.B) {
	ts := setupTestServer(b)
	defer ts.Close()

	client := &http.Client{}

	b.ResetTimer()
	for b.Loop() {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL+"/v1/providers", nil)
		if err != nil {
			b.Fatal(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			b.Fatal(err)
		}
		resp.Body.Close()
	}
}

// BenchmarkAPI_EvalCEL benchmarks the CEL evaluation endpoint.
func BenchmarkAPI_EvalCEL(b *testing.B) {
	ts := setupTestServer(b)
	defer ts.Close()

	client := &http.Client{}

	b.ResetTimer()
	for b.Loop() {
		body := strings.NewReader(`{"expression":"1+2"}`)
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, ts.URL+"/v1/eval/cel", body)
		if err != nil {
			b.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			b.Fatal(err)
		}
		resp.Body.Close()
	}
}

// ─── Solution Run/Render/Test Endpoints ─────────────────────────────────────

// TestAPI_SolutionRun_MissingPath verifies run endpoint rejects missing path.
func TestAPI_SolutionRun_MissingPath(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	e.POST("/v1/solutions/run").
		WithJSON(map[string]any{}).
		Expect().
		Status(http.StatusUnprocessableEntity)
}

// TestAPI_SolutionRun_InvalidPath verifies run endpoint rejects invalid path.
func TestAPI_SolutionRun_InvalidPath(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	e.POST("/v1/solutions/run").
		WithJSON(map[string]any{"path": "/nonexistent/solution.yaml"}).
		Expect().
		Status(http.StatusBadRequest)
}

// TestAPI_SolutionRender_MissingPath verifies render endpoint rejects missing path.
func TestAPI_SolutionRender_MissingPath(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	e.POST("/v1/solutions/render").
		WithJSON(map[string]any{}).
		Expect().
		Status(http.StatusUnprocessableEntity)
}

// TestAPI_SolutionRender_InvalidPath verifies render endpoint rejects invalid path.
func TestAPI_SolutionRender_InvalidPath(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	e.POST("/v1/solutions/render").
		WithJSON(map[string]any{"path": "/nonexistent/solution.yaml"}).
		Expect().
		Status(http.StatusBadRequest)
}

type mockPluginPool struct{}

func (m *mockPluginPool) EnsureAndAcquire(_ context.Context, deps []solution.PluginDependency) (func(), error) {
	return func() {}, nil
}

func (m *mockPluginPool) Shutdown() {
}

// TestAPI_SolutionRender_Calls exercises the spec.calls (parameterized calls)
// feature end-to-end through the render endpoint. It confirms that call
// definitions invoked from resolve steps bind their arguments -- defaults,
// call-site overrides, and resolver-sourced values -- and expose them to the
// underlying provider via the args namespace (_.args.x).
func TestAPI_SolutionRender_Calls(t *testing.T) {
	cfg := &config.Config{
		APIServer: config.APIServerConfig{
			APIVersion: settings.DefaultAPIVersion,
		},
	}

	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(celprovider.NewCelProvider()))
	require.NoError(t, reg.Register(staticprovider.New()))

	srv, err := api.NewServer(
		api.WithServerConfig(cfg),
		api.WithServerVersion("test-dev"),
		api.WithServerRegistry(reg),
		api.WithServerCompositeRegistry(provider.NewCompositeRegistryFromBase(reg)),
		api.WithServerPluginPool(&mockPluginPool{}),
	)
	require.NoError(t, err)

	apiRouter, err := api.SetupMiddleware(t.Context(), srv.Router(), &cfg.APIServer, logr.Discard())
	require.NoError(t, err)
	srv.SetAPIRouter(apiRouter)
	srv.InitAPI()
	endpoints.RegisterAll(srv.API(), srv.Router(), srv.HandlerCtx())

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()
	e := httpexpect.Default(t, ts.URL)

	content := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: api-calls-test
  version: 1.0.0
  description: Calls feature exercised over the API render endpoint
spec:
  calls:
    greet:
      provider: cel
      args:
        salutation:
          type: string
          default: Hello
        name:
          type: string
          required: true
      inputs:
        expression: '_.args.salutation + ", " + _.args.name + "!"'
  resolvers:
    english:
      type: string
      resolve:
        with:
          - call: greet
            args:
              name: World
    spanish:
      type: string
      resolve:
        with:
          - call: greet
            args:
              salutation: Hola
              name: Mundo
    requestor:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: Ada
    personalized:
      type: string
      resolve:
        with:
          - call: greet
            args:
              salutation: Hey
              name:
                expr: _.requestor
`

	solServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-yaml")
		_, _ = w.Write([]byte(content))
	}))
	defer solServer.Close()

	obj := e.POST("/v1/solutions/render").
		WithJSON(map[string]any{"path": solServer.URL + "/solution.yaml", "lockMode": "bestEffort"}).
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	data := obj.Value("resolverData").Object()
	data.HasValue("english", "Hello, World!")  // default salutation applied
	data.HasValue("spanish", "Hola, Mundo!")   // call-site override
	data.HasValue("requestor", "Ada")          // base value for the ref below
	data.HasValue("personalized", "Hey, Ada!") // arg sourced from a resolver ref
}

// TestAPI_DeferredValidation exercises the two-phase (deferred cross-resolver)
// validation feature over the API. Two resolvers validate against each other --
// one via a value reference (_.backupRegion) and one via a message-only
// reference ({{ .region }}). Before two-phase validation these mutual
// references formed a false ordering cycle. The render endpoint must now load
// and resolve them cleanly, and the lint endpoint must NOT report a
// resolver-cycle while still surfacing the informational
// deferred-validation-not-fail-fast advisory.
func TestAPI_DeferredValidation(t *testing.T) {
	cfg := &config.Config{
		APIServer: config.APIServerConfig{
			APIVersion: settings.DefaultAPIVersion,
		},
	}

	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(staticprovider.New()))
	require.NoError(t, reg.Register(validationprovider.NewValidationProvider()))

	srv, err := api.NewServer(
		api.WithServerConfig(cfg),
		api.WithServerVersion("test-dev"),
		api.WithServerRegistry(reg),
		api.WithServerCompositeRegistry(provider.NewCompositeRegistryFromBase(reg)),
		api.WithServerPluginPool(&mockPluginPool{}),
	)
	require.NoError(t, err)

	apiRouter, err := api.SetupMiddleware(t.Context(), srv.Router(), &cfg.APIServer, logr.Discard())
	require.NoError(t, err)
	srv.SetAPIRouter(apiRouter)
	srv.InitAPI()
	endpoints.RegisterAll(srv.API(), srv.Router(), srv.HandlerCtx())

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()
	e := httpexpect.Default(t, ts.URL)

	content := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: api-deferred-validation-test
  version: 1.0.0
  description: Two-phase cross-resolver validation exercised over the API
spec:
  resolvers:
    region:
      description: Primary deployment region
      resolve:
        with:
          - provider: static
            inputs:
              value: us-east1
      validate:
        with:
          # DEFERRED: references _.backupRegion -> runs after all resolvers.
          - provider: validation
            inputs:
              expression: "_.region != _.backupRegion"
              message: "tmpl: region must differ from backupRegion (both were {{ .region }})"
    backupRegion:
      description: Backup deployment region (message-only cross-reference)
      resolve:
        with:
          - provider: static
            inputs:
              value: us-west1
      validate:
        with:
          # DEFERRED via message-only reference to _.region.
          - provider: validation
            inputs:
              expression: "__self != ''"
              message: "tmpl: backupRegion must differ from region {{ .region }}"
`

	solServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-yaml")
		_, _ = w.Write([]byte(content))
	}))
	defer solServer.Close()
	solURL := solServer.URL + "/solution.yaml"

	// Render: mutual cross-resolver validation loads without a cycle error and
	// deferred rules pass because the regions differ.
	renderObj := e.POST("/v1/solutions/render").
		WithJSON(map[string]any{"path": solURL, "lockMode": "bestEffort"}).
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	data := renderObj.Value("resolverData").Object()
	data.HasValue("region", "us-east1")
	data.HasValue("backupRegion", "us-west1")

	// Lint: cross-resolver validation references must NOT trigger resolver-cycle,
	// and each deferred rule emits the info-level advisory.
	lintObj := e.POST("/v1/solutions/lint").
		WithJSON(map[string]any{"path": solURL}).
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	findings := lintObj.Value("findings").Array()
	rules := map[string]int{}
	for _, f := range findings.Iter() {
		rules[f.Object().Value("ruleName").String().Raw()]++
	}
	assert.Zero(t, rules["resolver-cycle"], "cross-resolver validation must not trigger resolver-cycle")
	assert.GreaterOrEqual(t, rules["deferred-validation-not-fail-fast"], 1,
		"deferred validation should emit the info advisory")
}

// TestAPI_LintParameterNumericMatches exercises the parameter-numeric-matches
// lint rule over the API. A resolver reads a numeric 'parameter' default with
// no explicit 'type' yet validates the value with matches(); automatic
// inference would coerce it to an integer (no matches() method) and fail at
// runtime. The lint endpoint must surface the warning while the sibling
// resolver that forces 'type: string' must not.
func TestAPI_LintParameterNumericMatches(t *testing.T) {
	cfg := &config.Config{
		APIServer: config.APIServerConfig{
			APIVersion: settings.DefaultAPIVersion,
		},
	}

	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(parameterprovider.NewParameterProvider()))
	require.NoError(t, reg.Register(validationprovider.NewValidationProvider()))

	srv, err := api.NewServer(
		api.WithServerConfig(cfg),
		api.WithServerVersion("test-dev"),
		api.WithServerRegistry(reg),
	)
	require.NoError(t, err)

	apiRouter, err := api.SetupMiddleware(t.Context(), srv.Router(), &cfg.APIServer, logr.Discard())
	require.NoError(t, err)
	srv.SetAPIRouter(apiRouter)
	srv.InitAPI()
	endpoints.RegisterAll(srv.API(), srv.Router(), srv.HandlerCtx())

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()
	e := httpexpect.Default(t, ts.URL)

	content := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: api-parameter-numeric-matches-test
  version: 1.0.0
  description: numeric parameter default validated with matches()
spec:
  resolvers:
    version:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: version
              default: 1
      validate:
        with:
          - provider: validation
            inputs:
              expression: '__self.matches("^[0-9]+$")'
              message: "version must be numeric"
    safeVersion:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: safeVersion
              type: string
              default: 1
      validate:
        with:
          - provider: validation
            inputs:
              expression: '__self.matches("^[0-9]+$")'
              message: "safeVersion must be numeric"
`

	solServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-yaml")
		_, _ = w.Write([]byte(content))
	}))
	defer solServer.Close()
	solURL := solServer.URL + "/solution.yaml"

	lintObj := e.POST("/v1/solutions/lint").
		WithJSON(map[string]any{"path": solURL}).
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	findings := lintObj.Value("findings").Array()
	matchesFindings := 0
	for _, f := range findings.Iter() {
		obj := f.Object()
		if obj.Value("ruleName").String().Raw() != "parameter-numeric-matches" {
			continue
		}
		matchesFindings++
		// Only the 'version' resolver (no explicit type) should be flagged.
		assert.Equal(t, "resolvers.version", obj.Value("location").String().Raw(),
			"the numeric-default resolver should be flagged")
	}
	assert.Equal(t, 1, matchesFindings,
		"exactly one resolver should trigger parameter-numeric-matches")
}

func TestAPI_SolutionTest_MissingPath(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	e.POST("/v1/solutions/test").
		WithJSON(map[string]any{}).
		Expect().
		Status(http.StatusUnprocessableEntity)
}

// TestAPI_SolutionTest_InvalidPath verifies test endpoint rejects invalid path.
func TestAPI_SolutionTest_InvalidPath(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	e.POST("/v1/solutions/test").
		WithJSON(map[string]any{"path": "/nonexistent/solution.yaml"}).
		Expect().
		Status(http.StatusBadRequest)
}

// TestAPI_SolutionInspect_MissingPath verifies inspect endpoint rejects missing path.
func TestAPI_SolutionInspect_MissingPath(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	e.POST("/v1/solutions/inspect").
		WithJSON(map[string]any{}).
		Expect().
		Status(http.StatusUnprocessableEntity)
}

// ─── Catalog Solutions & Sync Endpoints ─────────────────────────────────────

// TestAPI_CatalogSolutions_NotFound verifies 404 for solutions of a nonexistent catalog.
func TestAPI_CatalogSolutions_NotFound(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	e.GET("/v1/catalogs/nonexistent/solutions").
		Expect().
		Status(http.StatusNotFound)
}

// TestAPI_CatalogSync verifies the catalog sync endpoint.
func TestAPI_CatalogSync(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	obj := e.POST("/v1/catalogs/sync").
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	obj.Value("success").Boolean().IsTrue()
	obj.Value("message").String().NotEmpty()
	obj.Value("catalogs").Number().Ge(0)
}

// TestAPI_OpenAPIEndpoint_ContainsNewPaths verifies OpenAPI spec includes new endpoint paths.
func TestAPI_OpenAPIEndpoint_ContainsNewPaths(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	body := e.GET("/v1/openapi.json").
		Expect().
		Status(http.StatusOK).
		Body().Raw()

	newPaths := []string{
		"/v1/solutions/run",
		"/v1/solutions/render",
		"/v1/solutions/test",
		"/v1/solutions/inspect",
		"/v1/catalogs/sync",
	}

	for _, p := range newPaths {
		assert.Contains(t, body, p, "OpenAPI spec should contain path %s", p)
	}
}

// ─── Operation Defaults Tests ───────────────────────────────────────────────

// TestAPI_OpenAPISpec_AuthenticatedEndpointsHaveSecurity verifies that
// authenticated endpoints have the oauth2 security requirement in the OpenAPI spec.
func TestAPI_OpenAPISpec_AuthenticatedEndpointsHaveSecurity(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	body := e.GET("/v1/openapi.json").
		Expect().
		Status(http.StatusOK).
		Body().Raw()

	var spec map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &spec))

	paths, ok := spec["paths"].(map[string]any)
	require.True(t, ok, "expected paths in OpenAPI spec")

	authenticatedPaths := []struct {
		path   string
		method string
	}{
		{"/v1/admin/info", "get"},
		{"/v1/admin/reload-config", "post"},
		{"/v1/admin/clear-cache", "post"},
		{"/v1/providers", "get"},
		{"/v1/eval/cel", "post"},
		{"/v1/config", "get"},
	}

	for _, ep := range authenticatedPaths {
		pathObj, exists := paths[ep.path].(map[string]any)
		if !exists {
			t.Errorf("path %s not found in OpenAPI spec", ep.path)
			continue
		}
		methodObj, exists := pathObj[ep.method].(map[string]any)
		if !exists {
			t.Errorf("method %s not found for path %s", ep.method, ep.path)
			continue
		}
		security, hasSec := methodObj["security"]
		assert.True(t, hasSec, "expected security on %s %s", ep.method, ep.path)
		if hasSec {
			secList, ok := security.([]any)
			assert.True(t, ok && len(secList) > 0, "expected non-empty security on %s %s", ep.method, ep.path)
		}
	}
}

// TestAPI_OpenAPISpec_HealthEndpointsPublic verifies that health/probe endpoints
// have empty security (publicly accessible) in the OpenAPI spec.
func TestAPI_OpenAPISpec_HealthEndpointsPublic(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	body := e.GET("/v1/openapi.json").
		Expect().
		Status(http.StatusOK).
		Body().Raw()

	var spec map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &spec))

	paths, ok := spec["paths"].(map[string]any)
	require.True(t, ok, "expected paths in OpenAPI spec")

	publicPaths := []string{"/", "/health", "/health/live", "/health/ready"}

	for _, p := range publicPaths {
		pathObj, exists := paths[p].(map[string]any)
		if !exists {
			t.Errorf("path %s not found in OpenAPI spec", p)
			continue
		}
		getObj, exists := pathObj["get"].(map[string]any)
		if !exists {
			t.Errorf("GET method not found for path %s", p)
			continue
		}
		security, hasSec := getObj["security"]
		if hasSec {
			secList, ok := security.([]any)
			if ok && len(secList) > 0 {
				// Each item should be an empty object {}
				for _, item := range secList {
					secMap, mapOK := item.(map[string]any)
					assert.True(t, mapOK && len(secMap) == 0,
						"expected empty security object for public path %s, got %v", p, item)
				}
			}
		}
	}
}

// TestAPI_OpenAPISpec_ErrorCodesPresent verifies that authenticated endpoints
// document error response codes in the OpenAPI spec.
func TestAPI_OpenAPISpec_ErrorCodesPresent(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	body := e.GET("/v1/openapi.json").
		Expect().
		Status(http.StatusOK).
		Body().Raw()

	var spec map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &spec))

	paths, ok := spec["paths"].(map[string]any)
	require.True(t, ok)

	// Check that admin-info (GET) has 401, 403, 500 in responses
	adminInfo := paths["/v1/admin/info"].(map[string]any)["get"].(map[string]any)
	responses, ok := adminInfo["responses"].(map[string]any)
	require.True(t, ok, "expected responses in admin-info")

	expectedCodes := []string{"401", "403", "500"}
	for _, code := range expectedCodes {
		_, exists := responses[code]
		assert.True(t, exists, "expected %s response in admin-info endpoint", code)
	}
}

// setupTestServerWithPool creates a test server with a versioned plugin pool configured.
func setupTestServerWithPool(t testing.TB, poolOpts ...plugin.Option) *httptest.Server {
	t.Helper()

	cfg := &config.Config{
		APIServer: config.APIServerConfig{
			APIVersion: settings.DefaultAPIVersion,
		},
	}

	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(messageprovider.NewMessageProvider()))
	require.NoError(t, reg.Register(fileprovider.NewFileProvider()))

	compositeReg := provider.NewCompositeRegistryFromBase(reg)

	mockCat := catalog.NewMockCatalog("test-catalog",
		catalog.WithResolveFunc(func(_ context.Context, ref catalog.Reference) (catalog.ArtifactInfo, error) {
			ref.Version = semver.MustParse("1.0.0")
			return catalog.ArtifactInfo{
				Reference: ref,
				Catalog:   "test-catalog",
			}, nil
		}),
	)
	fetcher := plugin.NewFetcher(plugin.FetcherConfig{
		Catalog:  mockCat,
		Cache:    plugin.NewCache(t.TempDir()),
		Platform: plugin.CurrentPlatform(),
		Logger:   logr.Discard(),
	})

	pool := plugin.NewVersionPool(context.Background(), fetcher, compositeReg, poolOpts...)
	t.Cleanup(pool.Shutdown)

	srv, err := api.NewServer(
		api.WithServerConfig(cfg),
		api.WithServerVersion("test-dev"),
		api.WithServerRegistry(reg),
		api.WithServerCompositeRegistry(compositeReg),
		api.WithServerPluginPool(pool),
		api.WithServerPluginFetcher(fetcher),
	)
	require.NoError(t, err)

	apiRouter, err := api.SetupMiddleware(t.Context(), srv.Router(), &cfg.APIServer, logr.Discard())
	require.NoError(t, err)
	srv.SetAPIRouter(apiRouter)
	srv.InitAPI()

	hctx := srv.HandlerCtx()
	endpoints.RegisterAll(srv.API(), srv.Router(), hctx)

	return httptest.NewServer(srv.Router())
}

// serveSolutionWithPlugins creates an HTTP server that serves a solution YAML
// referencing external plugins. Returns the URL to the solution.
func serveSolutionWithPlugins(t *testing.T) string {
	t.Helper()
	content := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: plugin-test
  description: Test solution with external plugin dependency
spec:
  resolvers:
    greeting:
      description: A greeting
      type: string
      resolve:
        with:
          - provider: external-plugin
            inputs:
              message: hello
bundle:
  plugins:
    - name: external-plugin
      kind: provider
`
	solServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-yaml")
		_, _ = w.Write([]byte(content))
	}))
	t.Cleanup(solServer.Close)
	return solServer.URL + "/solution.yaml"
}

// TestAPI_SolutionDryRun_PluginExternalDisabled verifies that the dryrun
// endpoint returns 403 when external plugins are disabled.
func TestAPI_SolutionDryRun_PluginExternalDisabled(t *testing.T) {
	ts := setupTestServerWithPool(t, plugin.WithVersionPoolIdleTimeout(0), plugin.WithVersionPoolDisableExternal(true))
	defer ts.Close()
	e := httpexpect.Default(t, ts.URL)

	solURL := serveSolutionWithPlugins(t)

	e.POST("/v1/solutions/dryrun").
		WithJSON(map[string]any{"path": solURL, "lockMode": "bestEffort"}).
		Expect().
		Status(http.StatusForbidden).
		HasContentType("application/problem+json").
		Body().Contains("external plugins disabled")
}

// TestAPI_SolutionRun_PluginNotAllowed verifies that the run endpoint
// returns 403 when a plugin is not on the allowlist.
func TestAPI_SolutionRun_PluginNotAllowed(t *testing.T) {
	ts := setupTestServerWithPool(t,
		plugin.WithVersionPoolIdleTimeout(0),
		plugin.WithVersionPoolAllowedPlugins(map[string]catalog.PluginPolicy{
			"test-catalog": {Plugins: []string{"only-this-one"}},
		}),
	)
	defer ts.Close()
	e := httpexpect.Default(t, ts.URL)

	solURL := serveSolutionWithPlugins(t)

	e.POST("/v1/solutions/run").
		WithJSON(map[string]any{"path": solURL, "lockMode": "bestEffort"}).
		Expect().
		Status(http.StatusForbidden).
		HasContentType("application/problem+json").
		Body().Contains("not in allowlist")
}

// TestAPI_SolutionRender_PluginPoolFull verifies that the render endpoint
// returns 503 when the plugin pool is at capacity.
func TestAPI_SolutionRender_PluginPoolFull(t *testing.T) {
	cfg := &config.Config{
		APIServer: config.APIServerConfig{
			APIVersion: settings.DefaultAPIVersion,
		},
	}
	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(messageprovider.NewMessageProvider()))

	compositeReg := provider.NewCompositeRegistryFromBase(reg)

	mockCat := catalog.NewMockCatalog("test-catalog",
		catalog.WithResolveFunc(func(_ context.Context, ref catalog.Reference) (catalog.ArtifactInfo, error) {
			ref.Version = semver.MustParse("1.0.0")
			return catalog.ArtifactInfo{
				Reference: ref,
				Catalog:   "test-catalog",
			}, nil
		}),
	)
	fetcher := plugin.NewFetcher(plugin.FetcherConfig{
		Catalog:  mockCat,
		Cache:    plugin.NewCache(t.TempDir()),
		Platform: plugin.CurrentPlatform(),
		Logger:   logr.Discard(),
	})

	pool := plugin.NewVersionPool(context.Background(), fetcher, compositeReg,
		plugin.WithVersionPoolIdleTimeout(0),
		plugin.WithVersionPoolMaxPlugins(1),
	)
	t.Cleanup(pool.Shutdown)

	// Adopt a filler entry to fill the single slot
	fillerDep := solution.PluginDependency{
		Name:    "filler-plugin",
		Kind:    solution.PluginKindProvider,
		Catalog: "test-catalog",
	}
	pool.Adopt(
		identity.NewPluginIdentity("filler-plugin", "test-catalog"),
		semver.MustParse("1.0.0"),
		nil,
		fillerDep,
		nil,
	)

	srv, err := api.NewServer(
		api.WithServerConfig(cfg),
		api.WithServerVersion("test-dev"),
		api.WithServerRegistry(reg),
		api.WithServerCompositeRegistry(compositeReg),
		api.WithServerPluginPool(pool),
		api.WithServerPluginFetcher(fetcher),
	)
	require.NoError(t, err)

	apiRouter, err := api.SetupMiddleware(t.Context(), srv.Router(), &cfg.APIServer, logr.Discard())
	require.NoError(t, err)
	srv.SetAPIRouter(apiRouter)
	srv.InitAPI()
	endpoints.RegisterAll(srv.API(), srv.Router(), srv.HandlerCtx())

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()
	e := httpexpect.Default(t, ts.URL)

	solURL := serveSolutionWithPlugins(t)

	e.POST("/v1/solutions/render").
		WithJSON(map[string]any{"path": solURL, "lockMode": "bestEffort"}).
		Expect().
		Status(http.StatusServiceUnavailable)
}

// TestAPI_SolutionRun_AllowedCatalogs_Rejected verifies that the run endpoint
// rejects a plugin when the fetcher's allowedCatalogs blocks it end-to-end.
func TestAPI_SolutionRun_AllowedCatalogs_Rejected(t *testing.T) {
	// Create a pre-populated plugin cache so the fetcher takes the cache path
	// (which now validates allowedCatalogs).
	cacheDir := t.TempDir()
	platform := plugin.CurrentPlatform()
	pluginName := "external-plugin"
	pluginBinDir := cacheDir + "/" + pluginName + "/1.0.0/" + plugin.PlatformCacheKey(platform)
	require.NoError(t, os.MkdirAll(pluginBinDir, 0o755))
	// Create a fake executable so the cache recognizes it.
	require.NoError(t, os.WriteFile(pluginBinDir+"/"+pluginName, []byte("#!/bin/sh\n"), 0o755))

	fetcher := plugin.NewFetcher(plugin.FetcherConfig{
		Catalog: catalog.NewMockCatalog("untrusted-registry",
			catalog.WithResolveFunc(func(_ context.Context, ref catalog.Reference) (catalog.ArtifactInfo, error) {
				ref.Version = semver.MustParse("1.0.0")
				return catalog.ArtifactInfo{
					Reference: ref,
					Catalog:   "untrusted-registry",
				}, nil
			}),
		),
		Cache:           plugin.NewCache(cacheDir),
		Platform:        platform,
		AllowedCatalogs: []string{"approved-only"},
		Logger:          logr.Discard(),
	})

	cfg := &config.Config{
		APIServer: config.APIServerConfig{
			APIVersion: settings.DefaultAPIVersion,
		},
	}
	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(messageprovider.NewMessageProvider()))

	compositeReg := provider.NewCompositeRegistryFromBase(reg)

	pool := plugin.NewVersionPool(context.Background(), fetcher, compositeReg, plugin.WithVersionPoolIdleTimeout(0))
	t.Cleanup(pool.Shutdown)

	srv, err := api.NewServer(
		api.WithServerConfig(cfg),
		api.WithServerVersion("test-dev"),
		api.WithServerRegistry(reg),
		api.WithServerCompositeRegistry(compositeReg),
		api.WithServerPluginPool(pool),
		api.WithServerPluginFetcher(fetcher),
	)
	require.NoError(t, err)

	apiRouter, err := api.SetupMiddleware(t.Context(), srv.Router(), &cfg.APIServer, logr.Discard())
	require.NoError(t, err)
	srv.SetAPIRouter(apiRouter)
	srv.InitAPI()
	endpoints.RegisterAll(srv.API(), srv.Router(), srv.HandlerCtx())

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()
	e := httpexpect.Default(t, ts.URL)

	solURL := serveSolutionWithPlugins(t)

	e.POST("/v1/solutions/run").
		WithJSON(map[string]any{"path": solURL, "lockMode": "bestEffort"}).
		Expect().
		Status(http.StatusBadGateway).
		HasContentType("application/problem+json").
		Body().Contains("is not in the allowed catalogs list")
}

// ─── LockMode ───────────────────────────────────────────────────────────────

// serveBuiltinSolutionYAML serves an arbitrary solution YAML body over HTTP and
// returns the URL. The API server only accepts remote paths, so tests that need
// to exercise solution loading serve the document this way.
func serveBuiltinSolutionYAML(t *testing.T, content string) string {
	t.Helper()
	solServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-yaml")
		_, _ = w.Write([]byte(content))
	}))
	t.Cleanup(solServer.Close)
	return solServer.URL + "/solution.yaml"
}

// lockModeEndpoint describes one solution endpoint that accepts the lockMode
// field. run, render, and dryrun all share the same
// parseLockMode -> LoadSolutionWithLock -> ensureProviderDependencies ->
// handleEnsureError pipeline, so the lock-mode policy tests exercise every
// endpoint from a single table instead of duplicating per-endpoint cases.
// assertGreetingOK validates the endpoint-specific 200 body for the builtin
// "greeting" solution used by the success cases (run/render expose resolverData
// directly; dryrun returns a WhatIf report).
type lockModeEndpoint struct {
	name             string
	path             string
	assertGreetingOK func(*httpexpect.Object)
}

func lockModeEndpoints() []lockModeEndpoint {
	assertResolverData := func(o *httpexpect.Object) {
		o.Value("resolverData").Object().HasValue("greeting", "hello")
	}
	return []lockModeEndpoint{
		{name: "run", path: "/v1/solutions/run", assertGreetingOK: assertResolverData},
		{name: "render", path: "/v1/solutions/render", assertGreetingOK: assertResolverData},
		{
			name: "dryrun",
			path: "/v1/solutions/dryrun",
			assertGreetingOK: func(o *httpexpect.Object) {
				o.HasValue("dryRun", true)
				o.Value("solution").String().NotEmpty()
			},
		},
	}
}

// TestAPI_SolutionLockMode_InvalidValue verifies that an unrecognised lockMode
// is rejected by every endpoint that accepts the field (run, render, dryrun).
// The field carries an `enum` schema constraint, so Huma rejects the value at
// request validation with 422 before the handler runs.
func TestAPI_SolutionLockMode_InvalidValue(t *testing.T) {
	e, ts := setupExpect(t)
	defer ts.Close()

	// Any valid remote path passes requireRemotePath; enum validation fails first.
	solURL := serveBuiltinSolutionYAML(t, `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: lockmode-invalid
  version: 1.0.0
`)

	for _, ep := range lockModeEndpoints() {
		t.Run(ep.name, func(t *testing.T) {
			e.POST(ep.path).
				WithJSON(map[string]any{"path": solURL, "lockMode": "bogus"}).
				Expect().
				Status(http.StatusUnprocessableEntity).
				HasContentType("application/problem+json")
		})
	}
}

// TestAPI_SolutionLockMode_RequiresLockFile verifies that a solution referencing
// an external provider with no lock file is rejected by strict, the default
// (empty), and constrained modes. The empty case proves the default is strict
// rather than bestEffort. The missing-lock condition surfaces from dependency
// resolution as prepare.ErrMissingLockFile and is mapped to 400 at the API
// boundary, before any plugin is fetched.
func TestAPI_SolutionLockMode_RequiresLockFile(t *testing.T) {
	ts := setupTestServerWithPool(t, plugin.WithVersionPoolIdleTimeout(0))
	defer ts.Close()
	e := httpexpect.Default(t, ts.URL)

	solURL := serveSolutionWithPlugins(t)

	modes := []struct {
		name        string
		lockMode    string
		wantModeMsg string
	}{
		{name: "strict", lockMode: "strict", wantModeMsg: "strict mode requires a lock file"},
		{name: "default", lockMode: "", wantModeMsg: "strict mode requires a lock file"},
		{name: "constrained", lockMode: "constrained", wantModeMsg: "constrained mode requires a lock file"},
	}

	for _, ep := range lockModeEndpoints() {
		for _, tt := range modes {
			t.Run(ep.name+"/"+tt.name, func(t *testing.T) {
				body := map[string]any{"path": solURL}
				if tt.lockMode != "" {
					body["lockMode"] = tt.lockMode
				}

				resp := e.POST(ep.path).
					WithJSON(body).
					Expect().
					Status(http.StatusBadRequest).
					HasContentType("application/problem+json").
					Body()

				resp.Contains("requires a lock file but none was provided")
				resp.Contains(tt.wantModeMsg)
			})
		}
	}
}

// TestAPI_SolutionLockMode_BestEffortSkipsLockGuard verifies that bestEffort
// does NOT require a lock file: the same external-provider solution that strict
// rejects with 400 is accepted past lock resolution under bestEffort, failing
// later at plugin resolution (here, 403 because external plugins are disabled).
// The differing status codes prove the requirement is mode-dependent.
func TestAPI_SolutionLockMode_BestEffortSkipsLockGuard(t *testing.T) {
	ts := setupTestServerWithPool(t, plugin.WithVersionPoolIdleTimeout(0), plugin.WithVersionPoolDisableExternal(true))
	defer ts.Close()
	e := httpexpect.Default(t, ts.URL)

	solURL := serveSolutionWithPlugins(t)

	for _, ep := range lockModeEndpoints() {
		t.Run(ep.name, func(t *testing.T) {
			// strict: rejected because no lock file is present.
			e.POST(ep.path).
				WithJSON(map[string]any{"path": solURL, "lockMode": "strict"}).
				Expect().
				Status(http.StatusBadRequest).
				HasContentType("application/problem+json").
				Body().Contains("requires a lock file but none was provided")

			// bestEffort: passes lock resolution, then fails downstream at plugin resolution.
			e.POST(ep.path).
				WithJSON(map[string]any{"path": solURL, "lockMode": "bestEffort"}).
				Expect().
				Status(http.StatusForbidden).
				HasContentType("application/problem+json").
				Body().Contains("external plugins disabled")
		})
	}
}

// setupStaticProviderServer starts an API test server whose only builtin
// provider is `static`, with a composite registry wired so solution execution
// endpoints can resolve dependencies. Returns the running server.
func setupStaticProviderServer(t *testing.T) *httptest.Server {
	t.Helper()

	cfg := &config.Config{
		APIServer: config.APIServerConfig{
			APIVersion: settings.DefaultAPIVersion,
		},
	}

	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(staticprovider.New()))

	srv, err := api.NewServer(
		api.WithServerConfig(cfg),
		api.WithServerVersion("test-dev"),
		api.WithServerRegistry(reg),
		api.WithServerCompositeRegistry(provider.NewCompositeRegistryFromBase(reg)),
	)
	require.NoError(t, err)

	apiRouter, err := api.SetupMiddleware(t.Context(), srv.Router(), &cfg.APIServer, logr.Discard())
	require.NoError(t, err)
	srv.SetAPIRouter(apiRouter)
	srv.InitAPI()
	endpoints.RegisterAll(srv.API(), srv.Router(), srv.HandlerCtx())

	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)
	return ts
}

// TestAPI_SolutionLockMode_BuiltinOnlyStrictSucceeds verifies that a
// plugin-free (builtin-only) solution renders successfully under strict/default
// mode: the missing-lock requirement only applies when a referenced external
// provider actually needs a lock entry.
func TestAPI_SolutionLockMode_BuiltinOnlyStrictSucceeds(t *testing.T) {
	ts := setupStaticProviderServer(t)
	e := httpexpect.Default(t, ts.URL)

	solURL := serveBuiltinSolutionYAML(t, `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: lockmode-builtin-only
  version: 1.0.0
spec:
  resolvers:
    greeting:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
`)

	// Default lockMode (strict) must not require a lock for a plugin-free
	// solution.
	for _, ep := range lockModeEndpoints() {
		t.Run(ep.name, func(t *testing.T) {
			obj := e.POST(ep.path).
				WithJSON(map[string]any{"path": solURL}).
				Expect().
				Status(http.StatusOK).
				JSON().Object()

			ep.assertGreetingOK(obj)
		})
	}
}

// TestAPI_SolutionLockMode_DeclaredButUnusedPluginStrictSucceeds verifies that
// merely declaring a plugin in bundle.plugins does NOT require a lock file under
// strict mode when that plugin is not actually referenced by any resolver. The
// requirement is keyed on used external providers, not declarations, so a
// solution that only exercises a builtin provider renders successfully even
// though it declares an unused external plugin.
func TestAPI_SolutionLockMode_DeclaredButUnusedPluginStrictSucceeds(t *testing.T) {
	ts := setupStaticProviderServer(t)
	e := httpexpect.Default(t, ts.URL)

	solURL := serveBuiltinSolutionYAML(t, `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: lockmode-unused-plugin
  version: 1.0.0
spec:
  resolvers:
    greeting:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
bundle:
  plugins:
    - name: external-plugin
      kind: provider
`)

	for _, ep := range lockModeEndpoints() {
		t.Run(ep.name, func(t *testing.T) {
			obj := e.POST(ep.path).
				WithJSON(map[string]any{"path": solURL, "lockMode": "strict"}).
				Expect().
				Status(http.StatusOK).
				JSON().Object()

			ep.assertGreetingOK(obj)
		})
	}
}
