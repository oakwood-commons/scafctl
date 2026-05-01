// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builtin

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultRegistry(t *testing.T) {
	t.Run("returns registry with all providers", func(t *testing.T) {
		ctx := context.Background()
		reg, err := DefaultRegistry(ctx)
		require.NoError(t, err)
		require.NotNil(t, reg)

		// Verify all expected providers are registered
		expectedProviders := ProviderNames()
		for _, name := range expectedProviders {
			p, found := reg.Get(name)
			assert.True(t, found, "provider %q should be registered", name)
			assert.NotNil(t, p, "provider %q should not be nil", name)
		}
	})

	t.Run("returns same registry on multiple calls", func(t *testing.T) {
		ctx := context.Background()
		reg1, err1 := DefaultRegistry(ctx)
		require.NoError(t, err1)

		reg2, err2 := DefaultRegistry(ctx)
		require.NoError(t, err2)

		// Should return the same instance
		assert.Same(t, reg1, reg2, "DefaultRegistry should return the same instance")
	})

	t.Run("is thread-safe", func(t *testing.T) {
		ctx := context.Background()
		var wg sync.WaitGroup

		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				reg, err := DefaultRegistry(ctx)
				require.NoError(t, err)
				require.NotNil(t, reg)
			}()
		}

		wg.Wait()

		// All should be the same instance
		first, _ := DefaultRegistry(ctx)
		for i := 0; i < 10; i++ {
			reg, _ := DefaultRegistry(ctx)
			assert.Same(t, first, reg)
		}
	})
}

func TestProviderNames(t *testing.T) {
	names := ProviderNames()

	// Should have all built-in providers
	expectedCount := 9 // http, cel, file, validation, debug, go-template, message, static, parameter
	assert.Len(t, names, expectedCount, "should have %d built-in providers", expectedCount)

	// Verify expected names are present
	expectedNames := []string{
		"http",
		"cel",
		"file",
		"validation",
		"debug",
		"go-template",
		"message",
		"static",
		"parameter",
	}

	for _, expected := range expectedNames {
		assert.Contains(t, names, expected, "should contain provider %q", expected)
	}
}

func TestDefaultRegistry_ProviderFunctionality(t *testing.T) {
	ctx := context.Background()
	reg, err := DefaultRegistry(ctx)
	require.NoError(t, err)

	t.Run("http provider is functional", func(t *testing.T) {
		p, found := reg.Get("http")
		require.True(t, found)

		desc := p.Descriptor()
		assert.Equal(t, "http", desc.Name)
		assert.NotEmpty(t, desc.Description)
	})

	t.Run("cel provider is functional", func(t *testing.T) {
		p, found := reg.Get("cel")
		require.True(t, found)

		desc := p.Descriptor()
		assert.Equal(t, "cel", desc.Name)
	})

	t.Run("validation provider is functional", func(t *testing.T) {
		p, found := reg.Get("validation")
		require.True(t, found)

		desc := p.Descriptor()
		assert.Equal(t, "validation", desc.Name)
	})
}

// TestAllProvidersRegistered explicitly verifies that every built-in provider
// is registered in the default registry and has a valid descriptor.
func TestAllProvidersRegistered(t *testing.T) {
	ctx := context.Background()
	reg, err := DefaultRegistry(ctx)
	require.NoError(t, err)

	// All expected built-in providers
	expectedProviders := []struct {
		name        string
		description string // partial match
	}{
		{"http", "HTTP"},
		{"cel", "CEL"},
		{"file", "file"},
		{"validation", "validat"},
		{"debug", "debug"},
		{"go-template", "template"},
		{"message", "message"},
		{"static", "static"},
		{"parameter", "parameter"},
	}

	for _, expected := range expectedProviders {
		t.Run(expected.name, func(t *testing.T) {
			p, found := reg.Get(expected.name)
			require.True(t, found, "provider %q must be registered", expected.name)
			require.NotNil(t, p, "provider %q must not be nil", expected.name)

			desc := p.Descriptor()
			assert.Equal(t, expected.name, desc.Name, "descriptor name must match")
			assert.NotEmpty(t, desc.Description, "descriptor must have description")
			assert.Contains(t, strings.ToLower(desc.Description), strings.ToLower(expected.description),
				"description should mention %q", expected.description)
		})
	}

	registeredCount := len(reg.ListProviders())
	assert.Equal(t, len(expectedProviders), registeredCount, "registered provider count must match expected")
}
