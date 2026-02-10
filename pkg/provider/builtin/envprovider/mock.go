// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package envprovider

import (
	"fmt"
	"sync"
)

// MockEnvOps provides a mock implementation for testing
type MockEnvOps struct {
	mu   sync.RWMutex
	vars map[string]string

	// Error injection for testing error scenarios
	LookupEnvErr bool
	SetenvErr    bool
	UnsetenvErr  bool
	EnvironErr   bool
}

// NewMockEnvOps creates a new mock environment operations
func NewMockEnvOps() *MockEnvOps {
	return &MockEnvOps{
		vars: make(map[string]string),
	}
}

// LookupEnv looks up an environment variable in the mock store
func (m *MockEnvOps) LookupEnv(key string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	value, exists := m.vars[key]
	return value, exists
}

// Setenv sets an environment variable in the mock store
func (m *MockEnvOps) Setenv(key, value string) error {
	if m.SetenvErr {
		return fmt.Errorf("mock setenv error")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.vars[key] = value
	return nil
}

// Unsetenv removes an environment variable from the mock store
func (m *MockEnvOps) Unsetenv(key string) error {
	if m.UnsetenvErr {
		return fmt.Errorf("mock unsetenv error")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.vars, key)
	return nil
}

// Environ returns all environment variables from the mock store
func (m *MockEnvOps) Environ() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]string, 0, len(m.vars))
	for key, value := range m.vars {
		result = append(result, fmt.Sprintf("%s=%s", key, value))
	}
	return result
}

// Set is a helper method to pre-populate the mock store
func (m *MockEnvOps) Set(key, value string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.vars[key] = value
}

// Get is a helper method to read from the mock store
func (m *MockEnvOps) Get(key string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	value, exists := m.vars[key]
	return value, exists
}

// Clear removes all variables from the mock store
func (m *MockEnvOps) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.vars = make(map[string]string)
}
