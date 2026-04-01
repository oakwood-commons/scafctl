// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package endpoints

import (
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExampleJSON_LoadsKnownFile(t *testing.T) {
	data := exampleJSON("root-response")
	require.NotNil(t, data, "root-response.json should be embedded")

	m, ok := data.(map[string]any)
	require.True(t, ok, "root-response should be a JSON object")
	assert.Equal(t, "scafctl API", m["name"])
}

func TestExampleJSON_ReturnsNilForMissing(t *testing.T) {
	data := exampleJSON("does-not-exist")
	assert.Nil(t, data)
}

func TestAddExamples_GETEndpoint_SetsResponseOnly(t *testing.T) {
	op := addExamples(huma.Operation{
		OperationID: "root",
		Method:      http.MethodGet,
	}, http.StatusOK)

	assert.Nil(t, op.RequestBody, "GET endpoints should not have request body examples")
	require.NotNil(t, op.Responses)
	require.NotNil(t, op.Responses["200"])
	require.NotNil(t, op.Responses["200"].Content["application/json"])
	assert.NotNil(t, op.Responses["200"].Content["application/json"].Examples["default"])
}

func TestAddExamples_POSTEndpoint_SetsBothRequestAndResponse(t *testing.T) {
	op := addExamples(huma.Operation{
		OperationID: "eval-cel",
		Method:      http.MethodPost,
	}, http.StatusOK)

	require.NotNil(t, op.RequestBody, "POST endpoints should have request body examples")
	require.NotNil(t, op.RequestBody.Content["application/json"])
	assert.NotNil(t, op.RequestBody.Content["application/json"].Examples["default"])

	require.NotNil(t, op.Responses["200"])
	assert.NotNil(t, op.Responses["200"].Content["application/json"].Examples["default"])
}

func TestAddExamples_MissingFiles_NoOp(t *testing.T) {
	op := addExamples(huma.Operation{
		OperationID: "nonexistent-operation",
		Method:      http.MethodGet,
	}, http.StatusOK)

	assert.Nil(t, op.RequestBody)
	assert.Nil(t, op.Responses)
}

func TestAddExamples_AllOperationsHaveResponseExamples(t *testing.T) {
	operationIDs := []struct {
		id     string
		method string
	}{
		{"root", http.MethodGet},
		{"health", http.MethodGet},
		{"health-live", http.MethodGet},
		{"health-ready", http.MethodGet},
		{"list-providers", http.MethodGet},
		{"get-provider", http.MethodGet},
		{"get-provider-schema", http.MethodGet},
		{"eval-cel", http.MethodPost},
		{"eval-template", http.MethodPost},
		{"list-catalogs", http.MethodGet},
		{"get-catalog", http.MethodGet},
		{"list-catalog-solutions", http.MethodGet},
		{"sync-catalogs", http.MethodPost},
		{"list-schemas", http.MethodGet},
		{"get-schema", http.MethodGet},
		{"validate-schema", http.MethodPost},
		{"explain-solution", http.MethodPost},
		{"diff-solutions", http.MethodPost},
		{"solution-lint", http.MethodPost},
		{"solution-inspect", http.MethodPost},
		{"solution-dryrun", http.MethodPost},
		{"solution-run", http.MethodPost},
		{"solution-render", http.MethodPost},
		{"solution-test", http.MethodPost},
		{"admin-info", http.MethodGet},
		{"admin-reload-config", http.MethodPost},
		{"admin-clear-cache", http.MethodPost},
		{"get-config", http.MethodGet},
		{"get-settings", http.MethodGet},
		{"list-snapshots", http.MethodGet},
		{"get-snapshot", http.MethodGet},
	}

	for _, tt := range operationIDs {
		t.Run(tt.id, func(t *testing.T) {
			op := addExamples(huma.Operation{
				OperationID: tt.id,
				Method:      tt.method,
			}, http.StatusOK)

			require.NotNil(t, op.Responses, "operation %q should have response examples", tt.id)
			require.NotNil(t, op.Responses["200"], "operation %q should have 200 response", tt.id)
		})
	}
}

func TestAddExamples_POSTOperationsHaveRequestExamples(t *testing.T) {
	postOps := []string{
		"eval-cel",
		"eval-template",
		"validate-schema",
		"explain-solution",
		"diff-solutions",
		"solution-lint",
		"solution-inspect",
		"solution-dryrun",
		"solution-run",
		"solution-render",
		"solution-test",
	}

	for _, id := range postOps {
		t.Run(id, func(t *testing.T) {
			op := addExamples(huma.Operation{
				OperationID: id,
				Method:      http.MethodPost,
			}, http.StatusOK)

			require.NotNil(t, op.RequestBody, "POST operation %q should have request body examples", id)
			require.NotNil(t, op.RequestBody.Content["application/json"])
			assert.NotNil(t, op.RequestBody.Content["application/json"].Examples["default"])
		})
	}
}

func TestExampleJSON_AllFilesAreValidJSON(t *testing.T) {
	entries, err := exampleFS.ReadDir("examples")
	require.NoError(t, err)

	for _, entry := range entries {
		t.Run(entry.Name(), func(t *testing.T) {
			name := entry.Name()
			// Strip .json suffix
			name = name[:len(name)-5]
			data := exampleJSON(name)
			assert.NotNil(t, data, "file %s should parse as valid JSON", entry.Name())
		})
	}
}

func BenchmarkExampleJSON(b *testing.B) {
	for b.Loop() {
		exampleJSON("eval-cel-request")
	}
}

func BenchmarkAddExamples(b *testing.B) {
	for b.Loop() {
		addExamples(huma.Operation{
			OperationID: "eval-cel",
			Method:      http.MethodPost,
		}, http.StatusOK)
	}
}
