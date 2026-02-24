// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package telemetry

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
)

func TestSetup_NoEndpoint(t *testing.T) {
	ctx := context.Background()
	shutdown, err := Setup(ctx, Options{
		ServiceName:    "test-service",
		ServiceVersion: "0.0.0",
	})
	if err != nil {
		t.Fatalf("Setup() returned unexpected error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("Setup() returned nil shutdown function")
	}
	shutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := shutdown(shutCtx); err != nil {
		t.Errorf("shutdown() returned unexpected error: %v", err)
	}
}

func TestSetup_TracerProviderRegistered(t *testing.T) {
	ctx := context.Background()
	shutdown, err := Setup(ctx, Options{ServiceName: "test-service"})
	if err != nil {
		t.Fatalf("Setup() error: %v", err)
	}
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
	if err != nil {
		t.Fatalf("Setup() with empty options returned error: %v", err)
	}
	shutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_ = shutdown(shutCtx)
}
