// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"context"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
)

// mockBackendProvider implements provider.Provider for testing the manager.
type mockBackendProvider struct {
	loadData  *Data
	loadErr   error
	saveErr   error
	saveCalls []map[string]any
}

func (m *mockBackendProvider) Descriptor() *provider.Descriptor {
	return &provider.Descriptor{
		Name:        "mock-state",
		DisplayName: "Mock State",
		Description: "Mock state backend for testing",
		APIVersion:  "v1",
		Version:     semver.MustParse("1.0.0"),
		Capabilities: []provider.Capability{
			provider.CapabilityState,
		},
		OutputSchemas: map[provider.Capability]*jsonschema.Schema{
			provider.CapabilityState: schemahelper.ObjectSchema([]string{"success"}, map[string]*jsonschema.Schema{
				"success": schemahelper.BoolProp("Whether the operation succeeded"),
			}),
		},
	}
}

func (m *mockBackendProvider) Execute(_ context.Context, input any) (*provider.Output, error) {
	inputs, _ := input.(map[string]any)
	op, _ := inputs["operation"].(string)

	switch op {
	case "state_load":
		if m.loadErr != nil {
			return nil, m.loadErr
		}
		data := m.loadData
		if data == nil {
			data = NewData()
		}
		return &provider.Output{
			Data: map[string]any{
				"success": true,
				"data":    data,
			},
		}, nil
	case "state_save":
		if m.saveErr != nil {
			return nil, m.saveErr
		}
		m.saveCalls = append(m.saveCalls, inputs)
		return &provider.Output{
			Data: map[string]any{
				"success": true,
			},
		}, nil
	default:
		return nil, nil
	}
}

func newTestRegistry(t *testing.T, p provider.Provider) *provider.Registry {
	t.Helper()
	reg := provider.NewRegistry()
	err := reg.Register(p)
	assert.NoError(t, err, "failed to register mock provider")
	return reg
}

func literalValueRef(val any) *spec.ValueRef {
	return &spec.ValueRef{Literal: val}
}

