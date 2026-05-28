package authdelegation

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/api/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPassThroughDelegator(t *testing.T) {
	t.Parallel()
	t.Run("returns token for configured provider", func(t *testing.T) {
		t.Parallel()
		delegator, err := NewPassThroughDelegator("Github")
		require.NoError(t, err)

		handler := middleware.TokenPassthrough([]string{"Github"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			result, err := delegator.DelegateToken(r.Context(), "ignored-scope")

			require.NoError(t, err)
			assert.Equal(t, TokenResult{AccessToken: "ghp_123456"}, result)
		}))

		req := httptest.NewRequestWithContext(context.Background(), "GET", "/test", nil)
		req.Header.Set(fmt.Sprintf("%s%s", middleware.TokenHeaderPrefix, "Github"), "ghp_123456")
		handler.ServeHTTP(httptest.NewRecorder(), req)
	})

	t.Run("constructor rejects empty provider", func(t *testing.T) {
		t.Parallel()
		delegator, err := NewPassThroughDelegator("")

		require.Error(t, err)
		assert.Nil(t, delegator)
		assert.Contains(t, err.Error(), "provider is required")
	})

	t.Run("returns error when token is missing from context", func(t *testing.T) {
		t.Parallel()
		delegator, err := NewPassThroughDelegator("Github")
		require.NoError(t, err)

		handler := middleware.TokenPassthrough([]string{"Github"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			result, err := delegator.DelegateToken(r.Context(), "")

			require.Error(t, err)
			assert.Empty(t, result.AccessToken)
			assert.Contains(t, err.Error(), `token for provider "Github" not found`)
		}))

		req := httptest.NewRequestWithContext(context.Background(), "GET", "/test", nil)
		handler.ServeHTTP(httptest.NewRecorder(), req)
	})

	t.Run("does not return a different provider token", func(t *testing.T) {
		t.Parallel()
		delegator, err := NewPassThroughDelegator("Azure-Ad")
		require.NoError(t, err)

		handler := middleware.TokenPassthrough([]string{"Github", "Azure-Ad"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			result, err := delegator.DelegateToken(r.Context(), "")

			require.Error(t, err)
			assert.Empty(t, result.AccessToken)
			assert.Contains(t, err.Error(), `token for provider "Azure-Ad" not found`)
		}))

		req := httptest.NewRequestWithContext(context.Background(), "GET", "/test", nil)
		req.Header.Set(fmt.Sprintf("%s%s", middleware.TokenHeaderPrefix, "Github"), "ghp_123456")
		handler.ServeHTTP(httptest.NewRecorder(), req)
	})
}
