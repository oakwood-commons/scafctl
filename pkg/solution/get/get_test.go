// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package get

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGetter(t *testing.T) {
	t.Run("with default options", func(t *testing.T) {
		getter := NewGetter()

		require.NotNil(t, getter)
		assert.NotNil(t, getter.readFile, "readFile should be set to default os.ReadFile")
		assert.NotNil(t, getter.httpClient, "httpClient should be set to default client")
	})

	t.Run("with custom readFile", func(t *testing.T) {
		customReadFile := func(name string) ([]byte, error) {
			return []byte("custom data"), nil
		}

		getter := NewGetter(WithReadFile(customReadFile))

		require.NotNil(t, getter)
		assert.NotNil(t, getter.readFile, "readFile should be set")
		assert.NotNil(t, getter.httpClient, "httpClient should still be set to default")

		// Verify custom readFile works
		data, err := getter.readFile("test.txt")
		require.NoError(t, err)
		assert.Equal(t, []byte("custom data"), data)
	})

	t.Run("with custom httpClient", func(t *testing.T) {
		config := httpc.DefaultConfig()
		config.EnableCache = false
		customClient := httpc.NewClient(config)

		getter := NewGetter(WithHTTPClient(customClient))

		require.NotNil(t, getter)
		assert.NotNil(t, getter.readFile, "readFile should be set to default")
		assert.NotNil(t, getter.httpClient, "httpClient should be set to custom client")
		assert.Equal(t, customClient, getter.httpClient)
	})

	t.Run("with multiple options", func(t *testing.T) {
		customReadFile := func(name string) ([]byte, error) {
			return []byte("custom data"), nil
		}

		config := httpc.DefaultConfig()
		config.EnableCache = false
		customClient := httpc.NewClient(config)

		getter := NewGetter(
			WithReadFile(customReadFile),
			WithHTTPClient(customClient),
		)

		require.NotNil(t, getter)
		assert.NotNil(t, getter.readFile, "readFile should be set to custom")
		assert.NotNil(t, getter.httpClient, "httpClient should be set to custom")
		assert.Equal(t, customClient, getter.httpClient)

		// Verify custom readFile works
		data, err := getter.readFile("test.txt")
		require.NoError(t, err)
		assert.Equal(t, []byte("custom data"), data)
	})
}

func TestWithReadFile(t *testing.T) {
	customReadFile := func(name string) ([]byte, error) {
		return []byte("test"), nil
	}

	option := WithReadFile(customReadFile)
	require.NotNil(t, option)

	getter := &Getter{}
	option(getter)

	assert.NotNil(t, getter.readFile)
	data, err := getter.readFile("test.txt")
	require.NoError(t, err)
	assert.Equal(t, []byte("test"), data)
}

func TestWithHTTPClient(t *testing.T) {
	config := httpc.DefaultConfig()
	config.EnableCache = false
	customClient := httpc.NewClient(config)

	option := WithHTTPClient(customClient)
	require.NotNil(t, option)

	getter := &Getter{}
	option(getter)

	assert.NotNil(t, getter.httpClient)
	assert.Equal(t, customClient, getter.httpClient)
}

func TestWithLogger(t *testing.T) {
	customLogger := logr.Discard()

	option := WithLogger(customLogger)
	require.NotNil(t, option)

	getter := &Getter{}
	option(getter)

	// Logger is set (we can't easily compare logr.Logger values, so just check it's not nil)
	assert.NotNil(t, getter.logger)
}

func TestNewGetter_WithLogger(t *testing.T) {
	customLogger := logr.Discard()

	getter := NewGetter(WithLogger(customLogger))

	require.NotNil(t, getter)
	assert.NotNil(t, getter.logger)
	assert.NotNil(t, getter.readFile)
	assert.NotNil(t, getter.httpClient)
}

func TestFromUrl(t *testing.T) {
	validSolutionJSON := `{
		"apiVersion": "scafctl.io/v1",
		"kind": "Solution",
		"metadata": {
			"name": "test-solution",
			"version": "1.0.0"
		}
	}`

	t.Run("successful fetch", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(validSolutionJSON))
		}))
		defer server.Close()

		getter := NewGetter()
		ctx := context.Background()

		sol, err := getter.FromURL(ctx, server.URL)
		require.NoError(t, err)
		require.NotNil(t, sol)
	})

	t.Run("invalid URL", func(t *testing.T) {
		getter := NewGetter()
		ctx := context.Background()

		sol, err := getter.FromURL(ctx, "not-a-url")
		require.Error(t, err)
		assert.Nil(t, sol)
		assert.Contains(t, err.Error(), "not a valid URL")
	})

	t.Run("404 response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		// Disable caching so the 404 response is not persisted to the filesystem cache,
		// which would cause stale responses for later tests that reuse the same port.
		cfg := httpc.DefaultConfig()
		cfg.EnableCache = false
		cfg.RetryMax = 0
		getter := NewGetter(WithHTTPClient(httpc.NewClient(cfg)))
		ctx := context.Background()

		sol, err := getter.FromURL(ctx, server.URL)
		require.Error(t, err)
		assert.Nil(t, sol)
		assert.Contains(t, err.Error(), "non-200 response")
		assert.Contains(t, err.Error(), "404")
	})

	t.Run("500 response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		// Use a custom client with retries disabled for faster test
		config := httpc.DefaultConfig()
		config.RetryMax = 0
		customClient := httpc.NewClient(config)

		getter := NewGetter(WithHTTPClient(customClient))
		ctx := context.Background()

		sol, err := getter.FromURL(ctx, server.URL)
		require.Error(t, err)
		assert.Nil(t, sol)
		// When retries are exhausted, the error message contains "giving up after"
		assert.Contains(t, err.Error(), "Failed fetching from URL")
	})

	t.Run("invalid JSON response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("invalid json"))
		}))
		defer server.Close()

		getter := NewGetter()
		ctx := context.Background()

		sol, err := getter.FromURL(ctx, server.URL)
		require.Error(t, err)
		assert.Nil(t, sol)
		assert.Contains(t, err.Error(), "failed to load solution from")
	})

	t.Run("with custom HTTP client", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(validSolutionJSON))
		}))
		defer server.Close()

		config := httpc.DefaultConfig()
		config.EnableCache = false
		customClient := httpc.NewClient(config)

		getter := NewGetter(WithHTTPClient(customClient))
		ctx := context.Background()

		sol, err := getter.FromURL(ctx, server.URL)
		require.NoError(t, err)
		require.NotNil(t, sol)
	})

	t.Run("context cancellation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Never respond
			<-r.Context().Done()
		}))
		defer server.Close()

		getter := NewGetter()
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		sol, err := getter.FromURL(ctx, server.URL)
		require.Error(t, err)
		assert.Nil(t, sol)
	})

	t.Run("with custom logger", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(validSolutionJSON))
		}))
		defer server.Close()

		customLogger := logr.Discard()
		// Disable caching to avoid stale cached responses from port-reuse between tests.
		cfg := httpc.DefaultConfig()
		cfg.EnableCache = false
		cfg.RetryMax = 0
		getter := NewGetter(
			WithLogger(customLogger),
			WithHTTPClient(httpc.NewClient(cfg)),
		)
		ctx := context.Background()

		sol, err := getter.FromURL(ctx, server.URL)
		require.NoError(t, err)
		require.NotNil(t, sol)
	})

	t.Run("validation failure", func(t *testing.T) {
		// Missing metadata.name triggers validation error
		invalidSolutionJSON := `{"apiVersion":"scafctl.io/v1","kind":"Solution","metadata":{}}`

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(invalidSolutionJSON))
		}))
		defer server.Close()

		getter := NewGetter()
		ctx := context.Background()

		sol, err := getter.FromURL(ctx, server.URL)
		require.Error(t, err)
		assert.Nil(t, sol)
		assert.Contains(t, err.Error(), "validation")
	})
}

