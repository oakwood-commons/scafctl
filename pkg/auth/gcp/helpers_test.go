// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gcp

import (
	"context"
	"net/http"
	"testing"
)

// newTestRequest creates a test HTTP request.
func newTestRequest(t *testing.T) (*http.Request, error) {
	t.Helper()
	return http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com/api", nil)
}
