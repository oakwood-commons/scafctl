// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// HandlerContext provides shared dependencies to all API handlers.
// All handlers access scafctl domain packages through this struct.
type HandlerContext struct {
	Config           *config.Config
	ProviderRegistry *provider.Registry
	AuthRegistry     *auth.Registry
	Logger           logr.Logger
	IsShuttingDown   *int32
	StartTime        time.Time
}

// NewHandlerContext creates a new HandlerContext with the given dependencies.
func NewHandlerContext(
	cfg *config.Config,
	providerReg *provider.Registry,
	authReg *auth.Registry,
	logger logr.Logger,
	isShuttingDown *int32,
	startTime time.Time,
) *HandlerContext {
	return &HandlerContext{
		Config:           cfg,
		ProviderRegistry: providerReg,
		AuthRegistry:     authReg,
		Logger:           logger,
		IsShuttingDown:   isShuttingDown,
		StartTime:        startTime,
	}
}

// ShuttingDown returns true if the server is in graceful shutdown.
// Returns false when IsShuttingDown is nil (e.g., for export/spec contexts).
func (hc *HandlerContext) ShuttingDown() bool {
	if hc.IsShuttingDown == nil {
		return false
	}
	return atomic.LoadInt32(hc.IsShuttingDown) == 1
}
