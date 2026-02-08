// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"context"
	"sync"
)

// MockStore is a mock implementation of the Store interface for testing.
// It provides configurable behavior including error injection and call tracking.
type MockStore struct {
	mu sync.RWMutex

	// Data holds the mock secret data
	Data map[string][]byte

	// Error injection fields - set these to make operations return errors
	GetErr    error
	SetErr    error
	DeleteErr error
	ListErr   error
	ExistsErr error
	RotateErr error

	// Call tracking
	GetCalls    []string
	SetCalls    []SetCall
	DeleteCalls []string
	ListCalls   int
	ExistsCalls []string
	RotateCalls int
}

// SetCall records a call to Set with name and value.
type SetCall struct {
	Name  string
	Value []byte
}

// NewMockStore creates a new MockStore with an empty data map.
func NewMockStore() *MockStore {
	return &MockStore{
		Data: make(map[string][]byte),
	}
}

// Get retrieves a secret by name from the mock store.
func (m *MockStore) Get(_ context.Context, name string) ([]byte, error) {
	m.mu.Lock()
	m.GetCalls = append(m.GetCalls, name)
	m.mu.Unlock()

	if m.GetErr != nil {
		return nil, m.GetErr
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	value, ok := m.Data[name]
	if !ok {
		return nil, ErrNotFound
	}

	// Return a copy to prevent mutation
	result := make([]byte, len(value))
	copy(result, value)
	return result, nil
}

// Set stores a secret in the mock store.
func (m *MockStore) Set(_ context.Context, name string, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.SetCalls = append(m.SetCalls, SetCall{Name: name, Value: value})

	if m.SetErr != nil {
		return m.SetErr
	}

	// Store a copy to prevent mutation
	stored := make([]byte, len(value))
	copy(stored, value)
	m.Data[name] = stored

	return nil
}

// Delete removes a secret from the mock store.
func (m *MockStore) Delete(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.DeleteCalls = append(m.DeleteCalls, name)

	if m.DeleteErr != nil {
		return m.DeleteErr
	}

	delete(m.Data, name)
	return nil
}

// List returns all secret names in the mock store.
func (m *MockStore) List(_ context.Context) ([]string, error) {
	m.mu.Lock()
	m.ListCalls++
	m.mu.Unlock()

	if m.ListErr != nil {
		return nil, m.ListErr
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.Data))
	for name := range m.Data {
		names = append(names, name)
	}

	return names, nil
}

// Exists checks if a secret exists in the mock store.
func (m *MockStore) Exists(_ context.Context, name string) (bool, error) {
	m.mu.Lock()
	m.ExistsCalls = append(m.ExistsCalls, name)
	m.mu.Unlock()

	if m.ExistsErr != nil {
		return false, m.ExistsErr
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	_, ok := m.Data[name]
	return ok, nil
}

// Rotate simulates master key rotation by not changing anything in the mock.
// It only tracks that rotation was called.
func (m *MockStore) Rotate(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.RotateCalls++

	if m.RotateErr != nil {
		return m.RotateErr
	}

	return nil
}

// Reset clears all data and call tracking.
func (m *MockStore) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Data = make(map[string][]byte)
	m.GetErr = nil
	m.SetErr = nil
	m.DeleteErr = nil
	m.ListErr = nil
	m.ExistsErr = nil
	m.RotateErr = nil
	m.GetCalls = nil
	m.SetCalls = nil
	m.DeleteCalls = nil
	m.ListCalls = 0
	m.ExistsCalls = nil
	m.RotateCalls = 0
}

// Verify MockStore implements Store interface
var _ Store = (*MockStore)(nil)

// MockKeyring is a mock implementation of the Keyring interface for testing.
// It provides configurable behavior including error injection.
type MockKeyring struct {
	mu sync.RWMutex

	// Data holds the mock keyring data
	Data map[string]string

	// Error injection fields
	GetErr    error
	SetErr    error
	DeleteErr error

	// Call tracking
	GetCalls    []KeyringCall
	SetCalls    []KeyringSetCall
	DeleteCalls []KeyringCall
}

// KeyringCall records a call to Get or Delete.
type KeyringCall struct {
	Service string
	Account string
}

// KeyringSetCall records a call to Set.
type KeyringSetCall struct {
	Service string
	Account string
	Value   string
}

// NewMockKeyring creates a new MockKeyring with an empty data map.
func NewMockKeyring() *MockKeyring {
	return &MockKeyring{
		Data: make(map[string]string),
	}
}

// Get retrieves a value from the mock keyring.
func (m *MockKeyring) Get(service, account string) (string, error) {
	m.mu.Lock()
	m.GetCalls = append(m.GetCalls, KeyringCall{Service: service, Account: account})
	m.mu.Unlock()

	if m.GetErr != nil {
		return "", m.GetErr
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	key := service + ":" + account
	value, ok := m.Data[key]
	if !ok {
		return "", ErrKeyNotFound
	}

	return value, nil
}

// Set stores a value in the mock keyring.
func (m *MockKeyring) Set(service, account, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.SetCalls = append(m.SetCalls, KeyringSetCall{
		Service: service,
		Account: account,
		Value:   value,
	})

	if m.SetErr != nil {
		return m.SetErr
	}

	key := service + ":" + account
	m.Data[key] = value

	return nil
}

// Delete removes a value from the mock keyring.
func (m *MockKeyring) Delete(service, account string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.DeleteCalls = append(m.DeleteCalls, KeyringCall{Service: service, Account: account})

	if m.DeleteErr != nil {
		return m.DeleteErr
	}

	key := service + ":" + account
	delete(m.Data, key)

	return nil
}

// Reset clears all data and call tracking.
func (m *MockKeyring) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Data = make(map[string]string)
	m.GetErr = nil
	m.SetErr = nil
	m.DeleteErr = nil
	m.GetCalls = nil
	m.SetCalls = nil
	m.DeleteCalls = nil
}

// Verify MockKeyring implements Keyring interface
var _ Keyring = (*MockKeyring)(nil)