func TestFromLocalFileSystem(t *testing.T) {
	validSolutionJSON := `{
		"apiVersion": "scafctl.io/v1",
		"kind": "Solution",
		"metadata": {
			"name": "test-solution",
			"version": "1.0.0"
		}
	}`

	t.Run("successful read", func(t *testing.T) {
		customReadFile := func(name string) ([]byte, error) {
			return []byte(validSolutionJSON), nil
		}

		getter := NewGetter(WithReadFile(customReadFile))
		ctx := context.Background()

		sol, err := getter.FromLocalFileSystem(ctx, "test.json")
		require.NoError(t, err)
		require.NotNil(t, sol)
		assert.Equal(t, solution.DefaultAPIVersion, sol.APIVersion)
		assert.Equal(t, solution.SolutionKind, sol.Kind)
		assert.Equal(t, "private", sol.Catalog.Visibility)
	})

	t.Run("validation failure", func(t *testing.T) {
		// Missing metadata.name triggers validation error
		customReadFile := func(name string) ([]byte, error) {
			return []byte(`{"apiVersion":"scafctl.io/v1","kind":"Solution","metadata":{}}`), nil
		}

		getter := NewGetter(WithReadFile(customReadFile))
		ctx := context.Background()

		sol, err := getter.FromLocalFileSystem(ctx, "invalid.json")
		require.Error(t, err)
		require.NotNil(t, sol)
		assert.Contains(t, err.Error(), "validation")
	})

	t.Run("file read error", func(t *testing.T) {
		customReadFile := func(name string) ([]byte, error) {
			return nil, fmt.Errorf("file not found")
		}

		getter := NewGetter(WithReadFile(customReadFile))
		ctx := context.Background()

		sol, err := getter.FromLocalFileSystem(ctx, "missing.json")
		require.Error(t, err)
		require.NotNil(t, sol) // Returns empty solution on error
		assert.Contains(t, err.Error(), "Failed reading file")
	})

	t.Run("invalid JSON", func(t *testing.T) {
		customReadFile := func(name string) ([]byte, error) {
			return []byte("invalid json"), nil
		}

		getter := NewGetter(WithReadFile(customReadFile))
		ctx := context.Background()

		sol, err := getter.FromLocalFileSystem(ctx, "invalid.json")
		require.Error(t, err)
		require.NotNil(t, sol) // Returns empty solution on error
		assert.Contains(t, err.Error(), "failed to load solution from")
	})

	t.Run("with custom logger", func(t *testing.T) {
		customReadFile := func(name string) ([]byte, error) {
			return []byte(validSolutionJSON), nil
		}

		customLogger := logr.Discard()
		getter := NewGetter(
			WithReadFile(customReadFile),
			WithLogger(customLogger),
		)
		ctx := context.Background()

		sol, err := getter.FromLocalFileSystem(ctx, "test.json")
		require.NoError(t, err)
		require.NotNil(t, sol)
	})
}

func TestWithStatFunc(t *testing.T) {
	customStatFunc := func(path string) (os.FileInfo, error) {
		return nil, fmt.Errorf("stat error")
	}

	option := WithStatFunc(customStatFunc)
	require.NotNil(t, option)

	getter := &Getter{}
	option(getter)

	assert.NotNil(t, getter.statFunc)
	_, err := getter.statFunc("test.txt")
	require.Error(t, err)
	assert.Equal(t, "stat error", err.Error())
}

func TestGet(t *testing.T) {
	validSolutionJSON := `{
		"apiVersion": "scafctl.io/v1",
		"kind": "Solution",
		"metadata": {
			"name": "test-solution",
			"version": "1.0.0"
		}
	}`

	t.Run("with explicit URL path", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(validSolutionJSON))
		}))
		defer server.Close()

		// Disable caching so a previously cached response for a reused port doesn't
		// cause a stale 404 to be returned instead of the actual 200 from this server.
		cfg := httpc.DefaultConfig()
		cfg.EnableCache = false
		cfg.RetryMax = 0
		getter := NewGetter(WithHTTPClient(httpc.NewClient(cfg)))
		ctx := context.Background()

		sol, err := getter.Get(ctx, server.URL)
		require.NoError(t, err)
		require.NotNil(t, sol)
	})

	t.Run("with explicit file path", func(t *testing.T) {
		customReadFile := func(name string) ([]byte, error) {
			return []byte(validSolutionJSON), nil
		}

		getter := NewGetter(WithReadFile(customReadFile))
		ctx := context.Background()

		sol, err := getter.Get(ctx, "/path/to/solution.yaml")
		require.NoError(t, err)
		require.NotNil(t, sol)
	})

	t.Run("with empty path - finds solution", func(t *testing.T) {
		customReadFile := func(name string) ([]byte, error) {
			return []byte(validSolutionJSON), nil
		}

		customStatFunc := func(path string) (os.FileInfo, error) {
			// Simulate file exists
			return nil, nil
		}

		getter := NewGetter(
			WithReadFile(customReadFile),
			WithStatFunc(customStatFunc),
		)
		ctx := context.Background()

		sol, err := getter.Get(ctx, "")
		require.NoError(t, err)
		require.NotNil(t, sol)
	})

	t.Run("with empty path - no solution found", func(t *testing.T) {
		customStatFunc := func(path string) (os.FileInfo, error) {
			// Simulate file doesn't exist
			return nil, fmt.Errorf("file not found")
		}

		getter := NewGetter(WithStatFunc(customStatFunc))
		ctx := context.Background()

		sol, err := getter.Get(ctx, "")
		require.Error(t, err)
		assert.Nil(t, sol)
		assert.Contains(t, err.Error(), "no solution path provided and no solution file found")
	})
}

