// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpc

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/oakwood-commons/scafctl/pkg/metrics"
)

// metricsTransport is an http.RoundTripper that records Prometheus metrics for HTTP requests
type metricsTransport struct {
	base http.RoundTripper
}

// newMetricsTransport creates a new metrics transport that wraps the base transport
func newMetricsTransport(base http.RoundTripper) *metricsTransport {
	return &metricsTransport{
		base: base,
	}
}

// RoundTrip implements http.RoundTripper and records metrics for each request
func (t *metricsTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()

	// Extract labels for metrics (method, host, and parameterized path template)
	method := req.Method
	host, pathTemplate := extractMetricLabels(req.URL)

	ctx := req.Context()
	baseAttrs := []attribute.KeyValue{
		attribute.String(metrics.AttrMethod, method),
		attribute.String(metrics.LabelHost, host),
		attribute.String(metrics.LabelPathTemplate, pathTemplate),
	}

	// Track request size if body is present
	if req.Body != nil && req.ContentLength > 0 && metrics.HTTPClientRequestSize != nil {
		metrics.HTTPClientRequestSize.Record(ctx, float64(req.ContentLength),
			metric.WithAttributes(baseAttrs...))
	}

	// Execute the request
	resp, err := t.base.RoundTrip(req)

	// Calculate duration
	duration := time.Since(start).Seconds()

	// Determine status code
	statusCode := "error"
	if resp != nil {
		statusCode = strconv.Itoa(resp.StatusCode)
		// Track response size if available
		if resp.ContentLength > 0 && metrics.HTTPClientResponseSize != nil {
			metrics.HTTPClientResponseSize.Record(ctx, float64(resp.ContentLength),
				metric.WithAttributes(baseAttrs...))
		}
	}

	statusAttrs := append(baseAttrs, attribute.String(metrics.AttrStatusCode, statusCode)) //nolint:gocritic

	// Record request duration histogram
	if metrics.HTTPClientDuration != nil {
		metrics.HTTPClientDuration.Record(ctx, duration, metric.WithAttributes(statusAttrs...))
	}

	// Record request counter
	if metrics.HTTPClientRequestsTotal != nil {
		metrics.HTTPClientRequestsTotal.Add(ctx, 1, metric.WithAttributes(statusAttrs...))
	}

	// Record errors if present
	if err != nil && metrics.HTTPClientErrorsTotal != nil {
		errorType := categorizeError(err, resp)
		metrics.HTTPClientErrorsTotal.Add(ctx, 1,
			metric.WithAttributes(append(baseAttrs, attribute.String(metrics.AttrErrorType, errorType))...))
	}

	return resp, err
}

// categorizeError categorizes an HTTP error into a specific error type
// Priority order: HTTP errors (4xx/5xx), context errors, network errors
func categorizeError(err error, resp *http.Response) string {
	if err == nil {
		return "none"
	}

	// Check for HTTP-level errors first (4xx/5xx from response)
	if resp != nil {
		statusCode := resp.StatusCode
		if statusCode >= 400 && statusCode < 500 {
			return "client_error"
		}
		if statusCode >= 500 {
			return "server_error"
		}
	}

	// Check for context errors
	if errors.Is(err, context.Canceled) {
		return "context_canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "context_timeout"
	}

	// Check for network timeout errors
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "network_timeout"
	}

	// Check for connection refused errors
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if errors.Is(opErr.Err, syscall.ECONNREFUSED) {
			return "connection_refused"
		}
	}

	// Check for DNS errors
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "dns_error"
	}

	// Default to unknown error
	return "unknown"
}

var (
	// Tier 1 parameterization patterns (applied in order from most to least specific)
	uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	shaPattern  = regexp.MustCompile(`^[0-9a-f]{40,64}$`)
	intPattern  = regexp.MustCompile(`^\d+$`)
)

// parameterizePath applies Tier 1 parameterization patterns to URL path segments.
// It replaces UUIDs with {id}, SHA hashes with {hash}, and integers with {id}.
// This reduces cardinality while preserving route structure.
func parameterizePath(path string) string {
	if path == "" || path == "/" {
		return path
	}

	segments := strings.Split(strings.Trim(path, "/"), "/")
	for i, segment := range segments {
		// Skip empty segments
		if segment == "" {
			continue
		}

		// Apply patterns in order: UUID -> SHA hash -> integer
		switch {
		case uuidPattern.MatchString(segment):
			segments[i] = "{id}"
		case shaPattern.MatchString(segment):
			segments[i] = "{hash}"
		case intPattern.MatchString(segment):
			segments[i] = "{id}"
		}
	}

	return "/" + strings.Join(segments, "/")
}

// extractMetricLabels extracts host and path_template from a URL for metric labels.
// Host includes non-standard ports (omits 80 for http and 443 for https).
// Path is parameterized using Tier 1 patterns. Query parameters are stripped.
func extractMetricLabels(u *url.URL) (host, pathTemplate string) {
	// Extract host with non-standard ports
	host = u.Hostname()
	port := u.Port()

	// Include port only if non-standard
	if port != "" {
		scheme := u.Scheme
		if (scheme == "http" && port != "80") || (scheme == "https" && port != "443") {
			host = host + ":" + port
		}
	}

	// Parameterize path (query parameters already stripped by u.Path)
	pathTemplate = parameterizePath(u.Path)

	return host, pathTemplate
}
