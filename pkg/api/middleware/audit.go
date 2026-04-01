// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-logr/logr"
)

const maxAuditBodySize = 4096

// sensitiveKeys are JSON field names whose values must be redacted from audit logs.
var sensitiveKeys = []string{
	"password",
	"secret",
	"token",
	"key",
	"credential",
	"authorization",
}

// AuditLog returns middleware that writes structured audit log entries.
// Request bodies for mutation methods are captured and redacted before logging
// to prevent sensitive data (passwords, tokens, secrets) from leaking into logs.
// Set trustProxy true only when a trusted reverse proxy sanitizes X-Forwarded-For
// and X-Real-IP; leave false (the safe default) to use RemoteAddr for the audit
// source IP and prevent clients from spoofing their identity in audit logs.
func AuditLog(lgr logr.Logger, trustProxy bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

			// Capture request body summary for mutations
			var bodySummary string
			if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
				bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, maxAuditBodySize))
				if err == nil && len(bodyBytes) > 0 {
					bodySummary = redactBody(bodyBytes)
					// Restore the body for downstream handlers
					r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(bodyBytes), r.Body))
				}
			}

			next.ServeHTTP(sw, r)

			duration := time.Since(start)

			// Extract caller identity from auth claims
			caller := "anonymous"
			if claims := ClaimsFromContext(r.Context()); claims != nil {
				switch {
				case claims.Email != "":
					caller = claims.Email
				case claims.Name != "":
					caller = claims.Name
				case claims.Subject != "":
					caller = claims.Subject
				}
			}

			auditFields := []any{
				"caller", caller,
				"sourceIP", extractIP(r, trustProxy),
				"requestID", r.Header.Get("X-Request-ID"),
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"duration", duration.String(),
			}

			if bodySummary != "" {
				auditFields = append(auditFields, "body", bodySummary)
			}

			lgr.Info("audit", auditFields...)
		})
	}
}

// redactBody attempts to parse the body as JSON and redact sensitive fields.
// Non-JSON bodies are fully redacted to prevent leaking secrets in
// form-encoded or plaintext payloads.
func redactBody(body []byte) string {
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return "<non-JSON body redacted>"
	}
	redactMap(data)
	redacted, err := json.Marshal(data)
	if err != nil {
		return string(body)
	}
	return string(redacted)
}

// redactMap recursively redacts sensitive keys in a JSON object.
func redactMap(m map[string]any) {
	for k, v := range m {
		if isSensitiveKey(k) {
			m[k] = "[REDACTED]"
			continue
		}
		switch val := v.(type) {
		case map[string]any:
			redactMap(val)
		case []any:
			for _, item := range val {
				if nested, ok := item.(map[string]any); ok {
					redactMap(nested)
				}
			}
		}
	}
}

// isSensitiveKey returns true if the key matches a known sensitive field name.
func isSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, s := range sensitiveKeys {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}

// RedactJSON round-trips v through JSON and redacts sensitive field values in
// any map keys that match the sensitiveKeys list (e.g. password, secret, token).
// Arrays of objects are recursively redacted. Non-JSON-serializable values are
// returned unchanged. Intended for use in API response sanitization.
func RedactJSON(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return v
	}
	redactAny(out)
	return out
}

// redactAny applies sensitive-field redaction to any JSON-decoded value.
func redactAny(v any) {
	switch val := v.(type) {
	case map[string]any:
		redactMap(val)
	case []any:
		for _, item := range val {
			redactAny(item)
		}
	}
}