func TestFindSolution(t *testing.T) {
	t.Run("finds first existing solution", func(t *testing.T) {
		callCount := 0
		customStatFunc := func(path string) (os.FileInfo, error) {
			callCount++
			// Return success on the third call
			if callCount == 3 {
				return nil, nil
			}
			return nil, fmt.Errorf("not found")
		}

		getter := NewGetter(WithStatFunc(customStatFunc))
		path := getter.FindSolution()

		assert.NotEmpty(t, path)
		// The third file checked would be scafctl/scafctl.yaml
		assert.Contains(t, path, "scafctl")
	})

	t.Run("returns empty when no solution found", func(t *testing.T) {
		customStatFunc := func(path string) (os.FileInfo, error) {
			// Always return error (file doesn't exist)
			return nil, fmt.Errorf("not found")
		}

		getter := NewGetter(WithStatFunc(customStatFunc))
		path := getter.FindSolution()

		assert.Empty(t, path)
	})

	t.Run("checks all expected paths", func(t *testing.T) {
		checkedPaths := []string{}
		customStatFunc := func(path string) (os.FileInfo, error) {
			checkedPaths = append(checkedPaths, path)
			return nil, fmt.Errorf("not found")
		}

		getter := NewGetter(WithStatFunc(customStatFunc))
		_ = getter.FindSolution()

		// Should check multiple paths (3 folders * 6 filenames = 18 paths)
		assert.GreaterOrEqual(t, len(checkedPaths), 18)

		// Verify it checks for scafctl/solution.yaml
		foundSolutionYaml := false
		for _, p := range checkedPaths {
			if p == "scafctl/solution.yaml" {
				foundSolutionYaml = true
				break
			}
		}
		assert.True(t, foundSolutionYaml, "Should check for scafctl/solution.yaml")
	})

	t.Run("uses custom binary name folders", func(t *testing.T) {
		checkedPaths := []string{}
		customStatFunc := func(path string) (os.FileInfo, error) {
			checkedPaths = append(checkedPaths, path)
			return nil, fmt.Errorf("not found")
		}

		getter := NewGetter(
			WithStatFunc(customStatFunc),
			WithSolutionDiscovery(
				settings.SolutionFoldersFor("cldctl"),
				settings.SolutionFileNamesFor("cldctl"),
			),
		)
		_ = getter.FindSolution()

		// Should check cldctl/solution.yaml, not scafctl/solution.yaml
		foundCldctl := false
		foundScafctl := false
		for _, p := range checkedPaths {
			if p == "cldctl/solution.yaml" {
				foundCldctl = true
			}
			if p == "scafctl/solution.yaml" {
				foundScafctl = true
			}
		}
		assert.True(t, foundCldctl, "Should check for cldctl/solution.yaml")
		assert.False(t, foundScafctl, "Should NOT check for scafctl/solution.yaml")
	})
}

func TestGetPossibleSolutionPaths(t *testing.T) {
	t.Run("returns all possible paths", func(t *testing.T) {
		paths := PossibleSolutionPaths()

		// Should have 3 folders * 6 filenames = 18 paths
		assert.Len(t, paths, 18)
	})

	t.Run("contains expected paths", func(t *testing.T) {
		paths := PossibleSolutionPaths()

		// Check for some expected paths
		// Note: empty folder + filename produces bare "solution.yaml" (no leading slash)
		expectedPaths := []string{
			"scafctl/solution.yaml",
			"scafctl/solution.yml",
			".scafctl/solution.yaml",
			"solution.yaml", // Empty folder + filename
			"scafctl/scafctl.yaml",
			"solution.json", // Empty folder + filename
		}

		for _, expected := range expectedPaths {
			assert.Contains(t, paths, expected, "Should contain path: %s", expected)
		}
	})
}

// mockCatalogResolver implements CatalogResolver for testing
type mockCatalogResolver struct {
	solutions map[string][]byte
	err       error
}

func (m *mockCatalogResolver) FetchSolution(_ context.Context, nameWithVersion string) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	if content, ok := m.solutions[nameWithVersion]; ok {
		return content, nil
	}
	return nil, fmt.Errorf("solution not found: %s", nameWithVersion)
}

// mockRemoteResolver implements RemoteResolver for testing
type mockRemoteResolver struct {
	solutions  map[string][]byte
	bundleData map[string][]byte
	err        error
}

func (m *mockRemoteResolver) FetchRemoteSolution(_ context.Context, ref string) ([]byte, []byte, error) {
	if m.err != nil {
		return nil, nil, m.err
	}
	if content, ok := m.solutions[ref]; ok {
		return content, m.bundleData[ref], nil
	}
	return nil, nil, fmt.Errorf("remote solution not found: %s", ref)
}

func TestWithCatalogResolver(t *testing.T) {
	mock := &mockCatalogResolver{
		solutions: map[string][]byte{"test": []byte("content")},
	}

	option := WithCatalogResolver(mock)
	require.NotNil(t, option)

	getter := &Getter{}
	option(getter)

	assert.Equal(t, mock, getter.catalogResolver)
}

func TestGetter_isBareName(t *testing.T) {
	getter := NewGetter()

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "simple name is bare",
			path:     "my-solution",
			expected: true,
		},
		{
			name:     "name with version is bare",
			path:     "my-solution@1.0.0",
			expected: true,
		},
		{
			name:     "path with slash is not bare",
			path:     "./my-solution.yaml",
			expected: false,
		},
		{
			name:     "path with directory is not bare",
			path:     "examples/solution.yaml",
			expected: false,
		},
		{
			name:     "absolute path is not bare",
			path:     "/home/user/solution.yaml",
			expected: false,
		},
		{
			name:     "yaml extension is not bare",
			path:     "solution.yaml",
			expected: false,
		},
		{
			name:     "yml extension is not bare",
			path:     "solution.yml",
			expected: false,
		},
		{
			name:     "json extension is not bare",
			path:     "solution.json",
			expected: false,
		},
		{
			name:     "URL is not bare",
			path:     "https://example.com/solution.yaml",
			expected: false,
		},
		{
			name:     "http URL is not bare",
			path:     "http://localhost:8080/solution.yaml",
			expected: false,
		},
		{
			name:     "windows path is not bare",
			path:     "C:\\Users\\solution.yaml",
			expected: false,
		},
		{
			name:     "name with hyphen is bare",
			path:     "my-complex-solution-name",
			expected: true,
		},
		{
			name:     "name with numbers is bare",
			path:     "solution123",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getter.isBareName(tt.path)
			assert.Equal(t, tt.expected, result, "isBareName(%q) = %v, want %v", tt.path, result, tt.expected)
		})
	}
}

