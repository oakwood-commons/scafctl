package config

import (
	"net/http"
	"strings"
)

// DefaultTokenPassThroughAllowedHeaders returns the default token pass-through
// header suffixes used when apiServer.tokenPassThrough is omitted.
func DefaultTokenPassThroughAllowedHeaders() []string {
	return []string{"Github"}
}

// TokenPassThroughAllowedHeaders returns the configured token pass-through
// header suffixes. A nil TokenPassThrough config uses the default GitHub
// suffix; a non-nil config returns its normalized list, including an empty list.
func (c *APIServerConfig) TokenPassThroughAllowedHeaders() []string {
	if c == nil || c.TokenPassThrough == nil {
		return DefaultTokenPassThroughAllowedHeaders()
	}
	seen := make(map[string]struct{})
	headers := make([]string, 0, len(c.TokenPassThrough.AllowedHeaders))
	for _, header := range c.TokenPassThrough.AllowedHeaders {
		canonicalHeader := http.CanonicalHeaderKey(strings.TrimSpace(header))
		if canonicalHeader == "" {
			continue
		}
		if _, exists := seen[canonicalHeader]; !exists {
			seen[canonicalHeader] = struct{}{}
			headers = append(headers, canonicalHeader)
		}
	}
	return headers
}
