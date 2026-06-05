// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package parameterprovider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockHTTPClient is a mock HTTP client for testing
type MockHTTPClient struct {
	Response *http.Response
	Err      error
}

func (m *MockHTTPClient) Get(_ context.Context, _ string) (*http.Response, error) {
	return m.Response, m.Err
}

// MockFileOps is a mock file operations for testing
type MockFileOps struct {
	Content []byte
	Err     error
}

func (m *MockFileOps) ReadFile(_ string) ([]byte, error) {
	return m.Content, m.Err
}

func TestNewParameterProvider(t *testing.T) {
	p := NewParameterProvider()

	assert.NotNil(t, p)
	assert.NotNil(t, p.Descriptor())
	assert.Equal(t, ProviderName, p.Descriptor().Name)
	assert.Equal(t, Version, p.Descriptor().Version.String())
}

func TestParameterProvider_Execute_StringParameter(t *testing.T) {
	p := NewParameterProvider()
	ctx := provider.WithParameters(context.Background(), map[string]any{
		"env": "prod",
	})

	output, err := p.Execute(ctx, map[string]any{
		"key": "env",
	})

	require.NoError(t, err)
	assert.NotNil(t, output)
	assert.Equal(t, "prod", output.Data)
	assert.Equal(t, true, output.Metadata["exists"])
	assert.Equal(t, "string", output.Metadata["type"])
}

func TestParameterProvider_Execute_MissingParameter_NoDefault(t *testing.T) {
	p := NewParameterProvider()
	ctx := provider.WithParameters(context.Background(), map[string]any{})

	output, err := p.Execute(ctx, map[string]any{
		"key": "env",
	})

	assert.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "not provided")
}

func TestParameterProvider_Execute_NoKey(t *testing.T) {
	p := NewParameterProvider()
	ctx := provider.WithParameters(context.Background(), map[string]any{})

	output, err := p.Execute(ctx, map[string]any{})

	assert.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "key is required")
}

