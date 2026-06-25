// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ensure MockResolver satisfies the interface.
var _ ClusterResolver = (*MockResolver)(nil)

func TestMockResolver_Results(t *testing.T) {
	t.Parallel()

	want := &ClusterInfo{Name: "prod", APIServerURL: "https://api.example.com"}
	m := &MockResolver{
		ResolveResult: want,
		ListResult:    []ClusterInfo{*want},
	}

	got, err := m.Resolve(context.Background(), "prod")
	require.NoError(t, err)
	assert.Same(t, want, got)

	list, err := m.List(context.Background())
	require.NoError(t, err)
	assert.Len(t, list, 1)

	assert.Equal(t, []string{"prod"}, m.ResolveCalls)
	assert.Equal(t, 1, m.ListCalls)
}

func TestMockResolver_Errors(t *testing.T) {
	t.Parallel()

	resolveErr := errors.New("resolve boom")
	listErr := errors.New("list boom")
	m := &MockResolver{ResolveErr: resolveErr, ListErr: listErr}

	_, err := m.Resolve(context.Background(), "x")
	assert.ErrorIs(t, err, resolveErr)

	_, err = m.List(context.Background())
	assert.ErrorIs(t, err, listErr)
}

func TestMockResolver_Funcs(t *testing.T) {
	t.Parallel()

	m := &MockResolver{
		ResolveFunc: func(_ context.Context, name string) (*ClusterInfo, error) {
			return &ClusterInfo{Name: name}, nil
		},
		ListFunc: func(_ context.Context) ([]ClusterInfo, error) {
			return []ClusterInfo{{Name: "a"}, {Name: "b"}}, nil
		},
	}

	got, err := m.Resolve(context.Background(), "dynamic")
	require.NoError(t, err)
	assert.Equal(t, "dynamic", got.Name)

	list, err := m.List(context.Background())
	require.NoError(t, err)
	assert.Len(t, list, 2)
}
