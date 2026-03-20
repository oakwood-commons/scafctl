// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockProvider is a simple provider implementation for testing.
type mockProvider struct {
	descriptor *Descriptor
}

func (m *mockProvider) Descriptor() *Descriptor {
	return m.descriptor
}

func (m *mockProvider) Execute(_ context.Context, _ any) (*Output, error) {
	return &Output{Data: "mock"}, nil
}

// newMockProvider creates a mock provider with the given name and version.
func newMockProvider(name, version string, capabilities ...Capability) Provider {
	ver := semver.MustParse(version)
	if len(capabilities) == 0 {
		capabilities = []Capability{CapabilityFrom}
	}

	// Build output schemas for each capability with required fields
	outputSchemas := make(map[Capability]*jsonschema.Schema)
	for _, cap := range capabilities {
		switch cap {
		case CapabilityValidation:
			outputSchemas[cap] = &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"valid":  {Type: "boolean"},
					"errors": {Type: "array"},
				},
			}
		case CapabilityAuthentication:
			outputSchemas[cap] = &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"authenticated": {Type: "boolean"},
					"token":         {Type: "string"},
				},
			}
		case CapabilityAction:
			outputSchemas[cap] = &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"success": {Type: "boolean"},
				},
			}
		case CapabilityFrom, CapabilityTransform:
			outputSchemas[cap] = &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"result": {Type: "string"},
				},
			}
		}
	}

	return &mockProvider{
		descriptor: &Descriptor{
			Name:          name,
			APIVersion:    "v1",
			Version:       ver,
			Description:   "Mock provider for testing",
			Capabilities:  capabilities,
			OutputSchemas: outputSchemas,
			Schema: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"test": {Type: "string"},
				},
			},
		},
	}
}

func TestNewRegistry(t *testing.T) {
	tests := []struct {
		name string
		opts []RegistryOption
	}{
		{
			name: "default options",
			opts: nil,
		},
		{
			name: "with allow overwrite",
			opts: []RegistryOption{WithAllowOverwrite(true)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRegistry(tt.opts...)
			assert.NotNil(t, r)
			assert.Equal(t, 0, r.Count())
		})
	}
}

func TestRegistry_Register(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*Registry)
		provide Provider
		wantErr bool
		errMsg  string
	}{
		{
			name:    "successful registration",
			provide: newMockProvider("test", "1.0.0"),
			wantErr: false,
		},
		{
			name:    "nil provider",
			provide: nil,
			wantErr: true,
			errMsg:  "cannot register nil provider",
		},
		{
			name: "duplicate name",
			setup: func(r *Registry) {
				_ = r.Register(newMockProvider("test", "1.0.0"))
			},
			provide: newMockProvider("test", "2.0.0"),
			wantErr: true,
			errMsg:  "already registered",
		},
		{
			name: "invalid descriptor - empty name",
			provide: &mockProvider{
				descriptor: &Descriptor{
					Version:      semver.MustParse("1.0.0"),
					Capabilities: []Capability{CapabilityFrom},
				},
			},
			wantErr: true,
			errMsg:  "provider name cannot be empty",
		},
		{
			name: "invalid descriptor - nil version",
			provide: &mockProvider{
				descriptor: &Descriptor{
					Name:         "test",
					APIVersion:   "v1",
					Description:  "A test provider",
					Capabilities: []Capability{CapabilityFrom},
				},
			},
			wantErr: true,
			errMsg:  "provider version cannot be nil",
		},
		{
			name: "invalid descriptor - no capabilities",
			provide: &mockProvider{
				descriptor: &Descriptor{
					Name:        "test",
					APIVersion:  "v1",
					Version:     semver.MustParse("1.0.0"),
					Description: "A test provider",
				},
			},
			wantErr: true,
			errMsg:  "must declare at least one capability",
		},
		{
			name: "invalid descriptor - invalid capability",
			provide: &mockProvider{
				descriptor: &Descriptor{
					Name:         "test",
					APIVersion:   "v1",
					Version:      semver.MustParse("1.0.0"),
					Description:  "A test provider",
					Capabilities: []Capability{"invalid"},
				},
			},
			wantErr: true,
			errMsg:  "is not valid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRegistry()
			if tt.setup != nil {
				tt.setup(r)
			}

			err := r.Register(tt.provide)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRegistry_RegisterWithOverwrite(t *testing.T) {
	r := NewRegistry(WithAllowOverwrite(true))

	// Register initial version
	p1 := newMockProvider("test", "1.0.0")
	err := r.Register(p1)
	require.NoError(t, err)

	// Register newer version (should succeed)
	p2 := newMockProvider("test", "2.0.0")
	err = r.Register(p2)
	assert.NoError(t, err)

	// Verify newer version is registered
	retrieved, exists := r.Get("test")
	require.True(t, exists)
	assert.Equal(t, "2.0.0", retrieved.Descriptor().Version.String())

	// Try to register older version (should fail)
	p3 := newMockProvider("test", "1.5.0")
	err = r.Register(p3)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "older than existing version")
}

func TestRegistry_Get(t *testing.T) {
	r := NewRegistry()
	p := newMockProvider("test", "1.0.0")
	_ = r.Register(p)

	tests := []struct {
		name       string
		lookupName string
		wantExists bool
	}{
		{
			name:       "existing provider",
			lookupName: "test",
			wantExists: true,
		},
		{
			name:       "non-existing provider",
			lookupName: "nonexistent",
			wantExists: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			retrieved, exists := r.Get(tt.lookupName)
			assert.Equal(t, tt.wantExists, exists)
			if tt.wantExists {
				assert.NotNil(t, retrieved)
				assert.Equal(t, tt.lookupName, retrieved.Descriptor().Name)
			} else {
				assert.Nil(t, retrieved)
			}
		})
	}
}