func TestGetter_fromCatalog(t *testing.T) {
	validSolutionYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-solution
  version: 1.0.0
spec:
  resolvers: {}
`

	t.Run("successfully fetches from catalog", func(t *testing.T) {
		mock := &mockCatalogResolver{
			solutions: map[string][]byte{
				"test-solution@1.0.0": []byte(validSolutionYAML),
			},
		}

		getter := NewGetter(WithCatalogResolver(mock))
		sol, err := getter.fromCatalog(context.Background(), "test-solution@1.0.0")

		require.NoError(t, err)
		assert.NotNil(t, sol)
		assert.Equal(t, "test-solution", sol.Metadata.Name)
		assert.Equal(t, "catalog:test-solution@1.0.0", sol.GetPath())
	})

	t.Run("returns error when catalog returns error", func(t *testing.T) {
		mock := &mockCatalogResolver{
			err: fmt.Errorf("catalog error"),
		}

		getter := NewGetter(WithCatalogResolver(mock))
		_, err := getter.fromCatalog(context.Background(), "test-solution")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "catalog error")
	})

	t.Run("returns error for invalid solution content", func(t *testing.T) {
		mock := &mockCatalogResolver{
			solutions: map[string][]byte{
				"bad-solution": []byte("not valid yaml: {{{"),
			},
		}

		getter := NewGetter(WithCatalogResolver(mock))
		_, err := getter.fromCatalog(context.Background(), "bad-solution")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse solution from catalog")
	})
}

func TestGetter_Get_WithCatalogResolver(t *testing.T) {
	validSolutionYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: catalog-solution
  version: 1.0.0
spec:
  resolvers: {}
`

	t.Run("resolves bare name from catalog", func(t *testing.T) {
		mock := &mockCatalogResolver{
			solutions: map[string][]byte{
				"catalog-solution": []byte(validSolutionYAML),
			},
		}

		getter := NewGetter(WithCatalogResolver(mock))
		sol, err := getter.Get(context.Background(), "catalog-solution")

		require.NoError(t, err)
		assert.NotNil(t, sol)
		assert.Equal(t, "catalog-solution", sol.Metadata.Name)
		assert.Equal(t, "catalog:catalog-solution", sol.GetPath())
	})

	t.Run("falls back to file when catalog miss", func(t *testing.T) {
		mock := &mockCatalogResolver{
			solutions: map[string][]byte{}, // empty - nothing in catalog
		}

		// Create a temp file
		tmpFile, err := os.CreateTemp("", "solution-*.yaml")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())

		fileSolutionYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: file-solution
  version: 1.0.0
spec:
  resolvers: {}
`
		_, err = tmpFile.WriteString(fileSolutionYAML)
		require.NoError(t, err)
		tmpFile.Close()

		getter := NewGetter(WithCatalogResolver(mock))
		// Use the file path which is not a bare name (has slashes)
		sol, err := getter.Get(context.Background(), tmpFile.Name())

		require.NoError(t, err)
		assert.NotNil(t, sol)
		assert.Equal(t, "file-solution", sol.Metadata.Name)
	})

	t.Run("does not try catalog for paths with slashes", func(t *testing.T) {
		// This mock would return an error if called
		mock := &mockCatalogResolver{
			err: fmt.Errorf("should not be called"),
		}

		// Create a temp file
		tmpFile, err := os.CreateTemp("", "solution-*.yaml")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())

		fileSolutionYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: file-solution
  version: 1.0.0
spec:
  resolvers: {}
`
		_, err = tmpFile.WriteString(fileSolutionYAML)
		require.NoError(t, err)
		tmpFile.Close()

		getter := NewGetter(WithCatalogResolver(mock))
		// File path has slashes, should not try catalog
		sol, err := getter.Get(context.Background(), tmpFile.Name())

		require.NoError(t, err)
		assert.NotNil(t, sol)
		assert.Equal(t, "file-solution", sol.Metadata.Name)
	})

	t.Run("does not try catalog for .yaml files", func(t *testing.T) {
		// This mock would fail if called
		mock := &mockCatalogResolver{
			err: fmt.Errorf("should not be called"),
		}

		getter := NewGetter(WithCatalogResolver(mock))
		// Even without slashes, .yaml extension means it's a file
		_, err := getter.Get(context.Background(), "solution.yaml")

		// Should try to read as file and fail (file doesn't exist)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Failed reading file")
	})
}

func TestWithAppConfig(t *testing.T) {
	httpCfg := &config.HTTPClientConfig{}
	logger := logr.Discard()
	opt := WithAppConfig(httpCfg, logger)
	g := &Getter{}
	opt(g)
	assert.NotNil(t, g.httpClient)
	assert.Equal(t, logger, g.logger)
}

func TestMockGetter_GetWithBundle(t *testing.T) {
	mg := &MockGetter{}
	ctx := context.Background()
	expectedSol := &solution.Solution{}
	expectedBundle := []byte("bundle-data")

	mg.On("GetWithBundle", ctx, "my-solution").Return(expectedSol, expectedBundle, nil)

	sol, bundle, err := mg.GetWithBundle(ctx, "my-solution")
	assert.NoError(t, err)
	assert.Equal(t, expectedSol, sol)
	assert.Equal(t, expectedBundle, bundle)
	mg.AssertExpectations(t)
}

func TestMockGetter_GetWithBundle_NilSolution(t *testing.T) {
	mg := &MockGetter{}
	ctx := context.Background()

	mg.On("GetWithBundle", ctx, "missing").Return(nil, nil, fmt.Errorf("not found"))

	sol, bundle, err := mg.GetWithBundle(ctx, "missing")
	assert.Error(t, err)
	assert.Nil(t, sol)
	assert.Nil(t, bundle)
	mg.AssertExpectations(t)
}

