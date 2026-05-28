package middleware

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTokenPassthrough(t *testing.T) {
	t.Run("passes through token headers, no tokens", func(t *testing.T) {
		mw := TokenPassthrough([]string{"Github"})
		handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokens := TokensFromContext(r.Context())
			assert.Empty(t, tokens, "expected no tokens in context when no headers are set")
		}))

		req := httptest.NewRequestWithContext(context.Background(), "GET", "/test", nil)
		handler.ServeHTTP(httptest.NewRecorder(), req)
	})

	t.Run("passes through token headers, with tokens", func(t *testing.T) {
		mw := TokenPassthrough([]string{"Github"})
		handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokens := TokensFromContext(r.Context())
			expected := map[string]string{
				"Github": "ghp_123456",
			}

			assert.Equal(t, expected, tokens, "expected tokens in context to match headers")
		}))

		req := httptest.NewRequestWithContext(context.Background(), "GET", "/test", nil)
		req.Header.Set(fmt.Sprintf("%sGithub", TokenHeaderPrefix), "ghp_123456")
		handler.ServeHTTP(httptest.NewRecorder(), req)
	})

	t.Run("ignores if not in allowed headers", func(t *testing.T) {
		mw := TokenPassthrough([]string{"Github"})
		handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokens := TokensFromContext(r.Context())
			assert.Empty(t, tokens, "expected unconfigured token header to be ignored")
		}))

		req := httptest.NewRequestWithContext(context.Background(), "GET", "/test", nil)
		req.Header.Set(fmt.Sprintf("%sAzure-Ad", TokenHeaderPrefix), "value")
		handler.ServeHTTP(httptest.NewRecorder(), req)
	})

	t.Run("canonicalizes configured header suffixes", func(t *testing.T) {
		mw := TokenPassthrough([]string{"Github"})
		handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokens := TokensFromContext(r.Context())
			assert.Equal(t, map[string]string{
				"Github": "ghp_123456",
			}, tokens)
		}))

		req := httptest.NewRequestWithContext(context.Background(), "GET", "/test", nil)
		req.Header.Set(fmt.Sprintf("%sgithub", TokenHeaderPrefix), "ghp_123456")
		handler.ServeHTTP(httptest.NewRecorder(), req)
	})

	t.Run("ignores invalid configured header suffixes", func(t *testing.T) {
		mw := TokenPassthrough([]string{"Bad Header", ""})
		handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokens := TokensFromContext(r.Context())
			assert.Empty(t, tokens, "expected invalid configured token headers to be ignored")
		}))

		req := httptest.NewRequestWithContext(context.Background(), "GET", "/test", nil)
		req.Header.Set(fmt.Sprintf("%sBad-Header", TokenHeaderPrefix), "value")
		req.Header.Set(TokenHeaderPrefix, "value")
		handler.ServeHTTP(httptest.NewRecorder(), req)
	})

	t.Run("keeps token values unchanged", func(t *testing.T) {
		mw := TokenPassthrough([]string{"Github"})
		handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokens := TokensFromContext(r.Context())
			assert.Equal(t, "  token with spaces  ", tokens["Github"])
		}))

		req := httptest.NewRequestWithContext(context.Background(), "GET", "/test", nil)
		req.Header.Set(fmt.Sprintf("%sGithub", TokenHeaderPrefix), "  token with spaces  ")
		handler.ServeHTTP(httptest.NewRecorder(), req)
	})
}
