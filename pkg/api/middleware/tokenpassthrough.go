package middleware

import (
	"context"
	"net/http"
	"strings"
)

const (
	TokenHeaderPrefix            = "X-Authorization-"
	tokenContextKey   contextKey = "headerTokens"
)

func TokenPassthrough(allowedTokenHeaderSuffixes []string) func(http.Handler) http.Handler {
	allowedTokenHeaders := allowedProviderTokenHeaders(allowedTokenHeaderSuffixes)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokens := make(map[string]string)
			for _, key := range allowedTokenHeaders {
				if token := r.Header.Get(key); token != "" {
					tokens[strings.TrimPrefix(key, TokenHeaderPrefix)] = token
				}
			}
			ctx := withContextTokens(r.Context(), tokens)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func allowedProviderTokenHeaders(suffixes []string) []string {
	headers := make([]string, 0, len(suffixes))
	for _, suffix := range suffixes {
		suffix = strings.TrimSpace(suffix)
		if suffix == "" {
			continue
		}

		header := http.CanonicalHeaderKey(TokenHeaderPrefix + suffix)
		if !strings.HasPrefix(header, TokenHeaderPrefix) {
			continue
		}
		if !validHeaderFieldName(header) {
			continue
		}

		headers = append(headers, header)
	}
	return headers
}

func validHeaderFieldName(name string) bool {
	if name == "" {
		return false
	}
	for i := range len(name) {
		c := name[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
			continue
		}
		switch c {
		case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
			continue
		default:
			return false
		}
	}
	return true
}

func withContextTokens(ctx context.Context, tokens map[string]string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, tokenContextKey, tokens)
}

func TokensFromContext(ctx context.Context) map[string]string {
	tokens, _ := ctx.Value(tokenContextKey).(map[string]string)
	return tokens
}