func TestRegistry_Has(t *testing.T) {
	r := NewRegistry()
	p := newMockProvider("test", "1.0.0")
	_ = r.Register(p)

	assert.True(t, r.Has("test"))
	assert.False(t, r.Has("nonexistent"))
}

func TestRegistry_List(t *testing.T) {
	r := NewRegistry()

	// Empty registry
	assert.Empty(t, r.List())

	// Register multiple providers
	_ = r.Register(newMockProvider("zebra", "1.0.0"))
	_ = r.Register(newMockProvider("alpha", "1.0.0"))
	_ = r.Register(newMockProvider("beta", "1.0.0"))

	names := r.List()
	assert.Len(t, names, 3)
	// Should be sorted alphabetically
	assert.Equal(t, []string{"alpha", "beta", "zebra"}, names)
}

func TestRegistry_ListProviders(t *testing.T) {
	r := NewRegistry()

	// Empty registry
	assert.Empty(t, r.ListProviders())

	// Register multiple providers
	_ = r.Register(newMockProvider("zebra", "1.0.0"))
	_ = r.Register(newMockProvider("alpha", "1.0.0"))
	_ = r.Register(newMockProvider("beta", "1.0.0"))

	providers := r.ListProviders()
	assert.Len(t, providers, 3)
	// Should be sorted alphabetically by name
	names := make([]string, len(providers))
	for i, p := range providers {
		names[i] = p.Descriptor().Name
	}
	assert.Equal(t, []string{"alpha", "beta", "zebra"}, names)
}

func TestRegistry_ListByCapability(t *testing.T) {
	r := NewRegistry()

	// Register providers with different capabilities
	_ = r.Register(newMockProvider("from-only", "1.0.0", CapabilityFrom))
	_ = r.Register(newMockProvider("transform-only", "1.0.0", CapabilityTransform))
	_ = r.Register(newMockProvider("both", "1.0.0", CapabilityFrom, CapabilityTransform))
	_ = r.Register(newMockProvider("action-only", "1.0.0", CapabilityAction))

	tests := []struct {
		name       string
		capability Capability
		want       []string
	}{
		{
			name:       "from capability",
			capability: CapabilityFrom,
			want:       []string{"both", "from-only"},
		},
		{
			name:       "transform capability",
			capability: CapabilityTransform,
			want:       []string{"both", "transform-only"},
		},
		{
			name:       "action capability",
			capability: CapabilityAction,
			want:       []string{"action-only"},
		},
		{
			name:       "validation capability (none)",
			capability: CapabilityValidation,
			want:       []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			providers := r.ListByCapability(tt.capability)
			names := make([]string, len(providers))
			for i, p := range providers {
				names[i] = p.Descriptor().Name
			}
			assert.Equal(t, tt.want, names)
		})
	}
}

