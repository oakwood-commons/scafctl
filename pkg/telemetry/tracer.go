// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package telemetry

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// Tracer name constants. Each subsystem uses its own named tracer so spans
// are grouped by instrumentation scope in backend UIs like Jaeger.
const (
	// TracerRoot is the root instrumentation scope for the scafctl binary.
	TracerRoot = "github.com/oakwood-commons/scafctl"
	// TracerHTTPClient is the instrumentation scope for the HTTP client.
	TracerHTTPClient = TracerRoot + "/httpc"
	// TracerProvider is the instrumentation scope for provider execution.
	TracerProvider = TracerRoot + "/provider"
	// TracerResolver is the instrumentation scope for resolver evaluation.
	TracerResolver = TracerRoot + "/resolver"
	// TracerSolution is the instrumentation scope for solution runs.
	TracerSolution = TracerRoot + "/solution"
	// TracerAction is the instrumentation scope for action execution.
	TracerAction = TracerRoot + "/action"
	// TracerMCP is the instrumentation scope for MCP server handling.
	TracerMCP = TracerRoot + "/mcp"
)

// Tracer returns a named tracer from the global TracerProvider.
// Use the TracerXxx constants for the name argument.
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}