func TestGetter_GetWithBundle_LocalFile(t *testing.T) {
	const validSolutionYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: bundle-test
  version: 1.0.0
`
	tmpDir := t.TempDir()
	solPath := tmpDir + "/solution.yaml"
	require.NoError(t, os.WriteFile(solPath, []byte(validSolutionYAML), 0o600))

	getter := NewGetter()
	sol, bundleData, err := getter.GetWithBundle(context.Background(), solPath)
	require.NoError(t, err)
	require.NotNil(t, sol)
	assert.Nil(t, bundleData) // local files have no bundle data
}

func TestGetter_GetWithBundle_EmptyPath(t *testing.T) {
	getter := NewGetter()
	sol, bundleData, err := getter.GetWithBundle(context.Background(), "")
	assert.Error(t, err)
	assert.Nil(t, sol)
	assert.Nil(t, bundleData)
}

func TestValidatePositionalRef(t *testing.T) {
	tests := []struct {
		name     string
		arg      string
		fileFlag string
		wantErr  string
	}{
		{"catalog name passes", "my-app", "", ""},
		{"versioned name passes", "my-app@1.0.0", "", ""},
		{"file flag conflict", "my-app", "existing.yaml", "cannot use both -f/--file"},
		{"local path rejected", "./solution.yaml", "", "local file paths must use -f/--file flag"},
		{"yaml extension rejected", "solution.yaml", "", "local file paths must use -f/--file flag"},
		{"error includes cmd usage", "../foo.yaml", "", "scafctl test cmd -f ../foo.yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePositionalRef(tt.arg, tt.fileFlag, "scafctl test cmd")
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

func BenchmarkValidatePositionalRef(b *testing.B) {
	args := []string{"my-app", "./solution.yaml", "solution.yaml", "my-app@1.0.0"}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		for _, arg := range args {
			_ = ValidatePositionalRef(arg, "", "scafctl run solution")
		}
	}
}

func TestIsCatalogReference(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		// Catalog/registry refs (should return true)
		{"bare name", "my-app", true},
		{"versioned name", "my-app@1.0.0", true},
		{"registry ref", "ghcr.io/myorg/solutions/app:2.0", true},
		{"https URL", "https://example.com/solution.yaml", true},
		{"stdin marker", "-", true},
		{"bare name with dash", "deploy-to-k8s", true},
		{"name with underscore", "my_app", true},
		{"localhost registry", "localhost:5000/sol", true},
		{"oci URL", "oci://registry.example.com/sol:v1", true},
		{"nested registry ref", "registry.example.com/org/sol:v1", true},

		// Local file paths (should return false)
		{"absolute path", "/tmp/solution.yaml", false},
		{"relative dot path", "./solution.yaml", false},
		{"relative parent path", "../sol.yaml", false},
		{"yaml extension", "solution.yaml", false},
		{"yml extension", "solution.yml", false},
		{"json extension", "config.json", false},
		{"uppercase yaml", "Solution.YAML", false},
		{"dot only start", ".", false},
		{"dot-dot start", "..", false},

		// Relative paths without leading "./" — still local (not catalog refs)
		{"relative path no dot", "configs/solution", false},
		{"relative path nested", "relative/path/to/solution", false},
		{"relative path with yaml", "configs/solution.yaml", false},

		// Windows paths (should return false)
		{"windows absolute backslash", `C:\dir\sol`, false},
		{"windows absolute slash", "C:/dir/sol", false},
		{"bare backslash path", `dir\sol`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsCatalogReference(tt.input)
			assert.Equal(t, tt.expected, got, "IsCatalogReference(%q)", tt.input)
		})
	}
}

func BenchmarkIsCatalogReference(b *testing.B) {
	inputs := []string{
		"my-app",
		"my-app@1.0.0",
		"ghcr.io/myorg/solutions/app:2.0",
		"./solution.yaml",
		"/tmp/solution.yaml",
		"solution.yaml",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		for _, input := range inputs {
			IsCatalogReference(input)
		}
	}
}

func TestWithSolutionDiscovery(t *testing.T) {
	t.Parallel()
	folders := []string{"mycli", ".mycli", ""}
	fileNames := []string{"solution.yaml", "mycli.yaml"}

	g := NewGetter(WithSolutionDiscovery(folders, fileNames))
	assert.Equal(t, folders, g.solutionFolders)
	assert.Equal(t, fileNames, g.solutionFileNames)
}

func TestNewGetterFromContext_NilContext(t *testing.T) {
	t.Parallel()
	//nolint:staticcheck // SA1012: intentionally testing nil context handling
	g := NewGetterFromContext(nil)
	// Should fall back to defaults
	assert.Equal(t, settings.RootSolutionFolders, g.solutionFolders)
	assert.Equal(t, settings.SolutionFileNames, g.solutionFileNames)
}

func TestNewGetterFromContext_NoSettings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	g := NewGetterFromContext(ctx)
	// No settings in context — should use defaults
	assert.Equal(t, settings.RootSolutionFolders, g.solutionFolders)
	assert.Equal(t, settings.SolutionFileNames, g.solutionFileNames)
}

func TestNewGetterFromContext_DefaultBinaryName(t *testing.T) {
	t.Parallel()
	run := &settings.Run{BinaryName: "scafctl"}
	ctx := settings.IntoContext(context.Background(), run)
	g := NewGetterFromContext(ctx)
	// BinaryName matches default — should not override
	assert.Equal(t, settings.RootSolutionFolders, g.solutionFolders)
	assert.Equal(t, settings.SolutionFileNames, g.solutionFileNames)
}

func TestNewGetterFromContext_CustomBinaryName(t *testing.T) {
	t.Parallel()
	run := &settings.Run{BinaryName: "mycli"}
	ctx := settings.IntoContext(context.Background(), run)
	g := NewGetterFromContext(ctx)
	// BinaryName is custom — should override
	assert.Equal(t, settings.SolutionFoldersFor("mycli"), g.solutionFolders)
	assert.Equal(t, settings.SolutionFileNamesFor("mycli"), g.solutionFileNames)
}

func TestNewGetterFromContext_WithAdditionalOpts(t *testing.T) {
	t.Parallel()
	run := &settings.Run{BinaryName: "mycli"}
	ctx := settings.IntoContext(context.Background(), run)
	lgr := logr.Discard()
	g := NewGetterFromContext(ctx, WithLogger(lgr))
	// Should have custom folders AND the logger option applied
	assert.Equal(t, settings.SolutionFoldersFor("mycli"), g.solutionFolders)
	assert.NotNil(t, g.logger)
}

func TestWithRemoteResolver(t *testing.T) {
	t.Parallel()
	mock := &mockRemoteResolver{
		solutions: map[string][]byte{"ghcr.io/org/sol@1.0.0": []byte("content")},
	}

	option := WithRemoteResolver(mock)
	require.NotNil(t, option)

	getter := &Getter{}
	option(getter)

	assert.Equal(t, mock, getter.remoteResolver)
}

func TestGetter_Get_RemoteRef(t *testing.T) {
	t.Parallel()

	validSolution := []byte(`
metadata:
  name: remote-sol
  version: 1.0.0
spec:
  resolvers: {}
`)

	t.Run("resolves Docker-style remote ref", func(t *testing.T) {
		t.Parallel()
		mock := &mockRemoteResolver{
			solutions: map[string][]byte{
				"ghcr.io/myorg/starter-kit@1.0.0": validSolution,
			},
		}
		getter := NewGetter(WithRemoteResolver(mock))
		sol, err := getter.Get(context.Background(), "ghcr.io/myorg/starter-kit@1.0.0")
		require.NoError(t, err)
		assert.Equal(t, "remote-sol", sol.Metadata.Name)
		assert.Equal(t, "remote:ghcr.io/myorg/starter-kit@1.0.0", sol.GetPath())
	})

	t.Run("remote ref error returns wrapped error", func(t *testing.T) {
		t.Parallel()
		mock := &mockRemoteResolver{
			err: fmt.Errorf("network timeout"),
		}
		getter := NewGetter(WithRemoteResolver(mock))
		_, err := getter.Get(context.Background(), "ghcr.io/myorg/starter-kit@1.0.0")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to resolve remote reference")
		assert.Contains(t, err.Error(), "network timeout")
	})

	t.Run("falls through to file system without remote resolver", func(t *testing.T) {
		t.Parallel()
		// No remote resolver configured — should not panic, should fall through
		getter := NewGetter()
		_, err := getter.Get(context.Background(), "ghcr.io/myorg/starter-kit@1.0.0")
		require.Error(t, err)
		// Should be a file system error since no remote resolver and this isn't a file
	})
}

// mockBundleAwareCatalogResolver implements BundleAwareCatalogResolver for testing.
type mockBundleAwareCatalogResolver struct {
	solutions map[string][]byte
	bundles   map[string][]byte
	err       error
	fetchErr  error
	bundleErr error
}

func (m *mockBundleAwareCatalogResolver) FetchSolution(_ context.Context, nameWithVersion string) ([]byte, error) {
	if m.fetchErr != nil {
		return nil, m.fetchErr
	}
	if content, ok := m.solutions[nameWithVersion]; ok {
		return content, nil
	}
	return nil, fmt.Errorf("solution not found: %s", nameWithVersion)
}

func (m *mockBundleAwareCatalogResolver) FetchSolutionWithBundle(_ context.Context, nameWithVersion string) ([]byte, []byte, error) {
	if m.err != nil {
		return nil, nil, m.err
	}
	if m.bundleErr != nil {
		return nil, nil, m.bundleErr
	}
	content, ok := m.solutions[nameWithVersion]
	if !ok {
		return nil, nil, fmt.Errorf("solution not found: %s", nameWithVersion)
	}
	return content, m.bundles[nameWithVersion], nil
}

func TestGetter_fromCatalogWithBundle(t *testing.T) {
	t.Parallel()

	validSolutionYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: bundle-sol
  version: 1.0.0
spec:
  resolvers: {}
`

	t.Run("bundle-aware resolver returns content and bundle", func(t *testing.T) {
		t.Parallel()
		mock := &mockBundleAwareCatalogResolver{
			solutions: map[string][]byte{
				"bundle-sol@1.0.0": []byte(validSolutionYAML),
			},
			bundles: map[string][]byte{
				"bundle-sol@1.0.0": []byte("fake-bundle-tar"),
			},
		}

		getter := NewGetter(WithCatalogResolver(mock))
		sol, bundleData, err := getter.fromCatalogWithBundle(context.Background(), "bundle-sol@1.0.0")

		require.NoError(t, err)
		assert.Equal(t, "bundle-sol", sol.Metadata.Name)
		assert.Equal(t, []byte("fake-bundle-tar"), bundleData)
		assert.Equal(t, "catalog:bundle-sol@1.0.0", sol.GetPath())
	})

	t.Run("bundle-aware resolver returns content without bundle", func(t *testing.T) {
		t.Parallel()
		mock := &mockBundleAwareCatalogResolver{
			solutions: map[string][]byte{
				"no-bundle-sol": []byte(validSolutionYAML),
			},
		}

		getter := NewGetter(WithCatalogResolver(mock))
		sol, bundleData, err := getter.fromCatalogWithBundle(context.Background(), "no-bundle-sol")

		require.NoError(t, err)
		assert.NotNil(t, sol)
		assert.Nil(t, bundleData)
	})

	t.Run("bundle-aware resolver error", func(t *testing.T) {
		t.Parallel()
		mock := &mockBundleAwareCatalogResolver{
			err: fmt.Errorf("fetch failed"),
		}

		getter := NewGetter(WithCatalogResolver(mock))
		_, _, err := getter.fromCatalogWithBundle(context.Background(), "bad-sol")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "fetch failed")
	})

	t.Run("bundle-aware resolver returns invalid content", func(t *testing.T) {
		t.Parallel()
		mock := &mockBundleAwareCatalogResolver{
			solutions: map[string][]byte{
				"broken": []byte("not valid yaml: {{{"),
			},
		}

		getter := NewGetter(WithCatalogResolver(mock))
		_, _, err := getter.fromCatalogWithBundle(context.Background(), "broken")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse solution from catalog")
	})

	t.Run("falls back to basic resolver when not bundle-aware", func(t *testing.T) {
		t.Parallel()
		mock := &mockCatalogResolver{
			solutions: map[string][]byte{
				"basic-sol": []byte(validSolutionYAML),
			},
		}

		getter := NewGetter(WithCatalogResolver(mock))
		sol, bundleData, err := getter.fromCatalogWithBundle(context.Background(), "basic-sol")

		require.NoError(t, err)
		assert.Equal(t, "bundle-sol", sol.Metadata.Name) // name from the YAML content
		assert.Nil(t, bundleData)
		assert.Equal(t, "catalog:basic-sol", sol.GetPath())
	})

	t.Run("basic resolver error", func(t *testing.T) {
		t.Parallel()
		mock := &mockCatalogResolver{
			err: fmt.Errorf("catalog unavailable"),
		}

		getter := NewGetter(WithCatalogResolver(mock))
		_, _, err := getter.fromCatalogWithBundle(context.Background(), "bad-sol")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "catalog unavailable")
	})

	t.Run("basic resolver returns invalid content", func(t *testing.T) {
		t.Parallel()
		mock := &mockCatalogResolver{
			solutions: map[string][]byte{
				"broken": []byte("{{invalid}}"),
			},
		}

		getter := NewGetter(WithCatalogResolver(mock))
		_, _, err := getter.fromCatalogWithBundle(context.Background(), "broken")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse solution from catalog")
	})
}