func TestRegistry_ListByCategory(t *testing.T) {
	r := NewRegistry()

	// Create providers with categories
	p1 := newMockProvider("api1", "1.0.0")
	p1.Descriptor().Category = "api"
	_ = r.Register(p1)

	p2 := newMockProvider("api2", "1.0.0")
	p2.Descriptor().Category = "api"
	_ = r.Register(p2)

	p3 := newMockProvider("db1", "1.0.0")
	p3.Descriptor().Category = "database"
	_ = r.Register(p3)

	tests := []struct {
		name     string
		category string
		want     []string
	}{
		{
			name:     "api category",
			category: "api",
			want:     []string{"api1", "api2"},
		},
		{
			name:     "database category",
			category: "database",
			want:     []string{"db1"},
		},
		{
			name:     "non-existent category",
			category: "nonexistent",
			want:     []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			providers := r.ListByCategory(tt.category)
			names := make([]string, len(providers))
			for i, p := range providers {
				names[i] = p.Descriptor().Name
			}
			assert.Equal(t, tt.want, names)
		})
	}
}

func TestRegistry_Unregister(t *testing.T) {
	r := NewRegistry()
	p := newMockProvider("test", "1.0.0")
	_ = r.Register(p)

	// Unregister existing provider
	assert.True(t, r.Unregister("test"))
	assert.False(t, r.Has("test"))

	// Unregister non-existing provider
	assert.False(t, r.Unregister("nonexistent"))
}

func TestRegistry_Count(t *testing.T) {
	r := NewRegistry()
	assert.Equal(t, 0, r.Count())

	_ = r.Register(newMockProvider("test1", "1.0.0"))
	assert.Equal(t, 1, r.Count())

	_ = r.Register(newMockProvider("test2", "1.0.0"))
	assert.Equal(t, 2, r.Count())

	r.Unregister("test1")
	assert.Equal(t, 1, r.Count())
}

func TestRegistry_Clear(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(newMockProvider("test1", "1.0.0"))
	_ = r.Register(newMockProvider("test2", "1.0.0"))

	assert.Equal(t, 2, r.Count())

	r.Clear()
	assert.Equal(t, 0, r.Count())
	assert.Empty(t, r.List())
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	r := NewRegistry()

	// Concurrent registration
	var wg sync.WaitGroup
	numGoroutines := 10

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			p := newMockProvider(fmt.Sprintf("provider-%d", idx), "1.0.0")
			err := r.Register(p)
			assert.NoError(t, err)
		}(i)
	}
	wg.Wait()

	assert.Equal(t, numGoroutines, r.Count())

	// Concurrent reads
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			name := fmt.Sprintf("provider-%d", idx)
			p, exists := r.Get(name)
			assert.True(t, exists)
			assert.NotNil(t, p)
		}(i)
	}
	wg.Wait()

	// Concurrent list operations
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			names := r.List()
			assert.Len(t, names, numGoroutines)
		}()
	}
	wg.Wait()
}

