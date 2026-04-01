// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package endpoints

import (
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/oakwood-commons/scafctl/pkg/api"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// Standard error sets for reuse across endpoints.
var (
	// authenticatedGETErrors are the standard error codes for authenticated GET endpoints.
	authenticatedGETErrors = []int{
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
	}

	// authenticatedPOSTErrors are the standard error codes for authenticated POST endpoints.
	authenticatedPOSTErrors = []int{
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusUnprocessableEntity,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
	}

	// publicErrors are the standard error codes for unauthenticated endpoints (health, probes).
	publicErrors = []int{
		http.StatusInternalServerError,
		http.StatusServiceUnavailable,
	}

	// oauthSecurity references the "oauth2" scheme registered in huma.go.
	oauthSecurity = []map[string][]string{{"oauth2": {}}}

	// noSecurity marks an endpoint as publicly accessible (no auth required).
	noSecurity = []map[string][]string{{}}
)

// withDefaults applies standard operation defaults for authenticated endpoints.
// It sets DefaultStatus, MaxBodyBytes, BodyReadTimeout, Errors, and Security
// using values from configuration with fallback to settings constants.
// Explicitly set fields on the operation are preserved.
func withDefaults(op huma.Operation, hctx *api.HandlerContext, responseStatus int) huma.Operation { //nolint:unparam // responseStatus passes through to addExamples for future non-200 defaults
	op = addExamples(op, responseStatus)
	applyCommonDefaults(&op, hctx, responseStatus)

	if op.Security == nil {
		if hctx != nil && hctx.Config != nil && hctx.Config.APIServer.Auth.AzureOIDC.Enabled {
			op.Security = oauthSecurity
		} else {
			op.Security = noSecurity
		}
	}

	if op.Errors == nil {
		if op.Method == http.MethodGet {
			op.Errors = authenticatedGETErrors
		} else {
			op.Errors = authenticatedPOSTErrors
		}
	}

	return op
}

// withPublicDefaults applies standard operation defaults for public endpoints
// (health, probes, root). No Security requirement is set.
func withPublicDefaults(op huma.Operation, hctx *api.HandlerContext, responseStatus int) huma.Operation { //nolint:unparam // responseStatus passes through to addExamples for future non-200 defaults
	op = addExamples(op, responseStatus)
	applyCommonDefaults(&op, hctx, responseStatus)

	if op.Security == nil {
		op.Security = noSecurity
	}

	if op.Errors == nil {
		op.Errors = publicErrors
	}

	return op
}

// applyCommonDefaults sets DefaultStatus, MaxBodyBytes, and BodyReadTimeout
// from config or settings defaults. Explicitly set fields are preserved.
func applyCommonDefaults(op *huma.Operation, hctx *api.HandlerContext, responseStatus int) {
	if op.DefaultStatus == 0 {
		op.DefaultStatus = responseStatus
	}

	if op.MaxBodyBytes == 0 {
		op.MaxBodyBytes = settings.DefaultAPIOperationMaxBodyBytes
	}

	if op.BodyReadTimeout == 0 {
		op.BodyReadTimeout = parseBodyReadTimeout(hctx)
	}
}

// parseBodyReadTimeout resolves the body read timeout from config or default.
func parseBodyReadTimeout(hctx *api.HandlerContext) time.Duration {
	if hctx != nil && hctx.Config != nil && hctx.Config.APIServer.BodyReadTimeout != "" {
		if d, err := time.ParseDuration(hctx.Config.APIServer.BodyReadTimeout); err == nil {
			return d
		}
	}
	d, _ := time.ParseDuration(settings.DefaultAPIBodyReadTimeout)
	return d
}