func TestGetter_GetWithBundle_CatalogResolution(t *testing.T) {
	t.Parallel()

	validSolutionYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: cat-bundle-sol
  version: 1.0.0
spec:
  resolvers: {}
`

	t.Run("resolves bare name from bundle-aware catalog", func(t *testing.T) {
		t.Parallel()
		mock := &mockBundleAwareCatalogResolver{
			solutions: map[string][]byte{
				"cat-bundle-sol": []byte(validSolutionYAML),
			},
			bundles: map[string][]byte{
				"cat-bundle-sol": []byte("bundle-data"),
			},
		}

		getter := NewGetter(WithCatalogResolver(mock))
		sol, bundleData, err := getter.GetWithBundle(context.Background(), "cat-bundle-sol")

		require.NoError(t, err)
		assert.Equal(t, "cat-bundle-sol", sol.Metadata.Name)
		assert.Equal(t, []byte("bundle-data"), bundleData)
	})

	t.Run("versioned catalog lookup fails returns error directly", func(t *testing.T) {
		t.Parallel()
		mock := &mockBundleAwareCatalogResolver{
			err: fmt.Errorf("not found"),
		}

		getter := NewGetter(WithCatalogResolver(mock))
		_, _, err := getter.GetWithBundle(context.Background(), "missing@1.0.0")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("resolves URL path", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(validSolutionYAML))
		}))
		defer server.Close()

		cfg := httpc.DefaultConfig()
		cfg.EnableCache = false
		cfg.RetryMax = 0
		getter := NewGetter(WithHTTPClient(httpc.NewClient(cfg)))
		sol, bundleData, err := getter.GetWithBundle(context.Background(), server.URL)

		require.NoError(t, err)
		assert.NotNil(t, sol)
		assert.Nil(t, bundleData)
	})
}

func TestGetter_GetWithBundle_RemoteResolution(t *testing.T) {
	t.Parallel()

	validSolutionYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: remote-bundle-sol
  version: 1.0.0
spec:
  resolvers: {}
`

	t.Run("resolves remote ref with bundle", func(t *testing.T) {
		t.Parallel()
		mock := &mockRemoteResolver{
			solutions: map[string][]byte{
				"ghcr.io/myorg/starter-kit@1.0.0": []byte(validSolutionYAML),
			},
			bundleData: map[string][]byte{
				"ghcr.io/myorg/starter-kit@1.0.0": []byte("remote-bundle"),
			},
		}

		getter := NewGetter(WithRemoteResolver(mock))
		sol, bundleData, err := getter.GetWithBundle(context.Background(), "ghcr.io/myorg/starter-kit@1.0.0")

		require.NoError(t, err)
		assert.Equal(t, "remote-bundle-sol", sol.Metadata.Name)
		assert.Equal(t, []byte("remote-bundle"), bundleData)
	})

	t.Run("remote resolver error returns wrapped error", func(t *testing.T) {
		t.Parallel()
		mock := &mockRemoteResolver{
			err: fmt.Errorf("connection refused"),
		}

		getter := NewGetter(WithRemoteResolver(mock))
		_, _, err := getter.GetWithBundle(context.Background(), "ghcr.io/myorg/fail@1.0.0")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to resolve remote reference")
	})

	t.Run("remote resolver returns invalid content", func(t *testing.T) {
		t.Parallel()
		mock := &mockRemoteResolver{
			solutions: map[string][]byte{
				"ghcr.io/myorg/bad@1.0.0": []byte("{{invalid content}}"),
			},
		}

		getter := NewGetter(WithRemoteResolver(mock))
		_, _, err := getter.GetWithBundle(context.Background(), "ghcr.io/myorg/bad@1.0.0")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse solution from remote")
	})
}

