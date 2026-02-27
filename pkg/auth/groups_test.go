// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// MockGroupsProvider implements GroupsProvider for testing.
type MockGroupsProvider struct {
	Groups []string
	Err    error
}

func (m *MockGroupsProvider) GetGroups(_ context.Context) ([]string, error) {
	return m.Groups, m.Err
}

// Verify MockGroupsProvider satisfies the GroupsProvider interface at compile time.
var _ GroupsProvider = (*MockGroupsProvider)(nil)

func TestGroupsProvider_InterfaceSatisfaction(t *testing.T) {
	var provider GroupsProvider = &MockGroupsProvider{
		Groups: []string{"group-1", "group-2"},
	}
	groups, err := provider.GetGroups(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, []string{"group-1", "group-2"}, groups)
}