func TestManagerLoad(t *testing.T) {
	existingState := NewData()
	existingState.Metadata.Solution = "test-app"
	existingState.Values["existing_key"] = &Entry{Value: "existing_val", Type: "string"}

	tests := []struct {
		name     string
		config   *Config
		backend  *mockBackendProvider
		wantSkip bool
		wantErr  bool
		check    func(t *testing.T, result *LoadResult)
	}{
		{
			name:     "nil config skips",
			config:   nil,
			wantSkip: true,
		},
		{
			name: "enabled true loads state",
			config: &Config{
				Enabled: literalValueRef(true),
				Backend: Backend{
					Provider: "mock-state",
					Inputs:   map[string]*spec.ValueRef{"path": literalValueRef("test.json")},
				},
			},
			backend: &mockBackendProvider{loadData: existingState},
			check: func(t *testing.T, result *LoadResult) {
				assert.False(t, result.Skipped)
				assert.NotNil(t, result.Data)
				assert.Equal(t, "test-app", result.Data.Metadata.Solution)
			},
		},
		{
			name: "enabled false skips",
			config: &Config{
				Enabled: literalValueRef(false),
				Backend: Backend{
					Provider: "mock-state",
					Inputs:   map[string]*spec.ValueRef{},
				},
			},
			wantSkip: true,
		},
		{
			name: "nil enabled means enabled",
			config: &Config{
				Backend: Backend{
					Provider: "mock-state",
					Inputs:   map[string]*spec.ValueRef{},
				},
			},
			backend: &mockBackendProvider{},
			check: func(t *testing.T, result *LoadResult) {
				assert.False(t, result.Skipped)
				assert.NotNil(t, result.Data)
			},
		},
		{
			name: "backend load error",
			config: &Config{
				Enabled: literalValueRef(true),
				Backend: Backend{
					Provider: "mock-state",
					Inputs:   map[string]*spec.ValueRef{},
				},
			},
			backend: &mockBackendProvider{loadErr: assert.AnError},
			wantErr: true,
		},
		{
			name: "provider not found",
			config: &Config{
				Enabled: literalValueRef(true),
				Backend: Backend{
					Provider: "nonexistent",
					Inputs:   map[string]*spec.ValueRef{},
				},
			},
			wantErr: true,
		},
		{
			name: "command is captured",
			config: &Config{
				Enabled: literalValueRef(true),
				Backend: Backend{
					Provider: "mock-state",
					Inputs:   map[string]*spec.ValueRef{},
				},
			},
			backend: &mockBackendProvider{},
			check: func(t *testing.T, result *LoadResult) {
				assert.Equal(t, "run solution", result.Data.Command.Subcommand)
				assert.Equal(t, "bar", result.Data.Command.Parameters["foo"])
			},
		},
		{
			name: "state injected into context",
			config: &Config{
				Enabled: literalValueRef(true),
				Backend: Backend{
					Provider: "mock-state",
					Inputs:   map[string]*spec.ValueRef{},
				},
			},
			backend: &mockBackendProvider{},
			check: func(t *testing.T, result *LoadResult) {
				sd, ok := FromContext(result.Ctx)
				assert.True(t, ok)
				assert.NotNil(t, sd)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var reg *provider.Registry
			if tt.backend != nil {
				reg = newTestRegistry(t, tt.backend)
			} else {
				reg = provider.NewRegistry()
			}

			mgr := NewManager(tt.config, reg, "test-version")
			cmd := CommandInfo{
				Subcommand: "run solution",
				Parameters: map[string]string{"foo": "bar"},
			}
			result, err := mgr.Load(context.Background(), nil, cmd)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			if tt.wantSkip {
				assert.True(t, result.Skipped)
				return
			}
			if tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}

func TestManagerSave(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		backend *mockBackendProvider
		state   *Data
		setup   func() (*resolver.Context, []*resolver.Resolver)
		wantErr bool
		check   func(t *testing.T, sd *Data, backend *mockBackendProvider)
	}{
		{
			name:   "nil config is noop",
			config: nil,
			state:  NewData(),
			setup: func() (*resolver.Context, []*resolver.Resolver) {
				return resolver.NewContext(), nil
			},
		},
		{
			name: "collects saveToState values",
			config: &Config{
				Enabled: literalValueRef(true),
				Backend: Backend{
					Provider: "mock-state",
					Inputs:   map[string]*spec.ValueRef{},
				},
			},
			backend: &mockBackendProvider{},
			state:   NewData(),
			setup: func() (*resolver.Context, []*resolver.Resolver) {
				rctx := resolver.NewContext()
				rctx.SetResult("api_key", &resolver.ExecutionResult{
					Value:  "secret123",
					Status: resolver.ExecutionStatusSuccess,
				})
				rctx.SetResult("name", &resolver.ExecutionResult{
					Value:  "app",
					Status: resolver.ExecutionStatusSuccess,
				})
				rctx.SetResult("failed_one", &resolver.ExecutionResult{
					Value:  "x",
					Status: resolver.ExecutionStatusFailed,
				})
				resolvers := []*resolver.Resolver{
					{Name: "api_key", Type: "string", SaveToState: true},
					{Name: "name", Type: "string", SaveToState: false},
					{Name: "failed_one", Type: "string", SaveToState: true},
				}
				return rctx, resolvers
			},
			check: func(t *testing.T, sd *Data, backend *mockBackendProvider) {
				// api_key should be saved (saveToState + success)
				assert.Contains(t, sd.Values, "api_key")
				assert.Equal(t, "secret123", sd.Values["api_key"].Value)
				assert.Equal(t, "string", sd.Values["api_key"].Type)

				// name should NOT be saved (saveToState is false)
				_, hasName := sd.Values["name"]
				assert.False(t, hasName)

				// failed_one should NOT be saved (status is failed)
				_, hasFailed := sd.Values["failed_one"]
				assert.False(t, hasFailed)

				// backend save should have been called
				assert.Len(t, backend.saveCalls, 1)
			},
		},
		{
			name: "updates metadata",
			config: &Config{
				Enabled: literalValueRef(true),
				Backend: Backend{
					Provider: "mock-state",
					Inputs:   map[string]*spec.ValueRef{},
				},
			},
			backend: &mockBackendProvider{},
			state:   NewData(),
			setup: func() (*resolver.Context, []*resolver.Resolver) {
				return resolver.NewContext(), nil
			},
			check: func(t *testing.T, sd *Data, _ *mockBackendProvider) {
				assert.Equal(t, "my-app", sd.Metadata.Solution)
				assert.Equal(t, "2.0.0", sd.Metadata.Version)
				assert.Equal(t, "test-version", sd.Metadata.ScafctlVersion)
				assert.False(t, sd.Metadata.CreatedAt.IsZero())
				assert.False(t, sd.Metadata.LastUpdatedAt.IsZero())
			},
		},
		{
			name: "preserves existing createdAt",
			config: &Config{
				Enabled: literalValueRef(true),
				Backend: Backend{
					Provider: "mock-state",
					Inputs:   map[string]*spec.ValueRef{},
				},
			},
			backend: &mockBackendProvider{},
			state: func() *Data {
				sd := NewData()
				sd.Metadata.CreatedAt = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
				return sd
			}(),
			setup: func() (*resolver.Context, []*resolver.Resolver) {
				return resolver.NewContext(), nil
			},
			check: func(t *testing.T, sd *Data, _ *mockBackendProvider) {
				assert.Equal(t, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), sd.Metadata.CreatedAt)
			},
		},
		{
			name: "save backend error",
			config: &Config{
				Enabled: literalValueRef(true),
				Backend: Backend{
					Provider: "mock-state",
					Inputs:   map[string]*spec.ValueRef{},
				},
			},
			backend: &mockBackendProvider{saveErr: assert.AnError},
			state:   NewData(),
			setup: func() (*resolver.Context, []*resolver.Resolver) {
				return resolver.NewContext(), nil
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var reg *provider.Registry
			if tt.backend != nil {
				reg = newTestRegistry(t, tt.backend)
			} else {
				reg = provider.NewRegistry()
			}

			mgr := NewManager(tt.config, reg, "test-version")
			rctx, resolvers := tt.setup()
			solMeta := SolutionMeta{Name: "my-app", Version: "2.0.0"}

			err := mgr.Save(context.Background(), tt.state, rctx, resolvers, nil, nil, solMeta)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			if tt.check != nil {
				tt.check(t, tt.state, tt.backend)
			}
		})
	}
}

func TestManagerRequiredResolvers(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
		want   []string
	}{
		{
			name:   "nil config",
			config: nil,
			want:   nil,
		},
		{
			name: "literal values no resolvers",
			config: &Config{
				Enabled: literalValueRef(true),
				Backend: Backend{
					Inputs: map[string]*spec.ValueRef{"path": literalValueRef("test.json")},
				},
			},
			want: nil,
		},
		{
			name: "resolver reference in enabled",
			config: &Config{
				Enabled: &spec.ValueRef{Resolver: strPtr("use_state")},
				Backend: Backend{
					Inputs: map[string]*spec.ValueRef{},
				},
			},
			want: []string{"use_state"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := NewManager(tt.config, nil, "v")
			got := mgr.RequiredResolvers()
			if tt.want == nil {
				assert.Empty(t, got)
			} else {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestIsTruthy(t *testing.T) {
	tests := []struct {
		name string
		val  any
		want bool
	}{
		{"nil", nil, false},
		{"true", true, true},
		{"false", false, false},
		{"non-empty string", "yes", true},
		{"empty string", "", false},
		{"false string", "false", false},
		{"zero string", "0", false},
		{"int 1", 1, true},
		{"int 0", 0, false},
		{"int64 1", int64(1), true},
		{"int64 0", int64(0), false},
		{"float64 1", float64(1), true},
		{"float64 0", float64(0), false},
		{"struct", struct{}{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isTruthy(tt.val))
		})
	}
}

func strPtr(s string) *string {
	return &s
}

func TestStructToMap(t *testing.T) {
	t.Run("converts struct to map", func(t *testing.T) {
		sd := NewData()
		sd.SchemaVersion = 1
		sd.Metadata.Solution = "test-app"
		sd.Values["key1"] = &Entry{Value: "val1", Type: "string"}

		m, err := structToMap(sd)
		assert.NoError(t, err)
		assert.Equal(t, float64(1), m["schemaVersion"])

		meta, ok := m["metadata"].(map[string]any)
		assert.True(t, ok)
		assert.Equal(t, "test-app", meta["solution"])

		vals, ok := m["values"].(map[string]any)
		assert.True(t, ok)
		entry, ok := vals["key1"].(map[string]any)
		assert.True(t, ok)
		assert.Equal(t, "val1", entry["value"])
		assert.Equal(t, "string", entry["type"])
	})

	t.Run("empty data", func(t *testing.T) {
		sd := NewData()
		m, err := structToMap(sd)
		assert.NoError(t, err)
		assert.NotNil(t, m)
		assert.Equal(t, float64(SchemaVersionCurrent), m["schemaVersion"])
	})
}

func TestExtractStateData(t *testing.T) {
	t.Parallel()

	t.Run("nil result", func(t *testing.T) {
		t.Parallel()
		_, err := extractStateData(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "nil execution result")
	})

	t.Run("non-map output", func(t *testing.T) {
		t.Parallel()
		_, err := extractStateData(&provider.ExecutionResult{
			Output: provider.Output{Data: "not a map"},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "expected map output")
	})

	t.Run("missing data field", func(t *testing.T) {
		t.Parallel()
		_, err := extractStateData(&provider.ExecutionResult{
			Output: provider.Output{Data: map[string]any{"success": true}},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing 'data'")
	})

	t.Run("direct pointer", func(t *testing.T) {
		t.Parallel()
		expected := NewMockData("test", "1.0.0", map[string]*Entry{
			"env": {Value: "prod", Type: "string"},
		})
		result, err := extractStateData(&provider.ExecutionResult{
			Output: provider.Output{Data: map[string]any{
				"success": true,
				"data":    expected,
			}},
		})
		assert.NoError(t, err)
		assert.Equal(t, expected, result)
	})

	t.Run("map fallback", func(t *testing.T) {
		t.Parallel()
		// Simulate what happens when a provider returns data as map[string]any
		// (e.g., after JSON round-trip through a plugin boundary).
		dataMap := map[string]any{
			"schemaVersion": float64(1),
			"metadata": map[string]any{
				"solution":       "test-app",
				"version":        "1.0.0",
				"scafctlVersion": "dev",
				"createdAt":      "2025-01-01T00:00:00Z",
				"lastUpdatedAt":  "2025-01-01T00:00:00Z",
			},
			"command": map[string]any{
				"subcommand": "run solution",
				"parameters": map[string]any{},
			},
			"values": map[string]any{
				"region": map[string]any{
					"value":     "us-east-1",
					"type":      "string",
					"updatedAt": "2025-01-01T00:00:00Z",
					"immutable": false,
				},
			},
		}
		result, err := extractStateData(&provider.ExecutionResult{
			Output: provider.Output{Data: map[string]any{
				"success": true,
				"data":    dataMap,
			}},
		})
		assert.NoError(t, err)
		assert.Equal(t, 1, result.SchemaVersion)
		assert.Equal(t, "test-app", result.Metadata.Solution)
		assert.Contains(t, result.Values, "region")
		assert.Equal(t, "us-east-1", result.Values["region"].Value)
	})

	t.Run("unsupported type", func(t *testing.T) {
		t.Parallel()
		_, err := extractStateData(&provider.ExecutionResult{
			Output: provider.Output{Data: map[string]any{
				"success": true,
				"data":    42,
			}},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "expected *Data or map[string]any")
	})
}

func TestManagerLoad_ParamsAsParams(t *testing.T) {
	t.Run("CEL __params in backend inputs", func(t *testing.T) {
		backend := &mockBackendProvider{}
		reg := newTestRegistry(t, backend)

		expr := celexp.Expression("'gcp/' + __params.project + '/state.json'")
		cfg := &Config{
			Enabled: literalValueRef(true),
			Backend: Backend{
				Provider: "mock-state",
				Inputs:   map[string]*spec.ValueRef{"path": {Expr: &expr}},
			},
		}
		mgr := NewManager(cfg, reg, "v")
		params := map[string]any{"project": "my-proj"}
		result, err := mgr.Load(context.Background(), params, CommandInfo{Subcommand: "run solution"})
		assert.NoError(t, err)
		assert.False(t, result.Skipped)
	})

	t.Run("CEL __params in enabled", func(t *testing.T) {
		backend := &mockBackendProvider{}
		reg := newTestRegistry(t, backend)

		expr := celexp.Expression("__params.state_enabled == true")
		cfg := &Config{
			Enabled: &spec.ValueRef{Expr: &expr},
			Backend: Backend{
				Provider: "mock-state",
				Inputs:   map[string]*spec.ValueRef{},
			},
		}
		mgr := NewManager(cfg, reg, "v")

		// enabled=true
		result, err := mgr.Load(context.Background(), map[string]any{"state_enabled": true}, CommandInfo{})
		assert.NoError(t, err)
		assert.False(t, result.Skipped)

		// enabled=false
		result, err = mgr.Load(context.Background(), map[string]any{"state_enabled": false}, CommandInfo{})
		assert.NoError(t, err)
		assert.True(t, result.Skipped)
	})

	t.Run("Go template __params in backend inputs", func(t *testing.T) {
		backend := &mockBackendProvider{}
		reg := newTestRegistry(t, backend)

		tmpl := gotmpl.GoTemplatingContent("gcp/{{ .__params.project }}/state.json")
		cfg := &Config{
			Enabled: literalValueRef(true),
			Backend: Backend{
				Provider: "mock-state",
				Inputs:   map[string]*spec.ValueRef{"path": {Tmpl: &tmpl}},
			},
		}
		mgr := NewManager(cfg, reg, "v")
		params := map[string]any{"project": "my-proj"}
		result, err := mgr.Load(context.Background(), params, CommandInfo{})
		assert.NoError(t, err)
		assert.False(t, result.Skipped)
	})

	t.Run("nil params does not panic", func(t *testing.T) {
		backend := &mockBackendProvider{}
		reg := newTestRegistry(t, backend)

		cfg := &Config{
			Enabled: literalValueRef(true),
			Backend: Backend{
				Provider: "mock-state",
				Inputs:   map[string]*spec.ValueRef{"path": literalValueRef("default.json")},
			},
		}
		mgr := NewManager(cfg, reg, "v")
		result, err := mgr.Load(context.Background(), nil, CommandInfo{})
		assert.NoError(t, err)
		assert.False(t, result.Skipped)
	})
}

func TestResolveWithParams(t *testing.T) {
	t.Run("nil valueref returns nil", func(t *testing.T) {
		val, err := resolveWithParams(context.Background(), nil, nil, nil)
		assert.NoError(t, err)
		assert.Nil(t, val)
	})

	t.Run("literal ignores params", func(t *testing.T) {
		vr := literalValueRef("static")
		val, err := resolveWithParams(context.Background(), vr, nil, map[string]any{"key": "val"})
		assert.NoError(t, err)
		assert.Equal(t, "static", val)
	})

	t.Run("CEL uses __params", func(t *testing.T) {
		expr := celexp.Expression("__params.name + '-state.json'")
		vr := &spec.ValueRef{Expr: &expr}
		val, err := resolveWithParams(context.Background(), vr, nil, map[string]any{"name": "myapp"})
		assert.NoError(t, err)
		assert.Equal(t, "myapp-state.json", val)
	})

	t.Run("CEL uses both _ and __params", func(t *testing.T) {
		expr := celexp.Expression("_.resolver_out + '/' + __params.project")
		vr := &spec.ValueRef{Expr: &expr}
		resolverData := map[string]any{"resolver_out": "computed"}
		params := map[string]any{"project": "my-proj"}
		val, err := resolveWithParams(context.Background(), vr, resolverData, params)
		assert.NoError(t, err)
		assert.Equal(t, "computed/my-proj", val)
	})

	t.Run("template uses __params", func(t *testing.T) {
		tmpl := gotmpl.GoTemplatingContent("{{ .__params.project }}/state.json")
		vr := &spec.ValueRef{Tmpl: &tmpl}
		val, err := resolveWithParams(context.Background(), vr, nil, map[string]any{"project": "my-proj"})
		assert.NoError(t, err)
		assert.Equal(t, "my-proj/state.json", val)
	})

	t.Run("empty valueref returns error", func(t *testing.T) {
		vr := &spec.ValueRef{}
		_, err := resolveWithParams(context.Background(), vr, nil, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "empty value reference")
	})
}
