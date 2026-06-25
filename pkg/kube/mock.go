// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"context"
	"sync"
)

// MockResolver implements ClusterResolver for testing. Configure the result
// values or override the *Func fields for custom behavior.
type MockResolver struct {
	mu sync.Mutex

	ResolveResult *ClusterInfo
	ResolveErr    error
	ListResult    []ClusterInfo
	ListErr       error

	// ResolveFunc, when set, takes precedence over ResolveResult/ResolveErr.
	ResolveFunc func(ctx context.Context, name string) (*ClusterInfo, error)
	// ListFunc, when set, takes precedence over ListResult/ListErr.
	ListFunc func(ctx context.Context) ([]ClusterInfo, error)

	// ResolveCalls records the names passed to Resolve, in order.
	ResolveCalls []string
	// ListCalls counts the number of List invocations.
	ListCalls int
}

// Resolve returns the configured cluster info or invokes ResolveFunc.
func (m *MockResolver) Resolve(ctx context.Context, name string) (*ClusterInfo, error) {
	m.mu.Lock()
	m.ResolveCalls = append(m.ResolveCalls, name)
	fn := m.ResolveFunc
	result := m.ResolveResult
	err := m.ResolveErr
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, name)
	}
	return result, err
}

// List returns the configured clusters or invokes ListFunc.
func (m *MockResolver) List(ctx context.Context) ([]ClusterInfo, error) {
	m.mu.Lock()
	m.ListCalls++
	fn := m.ListFunc
	result := m.ListResult
	err := m.ListErr
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx)
	}
	return result, err
}
