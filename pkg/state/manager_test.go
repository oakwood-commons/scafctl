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
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	existingState.Parameters["env"] = "prod"

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

			mgr := NewManager(tt.config, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
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

// TestManagerLoad_ResultFields verifies the LoadResult enrichment fields
// (FirstRun, LoadedParams, LoadedResolvers, Location) that command-layer
// callers use to report what state was found before a run.
func TestManagerLoad_ResultFields(t *testing.T) {
	t.Parallel()

	t.Run("first run reports FirstRun true and zero counts", func(t *testing.T) {
		t.Parallel()
		// No loadData: the mock returns NewData(), a fresh document with a
		// zero CreatedAt -- exactly what a backend with nothing saved yet
		// produces.
		backend := &mockBackendProvider{}
		reg := provider.NewRegistry()
		require.NoError(t, reg.Register(backend))
		cfg := &Config{
			Enabled: literalValueRef(true),
			Backend: Backend{
				Provider: "mock-state",
				Inputs:   map[string]*spec.ValueRef{"path": literalValueRef("state.json")},
			},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		result, err := mgr.Load(context.Background(), nil, CommandInfo{})
		require.NoError(t, err)
		assert.True(t, result.FirstRun)
		assert.Equal(t, 0, result.LoadedParams)
		assert.Equal(t, 0, result.LoadedResolvers)
		assert.Equal(t, "state.json", result.Location)
	})

	t.Run("replay reports FirstRun false with prior counts", func(t *testing.T) {
		t.Parallel()
		existing := NewData()
		existing.Metadata.CreatedAt = time.Now().Add(-time.Hour)
		existing.Parameters["env"] = "prod"
		existing.Parameters["app"] = "demo"
		existing.Resolvers["deployment_id"] = &PersistedEntry{Value: "abc", Type: "string", Immutable: true}

		backend := &mockBackendProvider{loadData: existing}
		reg := provider.NewRegistry()
		require.NoError(t, reg.Register(backend))
		cfg := &Config{
			Enabled: literalValueRef(true),
			Backend: Backend{
				Provider: "mock-state",
				Inputs:   map[string]*spec.ValueRef{"path": literalValueRef("state.json")},
			},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		result, err := mgr.Load(context.Background(), nil, CommandInfo{})
		require.NoError(t, err)
		assert.False(t, result.FirstRun)
		assert.Equal(t, 2, result.LoadedParams)
		assert.Equal(t, 1, result.LoadedResolvers)
		assert.Equal(t, "state.json", result.Location)
	})

	t.Run("location falls back to the url input when path is absent", func(t *testing.T) {
		t.Parallel()
		backend := &mockBackendProvider{}
		reg := provider.NewRegistry()
		require.NoError(t, reg.Register(backend))
		cfg := &Config{
			Enabled: literalValueRef(true),
			Backend: Backend{
				Provider: "mock-state",
				Inputs:   map[string]*spec.ValueRef{"url": literalValueRef("https://example.com/state")},
			},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		result, err := mgr.Load(context.Background(), nil, CommandInfo{})
		require.NoError(t, err)
		assert.Equal(t, "https://example.com/state", result.Location)
	})

	t.Run("location is empty when the backend uses neither path nor url", func(t *testing.T) {
		t.Parallel()
		backend := &mockBackendProvider{}
		reg := provider.NewRegistry()
		require.NoError(t, reg.Register(backend))
		cfg := &Config{
			Enabled: literalValueRef(true),
			Backend: Backend{
				Provider: "mock-state",
				Inputs:   map[string]*spec.ValueRef{"bucket": literalValueRef("my-bucket")},
			},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		result, err := mgr.Load(context.Background(), nil, CommandInfo{})
		require.NoError(t, err)
		assert.Empty(t, result.Location)
	})

	t.Run("Provider is populated from the backend config", func(t *testing.T) {
		t.Parallel()
		backend := &mockBackendProvider{}
		reg := provider.NewRegistry()
		require.NoError(t, reg.Register(backend))
		cfg := &Config{
			Enabled: literalValueRef(true),
			Backend: Backend{
				Provider: "mock-state",
				Inputs:   map[string]*spec.ValueRef{"path": literalValueRef("state.json")},
			},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		result, err := mgr.Load(context.Background(), nil, CommandInfo{})
		require.NoError(t, err)
		assert.Equal(t, "mock-state", result.Provider)
	})

	t.Run("a replayed intent document is not mistaken for a first run", func(t *testing.T) {
		t.Parallel()
		// The lean "intent" projection deliberately omits CreatedAt (see
		// projectState/Intent), so a document produced by that format has a
		// zero CreatedAt even though it carries replayed parameters. FirstRun
		// must not key off CreatedAt alone, or replaying a committed intent
		// document (the --state-file headline path) would be misreported as
		// "no prior state" while parameters were actually reused.
		intentLike := NewData()
		// CreatedAt intentionally left zero, matching a decoded intent doc.
		intentLike.Parameters["appName"] = "my-app"
		intentLike.Parameters["environment"] = "sandbox"

		backend := &mockBackendProvider{loadData: intentLike}
		reg := provider.NewRegistry()
		require.NoError(t, reg.Register(backend))
		cfg := &Config{
			Enabled: literalValueRef(true),
			Backend: Backend{
				Provider: "mock-state",
				Inputs:   map[string]*spec.ValueRef{"path": literalValueRef("intent.json")},
			},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		result, err := mgr.Load(context.Background(), nil, CommandInfo{})
		require.NoError(t, err)
		assert.False(t, result.FirstRun, "loaded parameters must override a zero CreatedAt")
		assert.Equal(t, 2, result.LoadedParams)
	})
}

func TestManagerSave(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		backend *mockBackendProvider
		state   *Data
		params  map[string]any
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
			name: "persists merged parameters",
			config: &Config{
				Enabled: literalValueRef(true),
				Backend: Backend{
					Provider: "mock-state",
					Inputs:   map[string]*spec.ValueRef{},
				},
			},
			backend: &mockBackendProvider{},
			state:   NewData(),
			params:  map[string]any{"key1": "val1", "key2": "val2"},
			setup: func() (*resolver.Context, []*resolver.Resolver) {
				rctx := resolver.NewContext()
				rctx.SetResult("api_key", &resolver.ExecutionResult{
					Value:  "secret123",
					Status: resolver.ExecutionStatusSuccess,
				})
				resolvers := []*resolver.Resolver{
					{Name: "api_key", Type: "string"},
				}
				return rctx, resolvers
			},
			check: func(t *testing.T, sd *Data, backend *mockBackendProvider) {
				// merged params should be saved
				assert.Equal(t, "val1", sd.Parameters["key1"])
				assert.Equal(t, "val2", sd.Parameters["key2"])

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
				assert.Equal(t, "test-version", sd.Metadata.Runtime.Engine.Version)
				assert.Equal(t, "scafctl", sd.Metadata.Runtime.Engine.Name)
				assert.False(t, sd.Metadata.CreatedAt.IsZero())
				assert.False(t, sd.Metadata.LastUpdatedAt.IsZero())
			},
		},
		{
			name: "upgrades schema version on save",
			config: &Config{
				Enabled: literalValueRef(true),
				Backend: Backend{
					Provider: "mock-state",
					Inputs:   map[string]*spec.ValueRef{},
				},
			},
			backend: &mockBackendProvider{},
			// Simulate a state file loaded under an older, still-supported
			// schema. Saving must re-stamp it with the current schema version so
			// the on-disk format matches the content actually written.
			state: func() *Data {
				d := NewData()
				d.SchemaVersion = SchemaVersionMinimum
				return d
			}(),
			setup: func() (*resolver.Context, []*resolver.Resolver) {
				return resolver.NewContext(), nil
			},
			check: func(t *testing.T, sd *Data, backend *mockBackendProvider) {
				assert.Equal(t, SchemaVersionCurrent, sd.SchemaVersion)
				// The persisted document must carry the upgraded version too.
				require.Len(t, backend.saveCalls, 1)
				data, ok := backend.saveCalls[0]["data"].(map[string]any)
				require.True(t, ok, "save inputs must include the state data map")
				assert.EqualValues(t, SchemaVersionCurrent, data["schemaVersion"])
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

			mgr := NewManager(tt.config, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
			rctx, resolvers := tt.setup()
			solMeta := SolutionMeta{Name: "my-app", Version: "2.0.0"}

			_, err := mgr.Save(context.Background(), tt.state, rctx, resolvers, tt.params, nil, solMeta)
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

func TestManagerSaveImmutablesAndParams(t *testing.T) {
	baseConfig := &Config{
		Enabled: literalValueRef(true),
		Backend: Backend{
			Provider: "mock-state",
			Inputs:   map[string]*spec.ValueRef{},
		},
	}
	solMeta := SolutionMeta{Name: "my-app", Version: "1.0.0"}

	t.Run("SaveImmutables locks immutable without persisting parameters", func(t *testing.T) {
		backend := &mockBackendProvider{}
		reg := newTestRegistry(t, backend)
		mgr := NewManager(baseConfig, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})

		rctx := resolver.NewContext()
		rctx.SetResult("cluster_id", &resolver.ExecutionResult{
			Value:  "uuid-1234",
			Status: resolver.ExecutionStatusSuccess,
		})
		resolvers := []*resolver.Resolver{
			{Name: "cluster_id", Type: "string", Immutable: true},
		}
		sd := NewData()

		err := mgr.SaveImmutables(context.Background(), sd, rctx, resolvers,
			map[string]any{"appName": "demo"}, nil, solMeta, nil)
		require.NoError(t, err)

		// Immutable locked, but parameters were not persisted to the document.
		assert.Equal(t, "uuid-1234", sd.Resolvers["cluster_id"].Value)
		assert.Empty(t, sd.Parameters)
		assert.Len(t, backend.saveCalls, 1)
	})

	t.Run("SaveImmutables skips resolvers whose deferred validation failed", func(t *testing.T) {
		backend := &mockBackendProvider{}
		reg := newTestRegistry(t, backend)
		mgr := NewManager(baseConfig, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})

		rctx := resolver.NewContext()
		rctx.SetResult("cluster_id", &resolver.ExecutionResult{
			Value:  "uuid-1234",
			Status: resolver.ExecutionStatusSuccess,
		})
		resolvers := []*resolver.Resolver{
			{Name: "cluster_id", Type: "string", Immutable: true},
		}
		sd := NewData()

		err := mgr.SaveImmutables(context.Background(), sd, rctx, resolvers,
			nil, nil, solMeta, map[string]bool{"cluster_id": true})
		require.NoError(t, err)

		// The failed immutable must NOT be locked.
		assert.NotContains(t, sd.Resolvers, "cluster_id")
	})

	t.Run("SaveParams persists the merged parameter set", func(t *testing.T) {
		backend := &mockBackendProvider{}
		reg := newTestRegistry(t, backend)
		mgr := NewManager(baseConfig, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})

		sd := NewData()
		params := map[string]any{"appName": "demo", "region": "us-east1"}

		_, err := mgr.SaveParams(context.Background(), sd, params, nil, solMeta)
		require.NoError(t, err)

		assert.Equal(t, params, sd.Parameters)
		assert.Len(t, backend.saveCalls, 1)
	})

	t.Run("nil config is a no-op", func(t *testing.T) {
		mgr := NewManager(nil, provider.NewRegistry(), settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		sd := NewData()

		require.NoError(t, mgr.SaveImmutables(context.Background(), sd, resolver.NewContext(), nil, nil, nil, solMeta, nil))
		_, err := mgr.SaveParams(context.Background(), sd, nil, nil, solMeta)
		require.NoError(t, err)
	})
}

// TestManagerSaveImmutablesAndParams_EmitWrittenOnce verifies that an Emit
// target is written exactly once per run -- from the final SaveParams commit
// -- even though SaveImmutables (the interim pre-action lock commit) runs
// first and also commits to the primary backend. A side-effecting emit
// backend (e.g. a REST endpoint) must not be invoked twice for one
// successful run, and must not be invoked at all when SaveImmutables is the
// only commit that happens (e.g. because a later action fails and
// SaveParams is never reached).
func TestManagerSaveImmutablesAndParams_EmitWrittenOnce(t *testing.T) {
	t.Parallel()

	newMgr := func(t *testing.T) (*Manager, *mockBackendProvider, *mockBackendProvider) {
		t.Helper()
		primary := &mockBackendProvider{}
		emit := &mockBackendProvider{}
		reg := provider.NewRegistry()
		require.NoError(t, reg.Register(namedMockBackend("mock-primary", primary)))
		require.NoError(t, reg.Register(namedMockBackend("mock-emit", emit)))
		cfg := &Config{
			Enabled: literalValueRef(true),
			Backend: Backend{Provider: "mock-primary", Inputs: map[string]*spec.ValueRef{"path": literalValueRef("state.json")}},
			Emit: []EmitTarget{
				{Backend: Backend{Provider: "mock-emit", Format: FormatIntent, Inputs: map[string]*spec.ValueRef{"path": literalValueRef("intent.json")}}},
			},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		return mgr, primary, emit
	}
	solMeta := SolutionMeta{Name: "app", Version: "1.0.0"}

	t.Run("a full solution/action run writes the emit target exactly once", func(t *testing.T) {
		t.Parallel()
		mgr, primary, emit := newMgr(t)
		sd := NewData()

		// Mirrors run solution/run action: SaveImmutables runs before actions,
		// SaveParams runs after actions succeed.
		require.NoError(t, mgr.SaveImmutables(context.Background(), sd, resolver.NewContext(), nil, nil, nil, solMeta, nil))
		result, err := mgr.SaveParams(context.Background(), sd, map[string]any{"env": "prod"}, nil, solMeta)
		require.NoError(t, err)
		require.NotNil(t, result)

		assert.Len(t, primary.saveCalls, 2, "the primary is intentionally saved at both the interim lock commit and the final commit")
		assert.Len(t, emit.saveCalls, 1, "the emit target must be saved exactly once, at the final commit")
		require.Len(t, result.Emits, 1)
		assert.False(t, result.Emits[0].Skipped)
	})

	t.Run("SaveImmutables alone (e.g. a later action fails) never writes the emit target", func(t *testing.T) {
		t.Parallel()
		mgr, primary, emit := newMgr(t)
		sd := NewData()

		require.NoError(t, mgr.SaveImmutables(context.Background(), sd, resolver.NewContext(), nil, nil, nil, solMeta, nil))

		assert.Len(t, primary.saveCalls, 1, "the interim lock commit still saves the primary")
		assert.Empty(t, emit.saveCalls, "a run that never reaches SaveParams must not publish an emit target")
	})
}

func TestManagerSave_Immutable(t *testing.T) {
	baseConfig := &Config{
		Enabled: literalValueRef(true),
		Backend: Backend{
			Provider: "mock-state",
			Inputs:   map[string]*spec.ValueRef{},
		},
	}

	t.Run("first write saves immutable entry", func(t *testing.T) {
		backend := &mockBackendProvider{}
		reg := newTestRegistry(t, backend)
		mgr := NewManager(baseConfig, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})

		rctx := resolver.NewContext()
		rctx.SetResult("cluster_id", &resolver.ExecutionResult{
			Value:  "uuid-1234",
			Status: resolver.ExecutionStatusSuccess,
		})
		resolvers := []*resolver.Resolver{
			{Name: "cluster_id", Type: "string", Immutable: true},
		}
		sd := NewData()
		solMeta := SolutionMeta{Name: "my-app", Version: "1.0.0"}

		_, err := mgr.Save(context.Background(), sd, rctx, resolvers, nil, nil, solMeta)
		assert.NoError(t, err)
		assert.Contains(t, sd.Resolvers, "cluster_id")
		assert.Equal(t, "uuid-1234", sd.Resolvers["cluster_id"].Value)
		assert.Equal(t, "string", sd.Resolvers["cluster_id"].Type)
	})

	t.Run("same value is no-op", func(t *testing.T) {
		backend := &mockBackendProvider{}
		reg := newTestRegistry(t, backend)
		mgr := NewManager(baseConfig, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})

		rctx := resolver.NewContext()
		rctx.SetResult("cluster_id", &resolver.ExecutionResult{
			Value:  "uuid-1234",
			Status: resolver.ExecutionStatusSuccess,
		})
		resolvers := []*resolver.Resolver{
			{Name: "cluster_id", Type: "string", Immutable: true},
		}

		// Pre-populate state with the same value
		sd := NewData()
		sd.Resolvers["cluster_id"] = &PersistedEntry{
			Immutable: true,
			Value:     "uuid-1234",
			Type:      "string",
			CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		}
		solMeta := SolutionMeta{Name: "my-app", Version: "1.0.0"}

		_, err := mgr.Save(context.Background(), sd, rctx, resolvers, nil, nil, solMeta)
		assert.NoError(t, err)
		// CreatedAt should remain unchanged (entry was skipped)
		assert.Equal(t, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), sd.Resolvers["cluster_id"].CreatedAt)
	})

	t.Run("different value errors", func(t *testing.T) {
		backend := &mockBackendProvider{}
		reg := newTestRegistry(t, backend)
		mgr := NewManager(baseConfig, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})

		rctx := resolver.NewContext()
		rctx.SetResult("cluster_id", &resolver.ExecutionResult{
			Value:  "uuid-5678-new",
			Status: resolver.ExecutionStatusSuccess,
		})
		resolvers := []*resolver.Resolver{
			{Name: "cluster_id", Type: "string", Immutable: true},
		}

		// Pre-populate state with different value
		sd := NewData()
		sd.Resolvers["cluster_id"] = &PersistedEntry{
			Immutable: true,
			Value:     "uuid-1234-old",
			Type:      "string",
			CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		}
		solMeta := SolutionMeta{Name: "my-app", Version: "1.0.0"}

		_, err := mgr.Save(context.Background(), sd, rctx, resolvers, nil, nil, solMeta)
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrImmutableEntry)
		assert.Contains(t, err.Error(), "cluster_id")
		assert.Contains(t, err.Error(), "state delete")

		// Backend should NOT have been called (save aborted)
		assert.Empty(t, backend.saveCalls)
	})

	t.Run("non-immutable resolver does not create entry", func(t *testing.T) {
		backend := &mockBackendProvider{}
		reg := newTestRegistry(t, backend)
		mgr := NewManager(baseConfig, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})

		rctx := resolver.NewContext()
		rctx.SetResult("env", &resolver.ExecutionResult{
			Value:  "prod",
			Status: resolver.ExecutionStatusSuccess,
		})
		resolvers := []*resolver.Resolver{
			{Name: "env", Type: "string", Immutable: false},
		}
		sd := NewData()
		solMeta := SolutionMeta{Name: "my-app", Version: "1.0.0"}

		_, err := mgr.Save(context.Background(), sd, rctx, resolvers, nil, nil, solMeta)
		assert.NoError(t, err)
		assert.NotContains(t, sd.Resolvers, "env")
	})
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

func TestStructToMap(t *testing.T) {
	t.Run("converts struct to map", func(t *testing.T) {
		sd := NewData()
		sd.SchemaVersion = 1
		sd.Metadata.Solution = "test-app"
		sd.Parameters["key1"] = "val1"

		m, err := structToMap(sd)
		assert.NoError(t, err)
		assert.Equal(t, float64(1), m["schemaVersion"])

		meta, ok := m["metadata"].(map[string]any)
		assert.True(t, ok)
		assert.Equal(t, "test-app", meta["solution"])

		params, ok := m["parameters"].(map[string]any)
		assert.True(t, ok)
		assert.Equal(t, "val1", params["key1"])
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
		expected := NewMockData("test", "1.0.0", map[string]any{
			"env": "prod",
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
			"schemaVersion": float64(SchemaVersionCurrent),
			"metadata": map[string]any{
				"solution": "test-app",
				"version":  "1.0.0",
				"runtime": map[string]any{
					"engine": map[string]any{"name": "scafctl", "version": "dev"},
					"cli":    map[string]any{"name": "scafctl", "version": "dev"},
				},
				"createdAt":     "2025-01-01T00:00:00Z",
				"lastUpdatedAt": "2025-01-01T00:00:00Z",
			},
			"command": map[string]any{
				"subcommand": "run solution",
				"parameters": map[string]any{},
			},
			"parameters": map[string]any{
				"region": "us-east-1",
			},
			"resolvers":    map[string]any{},
			"fingerprints": map[string]any{},
		}
		result, err := extractStateData(&provider.ExecutionResult{
			Output: provider.Output{Data: map[string]any{
				"success": true,
				"data":    dataMap,
			}},
		})
		assert.NoError(t, err)
		assert.Equal(t, SchemaVersionCurrent, result.SchemaVersion)
		assert.Equal(t, "test-app", result.Metadata.Solution)
		assert.Contains(t, result.Parameters, "region")
		assert.Equal(t, "us-east-1", result.Parameters["region"])
	})

	t.Run("map fallback out-of-range version", func(t *testing.T) {
		t.Parallel()
		// A map-form state whose schemaVersion is below the floor must surface
		// the actionable version error, not a raw decode error -- even when a
		// field type no longer matches the current struct.
		dataMap := map[string]any{
			"schemaVersion": float64(1),
			"metadata":      map[string]any{"solution": map[string]any{"nested": "object"}},
		}
		_, err := extractStateData(&provider.ExecutionResult{
			Output: provider.Output{Data: map[string]any{
				"success": true,
				"data":    dataMap,
			}},
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrIncompatibleSchemaVersion)
	})

	t.Run("direct pointer out-of-range version", func(t *testing.T) {
		t.Parallel()
		// An in-process provider (e.g. an embedder's custom backend) returning a
		// *Data with an out-of-range schema version must also be rejected.
		bad := NewData()
		bad.SchemaVersion = SchemaVersionCurrent + 1
		_, err := extractStateData(&provider.ExecutionResult{
			Output: provider.Output{Data: map[string]any{
				"success": true,
				"data":    bad,
			}},
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrUnsupportedSchemaVersion)
	})

	t.Run("direct pointer normalizes nil maps", func(t *testing.T) {
		t.Parallel()
		// An in-process provider that constructs a *Data directly may leave the
		// maps nil. extractStateData must normalize the direct-pointer path so it
		// has the same postconditions as the map fallback (which normalizes via
		// DecodeData); otherwise downstream reads/writes could nil-map panic.
		bare := &Data{SchemaVersion: SchemaVersionCurrent}
		require.Nil(t, bare.Resolvers)
		require.Nil(t, bare.Parameters)
		require.Nil(t, bare.Fingerprints)
		require.Nil(t, bare.Command.Parameters)

		result, err := extractStateData(&provider.ExecutionResult{
			Output: provider.Output{Data: map[string]any{
				"success": true,
				"data":    bare,
			}},
		})
		require.NoError(t, err)
		assert.NotNil(t, result.Resolvers)
		assert.NotNil(t, result.Parameters)
		assert.NotNil(t, result.Fingerprints)
		assert.NotNil(t, result.Command.Parameters)
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

	t.Run("found false yields fresh state without data", func(t *testing.T) {
		t.Parallel()
		// A backend that reports an absent object (found:false) must yield fresh
		// empty state without a data field and without a version error.
		result, err := extractStateData(&provider.ExecutionResult{
			Output: provider.Output{Data: map[string]any{
				"success":      true,
				OutputKeyFound: false,
			}},
		})
		require.NoError(t, err)
		assert.Equal(t, SchemaVersionCurrent, result.SchemaVersion)
		assert.Empty(t, result.Parameters)
	})

	t.Run("found false wins over zero-version data", func(t *testing.T) {
		t.Parallel()
		// Even when a backend returns a zero-value document alongside found:false,
		// the absence signal takes precedence and no version guard is applied.
		result, err := extractStateData(&provider.ExecutionResult{
			Output: provider.Output{Data: map[string]any{
				"success":      true,
				OutputKeyFound: false,
				OutputKeyData:  &Data{},
			}},
		})
		require.NoError(t, err)
		assert.Equal(t, SchemaVersionCurrent, result.SchemaVersion)
	})

	t.Run("direct pointer empty document is fresh state", func(t *testing.T) {
		t.Parallel()
		// A zero-value *Data (no found signal) is the in-process equivalent of a
		// contentless payload and must be treated as fresh state, not a v0 file.
		result, err := extractStateData(&provider.ExecutionResult{
			Output: provider.Output{Data: map[string]any{
				"success":     true,
				OutputKeyData: &Data{},
			}},
		})
		require.NoError(t, err)
		assert.Equal(t, SchemaVersionCurrent, result.SchemaVersion)
		assert.NotNil(t, result.Resolvers)
	})

	t.Run("map fallback empty object is fresh state", func(t *testing.T) {
		t.Parallel()
		// A plugin backend that returns an empty map on not-found (instead of the
		// found signal) must still produce fresh state rather than a v0 error.
		result, err := extractStateData(&provider.ExecutionResult{
			Output: provider.Output{Data: map[string]any{
				"success":     true,
				OutputKeyData: map[string]any{},
			}},
		})
		require.NoError(t, err)
		assert.Equal(t, SchemaVersionCurrent, result.SchemaVersion)
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
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "v"})
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
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "v"})

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
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "v"})
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
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "v"})
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

func TestManagerSave_SaveOverrides(t *testing.T) {
	t.Parallel()

	t.Run("no saveOverrides backward compat", func(t *testing.T) {
		t.Parallel()
		backend := &mockBackendProvider{}
		reg := newTestRegistry(t, backend)
		cfg := &Config{
			Enabled: literalValueRef(true),
			Backend: Backend{
				Provider: "mock-state",
				Inputs:   map[string]*spec.ValueRef{"path": literalValueRef("state.json")},
			},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		sd := NewData()
		rctx := resolver.NewContext()
		_, err := mgr.Save(context.Background(), sd, rctx, nil, nil, nil, SolutionMeta{Name: "app", Version: "1.0.0"})
		assert.NoError(t, err)
		assert.Len(t, backend.saveCalls, 1)
		assert.Equal(t, "state.json", backend.saveCalls[0]["path"])
	})

	t.Run("saveOverrides with literal values", func(t *testing.T) {
		t.Parallel()
		backend := &mockBackendProvider{}
		reg := newTestRegistry(t, backend)
		cfg := &Config{
			Enabled: literalValueRef(true),
			Backend: Backend{
				Provider: "mock-state",
				Inputs:   map[string]*spec.ValueRef{"path": literalValueRef("state.json")},
				SaveOverrides: map[string]*spec.ValueRef{
					"branch": literalValueRef("feature-branch"),
				},
			},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		sd := NewData()
		rctx := resolver.NewContext()
		_, err := mgr.Save(context.Background(), sd, rctx, nil, nil, nil, SolutionMeta{Name: "app", Version: "1.0.0"})
		assert.NoError(t, err)
		assert.Len(t, backend.saveCalls, 1)
		assert.Equal(t, "feature-branch", backend.saveCalls[0]["branch"])
		assert.Equal(t, "state.json", backend.saveCalls[0]["path"])
	})

	t.Run("saveOverrides with rslvr reference", func(t *testing.T) {
		t.Parallel()
		backend := &mockBackendProvider{}
		reg := newTestRegistry(t, backend)
		branchResolver := "featureBranch"
		cfg := &Config{
			Enabled: literalValueRef(true),
			Backend: Backend{
				Provider: "mock-state",
				Inputs:   map[string]*spec.ValueRef{"path": literalValueRef("state.json")},
				SaveOverrides: map[string]*spec.ValueRef{
					"branch": {Resolver: &branchResolver},
				},
			},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		sd := NewData()
		rctx := resolver.NewContext()
		resolverData := map[string]any{"featureBranch": "feat/my-feature"}
		_, err := mgr.Save(context.Background(), sd, rctx, nil, nil, resolverData, SolutionMeta{Name: "app", Version: "1.0.0"})
		assert.NoError(t, err)
		assert.Len(t, backend.saveCalls, 1)
		assert.Equal(t, "feat/my-feature", backend.saveCalls[0]["branch"])
	})

	t.Run("saveOverrides with CEL expression using resolver data", func(t *testing.T) {
		t.Parallel()
		backend := &mockBackendProvider{}
		reg := newTestRegistry(t, backend)
		expr := celexp.Expression("'feat/' + _.appName")
		cfg := &Config{
			Enabled: literalValueRef(true),
			Backend: Backend{
				Provider: "mock-state",
				Inputs:   map[string]*spec.ValueRef{"path": literalValueRef("state.json")},
				SaveOverrides: map[string]*spec.ValueRef{
					"branch": {Expr: &expr},
				},
			},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		sd := NewData()
		rctx := resolver.NewContext()
		resolverData := map[string]any{"appName": "my-app"}
		_, err := mgr.Save(context.Background(), sd, rctx, nil, nil, resolverData, SolutionMeta{Name: "app", Version: "1.0.0"})
		assert.NoError(t, err)
		assert.Len(t, backend.saveCalls, 1)
		assert.Equal(t, "feat/my-app", backend.saveCalls[0]["branch"])
	})

	t.Run("saveOverrides key overlaps with inputs key", func(t *testing.T) {
		t.Parallel()
		backend := &mockBackendProvider{}
		reg := newTestRegistry(t, backend)
		cfg := &Config{
			Enabled: literalValueRef(true),
			Backend: Backend{
				Provider: "mock-state",
				Inputs: map[string]*spec.ValueRef{
					"path":   literalValueRef("state.json"),
					"branch": literalValueRef("main"),
				},
				SaveOverrides: map[string]*spec.ValueRef{
					"branch": literalValueRef("feature-branch"),
				},
			},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		sd := NewData()
		rctx := resolver.NewContext()
		_, err := mgr.Save(context.Background(), sd, rctx, nil, nil, nil, SolutionMeta{Name: "app", Version: "1.0.0"})
		assert.NoError(t, err)
		assert.Len(t, backend.saveCalls, 1)
		// saveOverrides value should win
		assert.Equal(t, "feature-branch", backend.saveCalls[0]["branch"])
		// non-overridden input should remain
		assert.Equal(t, "state.json", backend.saveCalls[0]["path"])
	})

	t.Run("saveOverrides ignored at load time", func(t *testing.T) {
		t.Parallel()
		backend := &mockBackendProvider{}
		reg := newTestRegistry(t, backend)
		// saveOverrides with a rslvr: ref -- would fail if resolved at load time
		branchResolver := "featureBranch"
		cfg := &Config{
			Enabled: literalValueRef(true),
			Backend: Backend{
				Provider: "mock-state",
				Inputs:   map[string]*spec.ValueRef{"path": literalValueRef("state.json")},
				SaveOverrides: map[string]*spec.ValueRef{
					"branch": {Resolver: &branchResolver},
				},
			},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		// Load should succeed -- saveOverrides are not resolved
		result, err := mgr.Load(context.Background(), nil, CommandInfo{Subcommand: "run solution"})
		assert.NoError(t, err)
		assert.False(t, result.Skipped)
	})

	t.Run("saveOverrides resolution error", func(t *testing.T) {
		t.Parallel()
		backend := &mockBackendProvider{}
		reg := newTestRegistry(t, backend)
		expr := celexp.Expression("_.nonexistent.nested.value")
		cfg := &Config{
			Enabled: literalValueRef(true),
			Backend: Backend{
				Provider: "mock-state",
				Inputs:   map[string]*spec.ValueRef{"path": literalValueRef("state.json")},
				SaveOverrides: map[string]*spec.ValueRef{
					"branch": {Expr: &expr},
				},
			},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		sd := NewData()
		rctx := resolver.NewContext()
		_, err := mgr.Save(context.Background(), sd, rctx, nil, nil, nil, SolutionMeta{Name: "app", Version: "1.0.0"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "save overrides")
	})

	t.Run("saveOverrides with nil ValueRef entry", func(t *testing.T) {
		t.Parallel()
		backend := &mockBackendProvider{}
		reg := newTestRegistry(t, backend)
		cfg := &Config{
			Enabled: literalValueRef(true),
			Backend: Backend{
				Provider: "mock-state",
				Inputs:   map[string]*spec.ValueRef{"path": literalValueRef("state.json")},
				SaveOverrides: map[string]*spec.ValueRef{
					"branch": nil,
				},
			},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		sd := NewData()
		rctx := resolver.NewContext()
		_, err := mgr.Save(context.Background(), sd, rctx, nil, nil, nil, SolutionMeta{Name: "app", Version: "1.0.0"})
		assert.NoError(t, err)
		assert.Len(t, backend.saveCalls, 1)
		// nil saveOverride should be skipped, not overwrite
		_, hasBranch := backend.saveCalls[0]["branch"]
		assert.False(t, hasBranch)
	})
}

// TestManagerSave_Format verifies that Backend.Format controls what shape the
// primary backend's state_save receives.
func TestManagerSave_Format(t *testing.T) {
	t.Parallel()

	t.Run("full format saves the complete document", func(t *testing.T) {
		t.Parallel()
		backend := &mockBackendProvider{}
		reg := newTestRegistry(t, backend)
		cfg := &Config{
			Enabled: literalValueRef(true),
			Backend: Backend{
				Provider: "mock-state",
				Format:   FormatFull,
				Inputs:   map[string]*spec.ValueRef{"path": literalValueRef("state.json")},
			},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		sd := NewData()
		sd.Resolvers["cluster_id"] = &PersistedEntry{Value: "abc", Type: "string", Immutable: true}
		_, err := mgr.Save(context.Background(), sd, resolver.NewContext(), nil, nil, nil, SolutionMeta{Name: "app", Version: "1.0.0"})
		require.NoError(t, err)
		require.Len(t, backend.saveCalls, 1)

		data, ok := backend.saveCalls[0]["data"].(map[string]any)
		require.True(t, ok)
		assert.Contains(t, data, "resolvers", "full format must retain resolver locks")
	})

	t.Run("intent format drops resolver locks", func(t *testing.T) {
		t.Parallel()
		backend := &mockBackendProvider{}
		reg := newTestRegistry(t, backend)
		cfg := &Config{
			Enabled: literalValueRef(true),
			Backend: Backend{
				Provider: "mock-state",
				Format:   FormatIntent,
				Inputs:   map[string]*spec.ValueRef{"path": literalValueRef("intent.json")},
			},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		sd := NewData()
		sd.Resolvers["cluster_id"] = &PersistedEntry{Value: "abc", Type: "string", Immutable: true}
		_, err := mgr.Save(context.Background(), sd, resolver.NewContext(), nil, map[string]any{"env": "prod"}, nil, SolutionMeta{Name: "app", Version: "1.0.0"})
		require.NoError(t, err)
		require.Len(t, backend.saveCalls, 1)

		data, ok := backend.saveCalls[0]["data"].(map[string]any)
		require.True(t, ok)
		assert.NotContains(t, data, "resolvers", "intent format must omit resolver locks")
		params, ok := data["parameters"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "prod", params["env"])
	})

	t.Run("unknown format fails the save", func(t *testing.T) {
		t.Parallel()
		backend := &mockBackendProvider{}
		reg := newTestRegistry(t, backend)
		cfg := &Config{
			Enabled: literalValueRef(true),
			Backend: Backend{
				Provider: "mock-state",
				Format:   "bogus",
				Inputs:   map[string]*spec.ValueRef{"path": literalValueRef("state.json")},
			},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		_, err := mgr.Save(context.Background(), NewData(), resolver.NewContext(), nil, nil, nil, SolutionMeta{Name: "app", Version: "1.0.0"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown backend format")
		assert.Empty(t, backend.saveCalls, "the backend must not be called when projection fails")
	})
}

// TestManagerSave_Emit verifies the save-only Emit mechanism: multiple
// projected targets, each independently formatted and independently gated.
func TestManagerSave_Emit(t *testing.T) {
	t.Parallel()

	t.Run("primary and emit targets are both saved", func(t *testing.T) {
		t.Parallel()
		primary := &mockBackendProvider{}
		emitTarget := &mockBackendProvider{}
		reg := provider.NewRegistry()
		require.NoError(t, reg.Register(namedMockBackend("mock-primary", primary)))
		require.NoError(t, reg.Register(namedMockBackend("mock-emit", emitTarget)))

		cfg := &Config{
			Enabled: literalValueRef(true),
			Backend: Backend{
				Provider: "mock-primary",
				Format:   FormatFull,
				Inputs:   map[string]*spec.ValueRef{"path": literalValueRef(".scafctl/state.json")},
			},
			Emit: []EmitTarget{
				{
					Backend: Backend{
						Provider: "mock-emit",
						Format:   FormatIntent,
						Inputs:   map[string]*spec.ValueRef{"path": literalValueRef("intent/sandbox.json")},
					},
				},
			},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		sd := NewData()
		sd.Resolvers["cluster_id"] = &PersistedEntry{Value: "abc", Type: "string", Immutable: true}
		_, err := mgr.Save(context.Background(), sd, resolver.NewContext(), nil, map[string]any{"env": "prod"}, nil, SolutionMeta{Name: "app", Version: "1.0.0"})
		require.NoError(t, err)

		require.Len(t, primary.saveCalls, 1)
		primaryData, _ := primary.saveCalls[0]["data"].(map[string]any)
		assert.Contains(t, primaryData, "resolvers", "primary (full) save must retain resolver locks")

		require.Len(t, emitTarget.saveCalls, 1, "the emit target must also be saved")
		emitData, _ := emitTarget.saveCalls[0]["data"].(map[string]any)
		assert.NotContains(t, emitData, "resolvers", "emit (intent) save must omit resolver locks")
		emitParams, _ := emitData["parameters"].(map[string]any)
		assert.Equal(t, "prod", emitParams["env"])
	})

	t.Run("multiple emit targets are all saved", func(t *testing.T) {
		t.Parallel()
		primary := &mockBackendProvider{}
		emitA := &mockBackendProvider{}
		emitB := &mockBackendProvider{}
		reg := provider.NewRegistry()
		require.NoError(t, reg.Register(namedMockBackend("mock-primary", primary)))
		require.NoError(t, reg.Register(namedMockBackend("mock-emit-a", emitA)))
		require.NoError(t, reg.Register(namedMockBackend("mock-emit-b", emitB)))

		cfg := &Config{
			Enabled: literalValueRef(true),
			Backend: Backend{Provider: "mock-primary", Inputs: map[string]*spec.ValueRef{"path": literalValueRef("state.json")}},
			Emit: []EmitTarget{
				{Backend: Backend{Provider: "mock-emit-a", Format: FormatIntent, Inputs: map[string]*spec.ValueRef{"path": literalValueRef("a.json")}}},
				{Backend: Backend{Provider: "mock-emit-b", Format: FormatIntent, Inputs: map[string]*spec.ValueRef{"path": literalValueRef("b.json")}}},
			},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		_, err := mgr.Save(context.Background(), NewData(), resolver.NewContext(), nil, nil, nil, SolutionMeta{Name: "app", Version: "1.0.0"})
		require.NoError(t, err)

		assert.Len(t, primary.saveCalls, 1)
		assert.Len(t, emitA.saveCalls, 1)
		assert.Len(t, emitB.saveCalls, 1)
	})

	t.Run("emit enabled false skips that target", func(t *testing.T) {
		t.Parallel()
		primary := &mockBackendProvider{}
		emitTarget := &mockBackendProvider{}
		reg := provider.NewRegistry()
		require.NoError(t, reg.Register(namedMockBackend("mock-primary", primary)))
		require.NoError(t, reg.Register(namedMockBackend("mock-emit", emitTarget)))

		cfg := &Config{
			Enabled: literalValueRef(true),
			Backend: Backend{Provider: "mock-primary", Inputs: map[string]*spec.ValueRef{"path": literalValueRef("state.json")}},
			Emit: []EmitTarget{
				{
					Backend: Backend{Provider: "mock-emit", Format: FormatIntent, Inputs: map[string]*spec.ValueRef{"path": literalValueRef("intent.json")}},
					Enabled: literalValueRef(false),
				},
			},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		_, err := mgr.Save(context.Background(), NewData(), resolver.NewContext(), nil, nil, nil, SolutionMeta{Name: "app", Version: "1.0.0"})
		require.NoError(t, err)

		assert.Len(t, primary.saveCalls, 1, "the primary backend must still be saved")
		assert.Empty(t, emitTarget.saveCalls, "a disabled emit target must not be saved")
	})

	t.Run("emit enabled absent defaults to true", func(t *testing.T) {
		t.Parallel()
		primary := &mockBackendProvider{}
		emitTarget := &mockBackendProvider{}
		reg := provider.NewRegistry()
		require.NoError(t, reg.Register(namedMockBackend("mock-primary", primary)))
		require.NoError(t, reg.Register(namedMockBackend("mock-emit", emitTarget)))

		cfg := &Config{
			Enabled: literalValueRef(true),
			Backend: Backend{Provider: "mock-primary", Inputs: map[string]*spec.ValueRef{"path": literalValueRef("state.json")}},
			Emit: []EmitTarget{
				{Backend: Backend{Provider: "mock-emit", Format: FormatIntent, Inputs: map[string]*spec.ValueRef{"path": literalValueRef("intent.json")}}},
			},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		_, err := mgr.Save(context.Background(), NewData(), resolver.NewContext(), nil, nil, nil, SolutionMeta{Name: "app", Version: "1.0.0"})
		require.NoError(t, err)

		assert.Len(t, emitTarget.saveCalls, 1, "an emit target with no Enabled field must default to always-emit")
	})

	t.Run("emit enabled can reference any resolver, unlike the primary Enabled", func(t *testing.T) {
		t.Parallel()
		primary := &mockBackendProvider{}
		emitTarget := &mockBackendProvider{}
		reg := provider.NewRegistry()
		require.NoError(t, reg.Register(namedMockBackend("mock-primary", primary)))
		require.NoError(t, reg.Register(namedMockBackend("mock-emit", emitTarget)))

		expr := celexp.Expression("_.environment == 'sandbox'")
		cfg := &Config{
			Enabled: literalValueRef(true),
			Backend: Backend{Provider: "mock-primary", Inputs: map[string]*spec.ValueRef{"path": literalValueRef("state.json")}},
			Emit: []EmitTarget{
				{
					Backend: Backend{Provider: "mock-emit", Format: FormatIntent, Inputs: map[string]*spec.ValueRef{"path": literalValueRef("intent.json")}},
					Enabled: &spec.ValueRef{Expr: &expr},
				},
			},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})

		// resolverData ("_") carries the "environment" resolver's output, which
		// is exactly the kind of reference the primary Config.Enabled cannot make
		// at load time (it runs before all resolvers), but an Emit target's
		// Enabled can, because it is evaluated at save time after every resolver
		// has run.
		resolverData := map[string]any{"environment": "sandbox"}
		_, err := mgr.Save(context.Background(), NewData(), resolver.NewContext(), nil, nil, resolverData, SolutionMeta{Name: "app", Version: "1.0.0"})
		require.NoError(t, err)
		assert.Len(t, emitTarget.saveCalls, 1)
	})

	t.Run("emit target save error aborts remaining emit targets but not the primary result", func(t *testing.T) {
		t.Parallel()
		primary := &mockBackendProvider{}
		failingEmit := &mockBackendProvider{saveErr: assert.AnError}
		neverReached := &mockBackendProvider{}
		reg := provider.NewRegistry()
		require.NoError(t, reg.Register(namedMockBackend("mock-primary", primary)))
		require.NoError(t, reg.Register(namedMockBackend("mock-emit-fail", failingEmit)))
		require.NoError(t, reg.Register(namedMockBackend("mock-emit-never", neverReached)))

		cfg := &Config{
			Enabled: literalValueRef(true),
			Backend: Backend{Provider: "mock-primary", Inputs: map[string]*spec.ValueRef{"path": literalValueRef("state.json")}},
			Emit: []EmitTarget{
				{Backend: Backend{Provider: "mock-emit-fail", Inputs: map[string]*spec.ValueRef{"path": literalValueRef("a.json")}}},
				{Backend: Backend{Provider: "mock-emit-never", Inputs: map[string]*spec.ValueRef{"path": literalValueRef("b.json")}}},
			},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		_, err := mgr.Save(context.Background(), NewData(), resolver.NewContext(), nil, nil, nil, SolutionMeta{Name: "app", Version: "1.0.0"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "emit[0]")

		assert.Len(t, primary.saveCalls, 1, "the primary backend save already succeeded and is not undone")
		assert.Empty(t, neverReached.saveCalls, "an emit target after a failing one must not run")
	})

	t.Run("no emit targets behaves exactly as before emit existed", func(t *testing.T) {
		t.Parallel()
		primary := &mockBackendProvider{}
		reg := newTestRegistry(t, primary)
		cfg := &Config{
			Enabled: literalValueRef(true),
			Backend: Backend{Provider: "mock-state", Inputs: map[string]*spec.ValueRef{"path": literalValueRef("state.json")}},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		_, err := mgr.Save(context.Background(), NewData(), resolver.NewContext(), nil, nil, nil, SolutionMeta{Name: "app", Version: "1.0.0"})
		require.NoError(t, err)
		assert.Len(t, primary.saveCalls, 1)
	})
}

// namedMockBackend wraps a *mockBackendProvider so its Descriptor() reports a
// caller-chosen name, letting a single test registry host several distinct
// mock backends (e.g. a primary and one or more emit targets).
type namedMockBackendProvider struct {
	*mockBackendProvider
	name string
}

func (n *namedMockBackendProvider) Descriptor() *provider.Descriptor {
	desc := *n.mockBackendProvider.Descriptor()
	desc.Name = n.name
	return &desc
}

func namedMockBackend(name string, p *mockBackendProvider) provider.Provider {
	return &namedMockBackendProvider{mockBackendProvider: p, name: name}
}

// TestManagerSave_SaveResult verifies that Save and SaveParams return a
// SaveResult describing every backend actually written -- the primary and
// each Emit target, including targets skipped by their Enabled condition --
// so command-layer callers can report a save confirmation without
// re-deriving it from the state Config.
func TestManagerSave_SaveResult(t *testing.T) {
	t.Parallel()

	t.Run("reports the primary write's location and format", func(t *testing.T) {
		t.Parallel()
		primary := &mockBackendProvider{}
		reg := provider.NewRegistry()
		require.NoError(t, reg.Register(namedMockBackend("mock-primary", primary)))

		cfg := &Config{
			Enabled: literalValueRef(true),
			Backend: Backend{
				Provider: "mock-primary",
				Inputs:   map[string]*spec.ValueRef{"path": literalValueRef("state.json")},
			},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		result, err := mgr.Save(context.Background(), NewData(), resolver.NewContext(), nil, nil, nil, SolutionMeta{Name: "app", Version: "1.0.0"})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "mock-primary", result.Primary.Provider)
		assert.Equal(t, "state.json", result.Primary.Location)
		assert.Equal(t, FormatFull, result.Primary.Format, "an unset Format normalizes to FormatFull")
		assert.False(t, result.Primary.Skipped)
		assert.Empty(t, result.Emits)
	})

	t.Run("reports each emit target, including one skipped by its Enabled condition", func(t *testing.T) {
		t.Parallel()
		primary := &mockBackendProvider{}
		enabledEmit := &mockBackendProvider{}
		disabledEmit := &mockBackendProvider{}
		reg := provider.NewRegistry()
		require.NoError(t, reg.Register(namedMockBackend("mock-primary", primary)))
		require.NoError(t, reg.Register(namedMockBackend("mock-emit-on", enabledEmit)))
		require.NoError(t, reg.Register(namedMockBackend("mock-emit-off", disabledEmit)))

		cfg := &Config{
			Enabled: literalValueRef(true),
			Backend: Backend{Provider: "mock-primary", Inputs: map[string]*spec.ValueRef{"path": literalValueRef("state.json")}},
			Emit: []EmitTarget{
				{Backend: Backend{Provider: "mock-emit-on", Format: FormatIntent, Inputs: map[string]*spec.ValueRef{"path": literalValueRef("intent.json")}}},
				{
					Backend: Backend{Provider: "mock-emit-off", Format: FormatIntent, Inputs: map[string]*spec.ValueRef{"path": literalValueRef("skipped.json")}},
					Enabled: literalValueRef(false),
				},
			},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		result, err := mgr.Save(context.Background(), NewData(), resolver.NewContext(), nil, nil, nil, SolutionMeta{Name: "app", Version: "1.0.0"})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, result.Emits, 2)

		assert.Equal(t, "mock-emit-on", result.Emits[0].Provider)
		assert.Equal(t, "intent.json", result.Emits[0].Location)
		assert.Equal(t, FormatIntent, result.Emits[0].Format)
		assert.False(t, result.Emits[0].Skipped)

		assert.Equal(t, "mock-emit-off", result.Emits[1].Provider)
		assert.Equal(t, FormatIntent, result.Emits[1].Format, "a skipped target still reports its declared Format")
		assert.True(t, result.Emits[1].Skipped)
		assert.Empty(t, disabledEmit.saveCalls, "a skipped target must not actually be saved")
	})

	t.Run("an emit target's Parameters narrowing is applied to the actual saved payload", func(t *testing.T) {
		t.Parallel()
		primary := &mockBackendProvider{}
		emit := &mockBackendProvider{}
		reg := provider.NewRegistry()
		require.NoError(t, reg.Register(namedMockBackend("mock-primary", primary)))
		require.NoError(t, reg.Register(namedMockBackend("mock-emit", emit)))

		cfg := &Config{
			Enabled: literalValueRef(true),
			Backend: Backend{Provider: "mock-primary", Inputs: map[string]*spec.ValueRef{"path": literalValueRef("state.json")}},
			Emit: []EmitTarget{
				{Backend: Backend{
					Provider:   "mock-emit",
					Format:     FormatIntent,
					Parameters: &ParameterProjection{Include: []string{"appName"}},
					Inputs:     map[string]*spec.ValueRef{"path": literalValueRef("intent.json")},
				}},
			},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})

		sd := NewData()
		mergedParams := map[string]any{"appName": "hello", "mode": "publish"}
		result, err := mgr.Save(context.Background(), sd, resolver.NewContext(), nil, mergedParams, nil, SolutionMeta{Name: "app", Version: "1.0.0"})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, result.Emits, 1)

		require.Len(t, emit.saveCalls, 1)
		saved, ok := emit.saveCalls[0]["data"].(map[string]any)
		require.True(t, ok)
		params, ok := saved["parameters"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, map[string]any{"appName": "hello"}, params, "the emit target's saved payload must reflect its own Parameters narrowing, not the full merged set")

		// The primary backend is unaffected -- it has no narrowing spec.
		require.Len(t, primary.saveCalls, 1)
		primaryData, ok := primary.saveCalls[0]["data"].(map[string]any)
		require.True(t, ok)
		primaryParams, ok := primaryData["parameters"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, mergedParams, primaryParams, "the primary backend must save the full parameter set")
	})

	t.Run("SaveParams also returns a SaveResult", func(t *testing.T) {
		t.Parallel()
		primary := &mockBackendProvider{}
		reg := provider.NewRegistry()
		require.NoError(t, reg.Register(namedMockBackend("mock-primary", primary)))

		cfg := &Config{
			Enabled: literalValueRef(true),
			Backend: Backend{Provider: "mock-primary", Inputs: map[string]*spec.ValueRef{"path": literalValueRef("state.json")}},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		result, err := mgr.SaveParams(context.Background(), NewData(), nil, nil, SolutionMeta{Name: "app", Version: "1.0.0"})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "state.json", result.Primary.Location)
	})

	t.Run("nil config yields a nil SaveResult and nil error", func(t *testing.T) {
		t.Parallel()
		mgr := NewManager(nil, provider.NewRegistry(), settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		result, err := mgr.Save(context.Background(), NewData(), resolver.NewContext(), nil, nil, nil, SolutionMeta{Name: "app", Version: "1.0.0"})
		require.NoError(t, err)
		assert.Nil(t, result)
	})
}

func TestRequiredParams(t *testing.T) {
	ctx := context.Background()

	t.Run("nil config", func(t *testing.T) {
		result := RequiredParams(ctx, nil)
		assert.Nil(t, result)
	})

	t.Run("literal only", func(t *testing.T) {
		cfg := &Config{
			Enabled: literalValueRef(true),
			Backend: Backend{
				Provider: "file",
				Inputs: map[string]*spec.ValueRef{
					"path": literalValueRef("fixed-path.json"),
				},
			},
		}
		result := RequiredParams(ctx, cfg)
		assert.Nil(t, result)
	})

	t.Run("CEL expression", func(t *testing.T) {
		expr := celexp.Expression("'state/' + __params.app_name + '.json'")
		cfg := &Config{
			Backend: Backend{
				Provider: "file",
				Inputs: map[string]*spec.ValueRef{
					"path": {Expr: &expr},
				},
			},
		}
		result := RequiredParams(ctx, cfg)
		assert.Equal(t, []string{"app_name"}, result)
	})

	t.Run("Go template", func(t *testing.T) {
		tmpl := gotmpl.GoTemplatingContent("dynamic/{{ .__params.project }}.json")
		cfg := &Config{
			Backend: Backend{
				Provider: "file",
				Inputs: map[string]*spec.ValueRef{
					"path": {Tmpl: &tmpl},
				},
			},
		}
		result := RequiredParams(ctx, cfg)
		assert.Equal(t, []string{"project"}, result)
	})

	t.Run("enabled field extraction", func(t *testing.T) {
		expr := celexp.Expression("__params.state_enabled == true")
		cfg := &Config{
			Enabled: &spec.ValueRef{Expr: &expr},
			Backend: Backend{
				Provider: "file",
				Inputs: map[string]*spec.ValueRef{
					"path": literalValueRef("fixed.json"),
				},
			},
		}
		result := RequiredParams(ctx, cfg)
		assert.Equal(t, []string{"state_enabled"}, result)
	})

	t.Run("mixed CEL and template deduplicated", func(t *testing.T) {
		expr := celexp.Expression("__params.project + '/state.json'")
		tmpl := gotmpl.GoTemplatingContent("{{ .__params.project }}/{{ .__params.env }}.json")
		cfg := &Config{
			Backend: Backend{
				Provider: "file",
				Inputs: map[string]*spec.ValueRef{
					"path":   {Expr: &expr},
					"backup": {Tmpl: &tmpl},
				},
			},
		}
		result := RequiredParams(ctx, cfg)
		assert.Equal(t, []string{"env", "project"}, result)
	})

	t.Run("saveOverrides excluded", func(t *testing.T) {
		expr := celexp.Expression("__params.save_branch")
		cfg := &Config{
			Backend: Backend{
				Provider: "file",
				Inputs: map[string]*spec.ValueRef{
					"path": literalValueRef("fixed.json"),
				},
				SaveOverrides: map[string]*spec.ValueRef{
					"branch": {Expr: &expr},
				},
			},
		}
		result := RequiredParams(ctx, cfg)
		assert.Nil(t, result)
	})
}

func TestMissingParams(t *testing.T) {
	ctx := context.Background()

	tmpl := gotmpl.GoTemplatingContent("dynamic/{{ .__params.project }}.json")
	cfg := &Config{
		Backend: Backend{
			Provider: "file",
			Inputs: map[string]*spec.ValueRef{
				"path": {Tmpl: &tmpl},
			},
		},
	}

	t.Run("all present", func(t *testing.T) {
		params := map[string]any{"project": "alpha"}
		result := MissingParams(ctx, cfg, params)
		assert.Nil(t, result)
	})

	t.Run("some missing", func(t *testing.T) {
		params := map[string]any{"other": "value"}
		result := MissingParams(ctx, cfg, params)
		assert.Equal(t, []string{"project"}, result)
	})

	t.Run("none supplied", func(t *testing.T) {
		result := MissingParams(ctx, cfg, nil)
		assert.Equal(t, []string{"project"}, result)
	})

	t.Run("empty params map", func(t *testing.T) {
		result := MissingParams(ctx, cfg, map[string]any{})
		assert.Equal(t, []string{"project"}, result)
	})
}

func TestLoad_MissingParamsError(t *testing.T) {
	tmpl := gotmpl.GoTemplatingContent("dynamic/{{ .__params.project }}.json")
	cfg := &Config{
		Enabled: literalValueRef(true),
		Backend: Backend{
			Provider: "mock-state",
			Inputs: map[string]*spec.ValueRef{
				"path": {Tmpl: &tmpl},
			},
		},
	}

	backend := &mockBackendProvider{}
	reg := newTestRegistry(t, backend)
	mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})

	t.Run("returns MissingParamsError when params missing", func(t *testing.T) {
		// No params supplied -- template will fail on .__params.project
		_, err := mgr.Load(context.Background(), map[string]any{}, CommandInfo{Subcommand: "run resolver"})
		assert.Error(t, err)

		var missingErr *MissingParamsError
		assert.ErrorAs(t, err, &missingErr)
		assert.Equal(t, []string{"project"}, missingErr.Missing)
		assert.NotNil(t, missingErr.Original)
	})

	t.Run("unwrap returns original", func(t *testing.T) {
		_, err := mgr.Load(context.Background(), map[string]any{}, CommandInfo{Subcommand: "run resolver"})
		assert.Error(t, err)

		var missingErr *MissingParamsError
		assert.ErrorAs(t, err, &missingErr)
		assert.Contains(t, missingErr.Unwrap().Error(), "resolve backend inputs")
	})

	t.Run("no error when params supplied", func(t *testing.T) {
		params := map[string]any{"project": "alpha"}
		result, err := mgr.Load(context.Background(), params, CommandInfo{Subcommand: "run resolver"})
		assert.NoError(t, err)
		assert.False(t, result.Skipped)
	})
}

// TestExtractParamRefs_AuthorFunction is a regression test: a __params
// reference wrapped in a call to an author-defined function must still be
// tracked, so state up-to-date checks see the param dependency rather than
// dropping it because the template failed to parse.
func TestExtractParamRefs_AuthorFunction(t *testing.T) {
	tmpl := gotmpl.GoTemplatingContent(`{{ greet .__params.project }}`)
	vr := &spec.ValueRef{Tmpl: &tmpl}
	seen := make(map[string]struct{})
	extractParamRefs(context.Background(), vr, seen)
	assert.Contains(t, seen, "project")
}
