// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package telemetry

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
)

func TestSetup_NoEndpoint(t *testing.T) {
	ctx := context.Background()
	shutdown, err := Setup(ctx, Options{
		ServiceName:    "test-service",
		ServiceVersion: "0.0.0",
	})
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	shutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	require.NoError(t, shutdown(shutCtx))
}

func TestSetup_TracerProviderRegistered(t *testing.T) {
	ctx := context.Background()
	shutdown, err := Setup(ctx, Options{ServiceName: "test-service"})
	require.NoError(t, err)
	defer func() {
		shutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_ = shutdown(shutCtx)
	}()

	tracer := otel.Tracer(TracerRoot)
	_, span := tracer.Start(ctx, "test-span")
	span.End()
}

func TestSetup_DefaultServiceName(t *testing.T) {
	ctx := context.Background()
	shutdown, err := Setup(ctx, Options{})
	require.NoError(t, err)

	shutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_ = shutdown(shutCtx)
}

// TestSetup_NoEndpoint_NoStderrOutput verifies that when no OTLP endpoint is
// configured, Setup does not create stdout/stderr fallback exporters. This
// prevents JSON span data from being dumped to the terminal during normal use.
func TestSetup_NoEndpoint_NoStderrOutput(t *testing.T) {
	// Capture stderr
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w

	ctx := context.Background()
	shutdown, err := Setup(ctx, Options{
		ServiceName:    "test-stderr",
		ServiceVersion: "0.0.0",
	})
	require.NoError(t, err)

	// Create and end a span to trigger any exporter
	tracer := otel.Tracer(TracerRoot)
	_, span := tracer.Start(ctx, "test-span-no-stderr")
	span.End()

	// Shutdown flushes any buffered exports
	shutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	require.NoError(t, shutdown(shutCtx))

	// Close write end and read all output
	w.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	os.Stderr = origStderr

	assert.Empty(t, buf.String(), "no stderr output expected when no OTLP endpoint is configured")
}

func TestSetup_SamplerType_AlwaysOff(t *testing.T) {
	ctx := context.Background()
	shutdown, err := Setup(ctx, Options{
		ServiceName: "test-sampler",
		SamplerType: "always_off",
	})
	require.NoError(t, err)
	defer func() {
		shutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_ = shutdown(shutCtx)
	}()

	// Should not panic — spans are created but not sampled
	tracer := otel.Tracer(TracerRoot)
	_, span := tracer.Start(ctx, "test-span-always-off")
	assert.False(t, span.SpanContext().IsSampled(), "span should not be sampled with always_off")
	span.End()
}

func TestSetup_SamplerType_TraceIDRatio(t *testing.T) {
	ctx := context.Background()
	shutdown, err := Setup(ctx, Options{
		ServiceName: "test-sampler",
		SamplerType: "traceidratio",
		SamplerArg:  0.5,
	})
	require.NoError(t, err)

	shutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	require.NoError(t, shutdown(shutCtx))
}

func TestSetup_SamplerType_Invalid(t *testing.T) {
	ctx := context.Background()
	_, err := Setup(ctx, Options{
		ServiceName: "test-sampler",
		SamplerType: "bogus",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown sampler type")
}

func TestSetup_SamplerType_TraceIDRatio_InvalidArg(t *testing.T) {
	ctx := context.Background()
	_, err := Setup(ctx, Options{
		ServiceName: "test-sampler",
		SamplerType: "traceidratio",
		SamplerArg:  2.0,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "between 0.0 and 1.0")
}

func TestParseSampler(t *testing.T) {
	tests := []struct {
		name        string
		samplerType string
		samplerArg  float64
		wantErr     bool
	}{
		{"empty defaults to always_on", "", 0, false},
		{"always_on", "always_on", 0, false},
		{"always_off", "always_off", 0, false},
		{"traceidratio valid", "traceidratio", 0.5, false},
		{"traceidratio zero", "traceidratio", 0.0, false},
		{"traceidratio one", "traceidratio", 1.0, false},
		{"traceidratio negative", "traceidratio", -0.1, true},
		{"traceidratio over one", "traceidratio", 1.1, true},
		{"unknown type", "random_sampler", 0, true},
		{"case insensitive", "ALWAYS_ON", 0, false},
		{"whitespace trimmed", "  always_off  ", 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sampler, err := parseSampler(tc.samplerType, tc.samplerArg)
			if tc.wantErr {
				assert.Error(t, err)
				assert.Nil(t, sampler)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, sampler)
			}
		})
	}
}