func TestGetter_fromRemoteRefWithBundle(t *testing.T) {
	t.Parallel()

	validSolutionYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: remote-ref-sol
  version: 2.0.0
spec:
  resolvers: {}
`

	t.Run("success with bundle", func(t *testing.T) {
		t.Parallel()
		mock := &mockRemoteResolver{
			solutions: map[string][]byte{
				"ghcr.io/org/sol@2.0.0": []byte(validSolutionYAML),
			},
			bundleData: map[string][]byte{
				"ghcr.io/org/sol@2.0.0": []byte("tar-data"),
			},
		}

		getter := NewGetter(WithRemoteResolver(mock))
		sol, bundleData, err := getter.fromRemoteRefWithBundle(context.Background(), "ghcr.io/org/sol@2.0.0")

		require.NoError(t, err)
		assert.Equal(t, "remote-ref-sol", sol.Metadata.Name)
		assert.Equal(t, []byte("tar-data"), bundleData)
		assert.Equal(t, "remote:ghcr.io/org/sol@2.0.0", sol.GetPath())
	})

	t.Run("success without bundle", func(t *testing.T) {
		t.Parallel()
		mock := &mockRemoteResolver{
			solutions: map[string][]byte{
				"ghcr.io/org/sol@2.0.0": []byte(validSolutionYAML),
			},
		}

		getter := NewGetter(WithRemoteResolver(mock))
		sol, bundleData, err := getter.fromRemoteRefWithBundle(context.Background(), "ghcr.io/org/sol@2.0.0")

		require.NoError(t, err)
		assert.NotNil(t, sol)
		assert.Nil(t, bundleData)
	})

	t.Run("fetch error", func(t *testing.T) {
		t.Parallel()
		mock := &mockRemoteResolver{
			err: fmt.Errorf("auth denied"),
		}

		getter := NewGetter(WithRemoteResolver(mock))
		_, _, err := getter.fromRemoteRefWithBundle(context.Background(), "ghcr.io/org/sol@2.0.0")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "auth denied")
	})

	t.Run("invalid solution content", func(t *testing.T) {
		t.Parallel()
		mock := &mockRemoteResolver{
			solutions: map[string][]byte{
				"ghcr.io/org/bad@1.0.0": []byte("not yaml"),
			},
		}

		getter := NewGetter(WithRemoteResolver(mock))
		_, _, err := getter.fromRemoteRefWithBundle(context.Background(), "ghcr.io/org/bad@1.0.0")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse solution from remote")
	})
}

func TestGetter_Get_CombinedCatalogFileError(t *testing.T) {
	t.Parallel()

	// When catalog fails for a bare name (without @), the error should be the
	// catalog error directly -- bare names never fall back to file resolution.
	mock := &mockCatalogResolver{
		err: fmt.Errorf("catalog offline"),
	}

	getter := NewGetter(
		WithCatalogResolver(mock),
		WithReadFile(func(name string) ([]byte, error) {
			return nil, fmt.Errorf("file not found")
		}),
	)

	_, err := getter.Get(context.Background(), "my-solution")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "catalog offline")
}

func TestExtractNameVersionFromRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		ref      string
		expected string
	}{
		{"full ref with @version", "ghcr.io/myorg/solutions/hello-world@1.0.0", "hello-world@1.0.0"},
		{"full ref with :tag", "ghcr.io/myorg/solutions/hello-world:1.0.0", "hello-world@1.0.0"},
		{"oci:// prefix", "oci://ghcr.io/myorg/solutions/hello-world@2.0.0", "hello-world@2.0.0"},
		{"no version", "ghcr.io/myorg/solutions/hello-world", "hello-world"},
		{"two-segment ref", "ghcr.io/hello-world@1.0.0", "hello-world@1.0.0"},
		{"localhost with port", "localhost:5000/my-app@0.1.0", "my-app@0.1.0"},
		{"no slash", "hello-world@1.0.0", ""},
		{"empty", "", ""},
		{"trailing slash", "ghcr.io/myorg/solutions/@1.0.0", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := extractNameVersionFromRef(tc.ref)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestGetter_Get_RegistryRef_LocalFirst(t *testing.T) {
	t.Parallel()

	validSolutionYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: hello-world
  version: 1.0.0
spec:
  resolvers: {}
`

	t.Run("returns local catalog hit without calling remote", func(t *testing.T) {
		t.Parallel()

		catalogMock := &mockCatalogResolver{
			solutions: map[string][]byte{
				"hello-world@1.0.0": []byte(validSolutionYAML),
			},
		}
		remoteMock := &mockRemoteResolver{
			solutions: map[string][]byte{},
			err:       fmt.Errorf("should not be called"),
		}

		getter := NewGetter(
			WithCatalogResolver(catalogMock),
			WithRemoteResolver(remoteMock),
		)

		sol, err := getter.Get(context.Background(), "ghcr.io/myorg/solutions/hello-world@1.0.0")
		require.NoError(t, err)
		assert.Equal(t, "hello-world", sol.Metadata.Name)
	})

	t.Run("falls back to remote on local catalog miss", func(t *testing.T) {
		t.Parallel()

		catalogMock := &mockCatalogResolver{
			solutions: map[string][]byte{}, // empty — not in local catalog
		}
		remoteMock := &mockRemoteResolver{
			solutions: map[string][]byte{
				"ghcr.io/myorg/solutions/hello-world@1.0.0": []byte(validSolutionYAML),
			},
		}

		getter := NewGetter(
			WithCatalogResolver(catalogMock),
			WithRemoteResolver(remoteMock),
		)

		sol, err := getter.Get(context.Background(), "ghcr.io/myorg/solutions/hello-world@1.0.0")
		require.NoError(t, err)
		assert.Equal(t, "hello-world", sol.Metadata.Name)
	})

	t.Run("returns remote error when both miss", func(t *testing.T) {
		t.Parallel()

		catalogMock := &mockCatalogResolver{
			solutions: map[string][]byte{},
		}
		remoteMock := &mockRemoteResolver{
			solutions: map[string][]byte{},
		}

		getter := NewGetter(
			WithCatalogResolver(catalogMock),
			WithRemoteResolver(remoteMock),
		)

		_, err := getter.Get(context.Background(), "ghcr.io/myorg/solutions/hello-world@1.0.0")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to resolve remote reference")
	})
}