func TestGlobalRegistry(t *testing.T) {
	// Clean slate for this test
	ResetGlobalRegistry()

	t.Run("register and get", func(t *testing.T) {
		p := newMockProvider("global-test", "1.0.0")
		err := Register(p)
		require.NoError(t, err)

		retrieved, exists := Get("global-test")
		assert.True(t, exists)
		assert.NotNil(t, retrieved)
	})

	t.Run("has", func(t *testing.T) {
		assert.True(t, Has("global-test"))
		assert.False(t, Has("nonexistent"))
	})

	t.Run("list", func(t *testing.T) {
		names := List()
		assert.Contains(t, names, "global-test")
	})

	t.Run("list providers", func(t *testing.T) {
		providers := ListProviders()
		assert.NotEmpty(t, providers)
	})

	t.Run("count", func(t *testing.T) {
		count := Count()
		assert.Greater(t, count, 0)
	})

	t.Run("list by capability", func(t *testing.T) {
		providers := ListByCapability(CapabilityFrom)
		names := make([]string, len(providers))
		for i, p := range providers {
			names[i] = p.Descriptor().Name
		}
		assert.Contains(t, names, "global-test")
	})

	t.Run("list by category", func(t *testing.T) {
		providers := ListByCategory("test-category")
		assert.Empty(t, providers)
	})

	t.Run("get global registry", func(t *testing.T) {
		gr := GetGlobalRegistry()
		assert.NotNil(t, gr)
		assert.Same(t, globalRegistry, gr)
	})

	// Clean up
	ResetGlobalRegistry()
}

func TestRegistry_DescriptorLookup(t *testing.T) {
	r := NewRegistry()
	p := newMockProvider("lookup-test", "1.0.0", CapabilityFrom)
	_ = r.Register(p)

	lookup := r.DescriptorLookup()
	assert.NotNil(t, lookup)

	desc := lookup("lookup-test")
	assert.NotNil(t, desc)
	assert.Equal(t, "lookup-test", desc.Name)

	nilDesc := lookup("nonexistent")
	assert.Nil(t, nilDesc)
}

func TestValidateDescriptor(t *testing.T) {
	r := NewRegistry()

	tests := []struct {
		name    string
		desc    *Descriptor
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid descriptor",
			desc: &Descriptor{
				Name:         "test",
				APIVersion:   "v1",
				Version:      semver.MustParse("1.0.0"),
				Description:  "A test provider",
				Capabilities: []Capability{CapabilityFrom},
				Schema: &jsonschema.Schema{
					Type: "object",
					Properties: map[string]*jsonschema.Schema{
						"test": {Type: "string"},
					},
				},
				OutputSchemas: map[Capability]*jsonschema.Schema{
					CapabilityFrom: {
						Type: "object",
						Properties: map[string]*jsonschema.Schema{
							"result": {Type: "string"},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "empty name",
			desc: &Descriptor{
				Version:      semver.MustParse("1.0.0"),
				Capabilities: []Capability{CapabilityFrom},
			},
			wantErr: true,
			errMsg:  "name cannot be empty",
		},
		{
			name: "nil version",
			desc: &Descriptor{
				Name:         "test",
				APIVersion:   "v1",
				Description:  "A test provider",
				Capabilities: []Capability{CapabilityFrom},
			},
			wantErr: true,
			errMsg:  "version cannot be nil",
		},
		{
			name: "no capabilities",
			desc: &Descriptor{
				Name:        "test",
				APIVersion:  "v1",
				Version:     semver.MustParse("1.0.0"),
				Description: "A test provider",
			},
			wantErr: true,
			errMsg:  "at least one capability",
		},
		{
			name: "empty APIVersion",
			desc: &Descriptor{
				Name:         "test",
				Version:      semver.MustParse("1.0.0"),
				Description:  "A test provider",
				Capabilities: []Capability{CapabilityFrom},
			},
			wantErr: true,
			errMsg:  "APIVersion cannot be empty",
		},
		{
			name: "empty Description",
			desc: &Descriptor{
				Name:         "test",
				APIVersion:   "v1",
				Version:      semver.MustParse("1.0.0"),
				Capabilities: []Capability{CapabilityFrom},
			},
			wantErr: true,
			errMsg:  "description cannot be empty",
		},
		{
			name: "invalid property type",
			desc: &Descriptor{
				Name:         "test",
				APIVersion:   "v1",
				Version:      semver.MustParse("1.0.0"),
				Description:  "A test provider",
				Capabilities: []Capability{CapabilityFrom},
				Schema: &jsonschema.Schema{
					Type: "object",
					Properties: map[string]*jsonschema.Schema{
						"test": {Type: "invalid"},
					},
				},
			},
			wantErr: true,
			errMsg:  "invalid type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := r.validateDescriptor(tt.desc)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