func TestParameterProvider_ParseValue_Boolean(t *testing.T) {
	p := NewParameterProvider()
	ctx := context.Background()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"lowercase true", "true", true},
		{"uppercase TRUE", "TRUE", true},
		{"lowercase false", "false", false},
		{"uppercase FALSE", "FALSE", false},
		{"mixed case True", "True", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := p.parseValue(ctx, tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParameterProvider_ParseValue_Integer(t *testing.T) {
	p := NewParameterProvider()
	ctx := context.Background()

	tests := []struct {
		name     string
		input    string
		expected int64
	}{
		{"positive", "42", 42},
		{"negative", "-10", -10},
		{"zero", "0", 0},
		{"large", "9999999", 9999999},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := p.parseValue(ctx, tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParameterProvider_ParseValue_Float(t *testing.T) {
	p := NewParameterProvider()
	ctx := context.Background()

	tests := []struct {
		name     string
		input    string
		expected float64
	}{
		{"decimal", "3.14", 3.14},
		{"negative", "-2.5", -2.5},
		{"scientific", "1e10", 1e10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := p.parseValue(ctx, tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParameterProvider_ParseValue_CSV(t *testing.T) {
	p := NewParameterProvider()
	ctx := context.Background()

	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{"simple list", "a,b,c", []string{"a", "b", "c"}},
		{"with spaces", "a, b, c", []string{"a", "b", "c"}},
		{"mixed", "us-east1,us-west1,eu-west1", []string{"us-east1", "us-west1", "eu-west1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := p.parseValue(ctx, tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParameterProvider_ParseValue_QuotedString_NoCSVParsing(t *testing.T) {
	p := NewParameterProvider()
	ctx := context.Background()

	// Quoted strings should not be parsed as CSV
	result, err := p.parseValue(ctx, `"a,b,c"`)
	require.NoError(t, err)
	assert.Equal(t, "a,b,c", result) // Should be string, not array
}

func TestParameterProvider_ParseValue_JSON_Object(t *testing.T) {
	p := NewParameterProvider()
	ctx := context.Background()

	result, err := p.parseValue(ctx, `{"key":"value","num":42}`)
	require.NoError(t, err)

	obj, ok := result.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "value", obj["key"])
	assert.Equal(t, float64(42), obj["num"]) // JSON numbers are float64
}

func TestParameterProvider_ParseValue_JSON_Array(t *testing.T) {
	p := NewParameterProvider()
	ctx := context.Background()

	result, err := p.parseValue(ctx, `["a","b","c"]`)
	require.NoError(t, err)

	arr, ok := result.([]any)
	require.True(t, ok)
	assert.Len(t, arr, 3)
	assert.Equal(t, "a", arr[0])
}

func TestParameterProvider_ParseValue_FileProtocol(t *testing.T) {
	mockFile := &MockFileOps{
		Content: []byte("file content"),
		Err:     nil,
	}

	p := NewParameterProvider(WithFileOps(mockFile))
	ctx := context.Background()

	result, err := p.parseValue(ctx, "file:///path/to/file.txt")
	require.NoError(t, err)
	assert.Equal(t, "file content", result)
}

func TestParameterProvider_ParseValue_FileProtocol_Error(t *testing.T) {
	mockFile := &MockFileOps{
		Content: nil,
		Err:     errors.New("file not found"),
	}

	p := NewParameterProvider(WithFileOps(mockFile))
	ctx := context.Background()

	result, err := p.parseValue(ctx, "file:///path/to/missing.txt")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to read file")
}

func TestParameterProvider_ParseValue_LiteralString(t *testing.T) {
	p := NewParameterProvider()
	ctx := context.Background()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple", "hello", "hello"},
		{"with spaces", "hello world", "hello world"},
		{"url-like but quoted", `"https://example.com"`, "https://example.com"},
		{"number-like string", "042x", "042x"}, // Not parseable as number
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := p.parseValue(ctx, tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParameterProvider_ParseValue_Stdin_ShouldError(t *testing.T) {
	p := NewParameterProvider()
	ctx := context.Background()

	result, err := p.parseValue(ctx, "-")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "stdin value")
}

func TestParameterProvider_Execute_DryRun(t *testing.T) {
	p := NewParameterProvider()
	ctx := provider.WithDryRun(context.Background(), true)
	ctx = provider.WithParameters(ctx, map[string]any{})

	output, err := p.Execute(ctx, map[string]any{
		"key": "env",
	})

	require.NoError(t, err)
	assert.NotNil(t, output)
	assert.Equal(t, "[DRY-RUN] Not retrieved", output.Data)
	assert.True(t, output.Metadata["dryRun"].(bool))
}

func TestParameterProvider_Execute_AlreadyParsedValue(t *testing.T) {
	p := NewParameterProvider()

	// Simulate value already parsed (e.g., from merging multiple -r flags)
	ctx := provider.WithParameters(context.Background(), map[string]any{
		"items": []string{"a", "b", "c"},
	})

	output, err := p.Execute(ctx, map[string]any{
		"key": "items",
	})

	require.NoError(t, err)
	assert.NotNil(t, output)
	result := output.Data
	arr, ok := result.([]string)
	require.True(t, ok)
	assert.Equal(t, []string{"a", "b", "c"}, arr)
}

func TestDetectType(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{"nil", nil, "null"},
		{"bool", true, "boolean"},
		{"int", 42, "integer"},
		{"int64", int64(42), "integer"},
		{"float64", 3.14, "float"},
		{"string", "hello", "string"},
		{"array", []string{"a", "b"}, "array"},
		{"map", map[string]any{"key": "value"}, "object"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsQuoted(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"quoted", `"hello"`, true},
		{"not quoted", "hello", false},
		{"single char", `"a"`, true},
		{"empty quotes", `""`, true},
		{"one quote", `"hello`, false},
		{"trailing quote", `hello"`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isQuoted(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParameterProvider_Descriptor(t *testing.T) {
	p := NewParameterProvider()
	desc := p.Descriptor()

	assert.Equal(t, ProviderName, desc.Name)
	assert.Equal(t, "CLI Parameters", desc.DisplayName)
	assert.Contains(t, desc.Capabilities, provider.CapabilityFrom)

	// Check schema
	assert.Contains(t, desc.Schema.Properties, "key")
	assert.Contains(t, desc.Schema.Required, "key")
	assert.Contains(t, desc.Schema.Properties, "default")
	assert.NotContains(t, desc.Schema.Required, "default")
}

func TestParameterProvider_Execute_Default_UsedWhenKeyMissing(t *testing.T) {
	t.Parallel()
	p := NewParameterProvider()
	ctx := provider.WithParameters(context.Background(), map[string]any{})

	output, err := p.Execute(ctx, map[string]any{
		"key":     "env",
		"default": "development",
	})

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "development", output.Data)
	assert.Equal(t, false, output.Metadata["exists"])
	assert.Equal(t, "string", output.Metadata["type"])
}

func TestParameterProvider_Execute_Default_NotUsedWhenKeyPresent(t *testing.T) {
	t.Parallel()
	p := NewParameterProvider()
	ctx := provider.WithParameters(context.Background(), map[string]any{
		"env": "production",
	})

	output, err := p.Execute(ctx, map[string]any{
		"key":     "env",
		"default": "development",
	})

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "production", output.Data)
	assert.Equal(t, true, output.Metadata["exists"])
}

func TestParameterProvider_Execute_Default_TypedValues(t *testing.T) {
	t.Parallel()
	p := NewParameterProvider()
	ctx := provider.WithParameters(context.Background(), map[string]any{})

	tests := []struct {
		name         string
		defaultVal   any
		expectedData any
		expectedType string
	}{
		{"string literal", "fallback", "fallback", "string"},
		{"bool string true", "true", true, "boolean"},
		{"integer string", "42", int64(42), "integer"},
		{"csv string", "a,b,c", []string{"a", "b", "c"}, "array"},
		{"already-bool", true, true, "boolean"},
		{"already-int", int64(99), int64(99), "integer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			output, err := p.Execute(ctx, map[string]any{
				"key":     "missing",
				"default": tt.defaultVal,
			})
			require.NoError(t, err)
			require.NotNil(t, output)
			assert.Equal(t, tt.expectedData, output.Data)
			assert.Equal(t, false, output.Metadata["exists"])
			assert.Equal(t, tt.expectedType, output.Metadata["type"])
		})
	}
}

func TestParameterProvider_Execute_NoDefault_StillErrors(t *testing.T) {
	t.Parallel()
	p := NewParameterProvider()
	ctx := provider.WithParameters(context.Background(), map[string]any{})

	output, err := p.Execute(ctx, map[string]any{
		"key": "env",
	})

	assert.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "not provided")
}

func TestWithHTTPClient_ExercisesOption(t *testing.T) {
	t.Parallel()
	allow := true
	cfg := &config.Config{HTTPClient: config.HTTPClientConfig{AllowPrivateIPs: &allow}}
	ctx := config.WithConfig(context.Background(), cfg)

	mock := &MockHTTPClient{Err: errors.New("mock-error")}
	p := NewParameterProvider(WithHTTPClient(mock))
	_, err := p.parseValue(ctx, "http://127.0.0.1/data")
	assert.ErrorContains(t, err, "mock-error")
}

func TestParameterProvider_ParseValue_HTTP_Success(t *testing.T) {
	t.Parallel()
	allow := true
	cfg := &config.Config{HTTPClient: config.HTTPClientConfig{AllowPrivateIPs: &allow}}
	ctx := config.WithConfig(context.Background(), cfg)

	mock := &MockHTTPClient{
		Response: &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("remote-value")),
		},
	}
	p := NewParameterProvider(WithHTTPClient(mock))
	result, err := p.parseValue(ctx, "http://127.0.0.1/data")
	require.NoError(t, err)
	assert.Equal(t, "remote-value", result)
}

func TestParameterProvider_ParseValue_HTTP_NonOKStatus(t *testing.T) {
	t.Parallel()
	allow := true
	cfg := &config.Config{HTTPClient: config.HTTPClientConfig{AllowPrivateIPs: &allow}}
	ctx := config.WithConfig(context.Background(), cfg)

	mock := &MockHTTPClient{
		Response: &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader("")),
		},
	}
	p := NewParameterProvider(WithHTTPClient(mock))
	_, err := p.parseValue(ctx, "http://127.0.0.1/data")
	assert.ErrorContains(t, err, "404")
}

func TestDefaultFileOps_ReadFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.txt")
	require.NoError(t, os.WriteFile(path, []byte("file-content"), 0o600))

	ops := &DefaultFileOps{}
	content, err := ops.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "file-content", string(content))
}

func TestDefaultFileOps_ReadFile_Missing(t *testing.T) {
	t.Parallel()
	ops := &DefaultFileOps{}
	_, err := ops.ReadFile(filepath.Join(t.TempDir(), "nonexistent.txt"))
	assert.Error(t, err)
}

func TestDefaultHTTPClient_Get(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := &DefaultHTTPClient{client: httpc.NewClient(&httpc.ClientConfig{
		Timeout:      settings.DefaultHTTPTimeout,
		RetryMax:     0,
		RetryWaitMin: settings.DefaultHTTPRetryWaitMinimum,
		RetryWaitMax: settings.DefaultHTTPRetryWaitMaximum,
	})}

	resp, err := c.Get(context.Background(), srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestParameterProvider_Execute_Default_ParseError(t *testing.T) {
	t.Parallel()
	p := NewParameterProvider()
	ctx := provider.WithParameters(context.Background(), map[string]any{})

	// "-" triggers a parse error in parseValue (stdin should have been resolved)
	output, err := p.Execute(ctx, map[string]any{
		"key":     "env",
		"default": "-",
	})

	assert.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "failed to parse default")
	assert.Contains(t, err.Error(), "stdin value")
}

func TestParameterProvider_Execute_DryRun_WithDefault_IgnoresDefault(t *testing.T) {
	t.Parallel()
	p := NewParameterProvider()
	// dry-run is active, key is absent, default is provided
	ctx := provider.WithDryRun(context.Background(), true)
	ctx = provider.WithParameters(ctx, map[string]any{})

	output, err := p.Execute(ctx, map[string]any{
		"key":     "env",
		"default": "should-not-appear",
	})

	// dry-run fires before default check — always returns the dry-run sentinel
	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "[DRY-RUN] Not retrieved", output.Data)
	assert.True(t, output.Metadata["dryRun"].(bool))
}