func TestGetter_GetWithBundle_RegistryRef_LocalFirst(t *testing.T) {
	t.Parallel()

	validSolutionYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: hello-world
  version: 1.0.0
spec:
  resolvers: {}
`

	t.Run("returns local catalog hit with bundle", func(t *testing.T) {
		t.Parallel()

		bundleMock := &mockBundleAwareCatalogResolver{
			solutions: map[string][]byte{"hello-world@1.0.0": []byte(validSolutionYAML)},
			bundles:   map[string][]byte{"hello-world@1.0.0": []byte("bundle-tar")},
		}
		remoteMock := &mockRemoteResolver{
			err: fmt.Errorf("should not be called"),
		}

		getter := NewGetter(
			WithCatalogResolver(bundleMock),
			WithRemoteResolver(remoteMock),
		)

		sol, bundleData, err := getter.GetWithBundle(context.Background(), "ghcr.io/myorg/solutions/hello-world@1.0.0")
		require.NoError(t, err)
		assert.Equal(t, "hello-world", sol.Metadata.Name)
		assert.Equal(t, []byte("bundle-tar"), bundleData)
	})

	t.Run("falls back to remote on local miss", func(t *testing.T) {
		t.Parallel()

		catalogMock := &mockCatalogResolver{
			solutions: map[string][]byte{},
		}
		remoteMock := &mockRemoteResolver{
			solutions: map[string][]byte{
				"ghcr.io/myorg/solutions/hello-world@1.0.0": []byte(validSolutionYAML),
			},
			bundleData: map[string][]byte{
				"ghcr.io/myorg/solutions/hello-world@1.0.0": []byte("remote-bundle"),
			},
		}

		getter := NewGetter(
			WithCatalogResolver(catalogMock),
			WithRemoteResolver(remoteMock),
		)

		sol, bundleData, err := getter.GetWithBundle(context.Background(), "ghcr.io/myorg/solutions/hello-world@1.0.0")
		require.NoError(t, err)
		assert.Equal(t, "hello-world", sol.Metadata.Name)
		assert.Equal(t, []byte("remote-bundle"), bundleData)
	})
}
