package get

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/httpc"
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
		"name": "test-solution",
		"version": "1.0.0"
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

		getter := NewGetter()
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
		assert.Contains(t, err.Error(), "Failed unmarshalling data")
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
		getter := NewGetter(WithLogger(customLogger))
		ctx := context.Background()

		sol, err := getter.FromURL(ctx, server.URL)
		require.NoError(t, err)
		require.NotNil(t, sol)
	})
}

func TestFromLocalFileSystem(t *testing.T) {
	validSolutionJSON := `{
		"name": "test-solution",
		"version": "1.0.0"
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
		assert.Contains(t, err.Error(), "Failed unmarshalling data")
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
		"name": "test-solution",
		"version": "1.0.0"
	}`

	t.Run("with explicit URL path", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(validSolutionJSON))
		}))
		defer server.Close()

		getter := NewGetter()
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
		// Note: empty folder creates paths like "/solution.yaml"
		expectedPaths := []string{
			"scafctl/solution.yaml",
			"scafctl/solution.yml",
			".scafctl/solution.yaml",
			"/solution.yaml", // Empty folder + filename
			"scafctl/scafctl.yaml",
			"/solution.json", // Empty folder + filename
		}

		for _, expected := range expectedPaths {
			assert.Contains(t, paths, expected, "Should contain path: %s", expected)
		}
	})
}
