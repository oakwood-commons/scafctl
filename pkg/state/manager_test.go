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
	loadData   *Data
	loadErr    error
	notFound   bool // state_load reports found:false (no state exists yet)
	saveErr    error
	loadCalls  int
	saveCalls  []map[string]any
	workingDir string // working directory seen by the most recent call
}

func (m *mockBackendProvider) Descriptor() *provider.Descriptor {
	return &provider.Descriptor{
		Name:        "mock-state",
		DisplayName: "Mock State",
		Description: "Mock state provider for testing",
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

func (m *mockBackendProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
	inputs, _ := input.(map[string]any)
	op, _ := inputs["operation"].(string)
	m.workingDir, _ = provider.WorkingDirectoryFromContext(ctx)

	switch op {
	case "state_load":
		m.loadCalls++
		if m.loadErr != nil {
			return nil, m.loadErr
		}
		if m.notFound {
			return &provider.Output{
				Data: map[string]any{
					"success":      true,
					OutputKeyFound: false,
				},
			}, nil
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
				Load: &LoadConfig{
					Provider: "mock-state",
					Inputs:   map[string]*spec.ValueRef{"path": literalValueRef("test.json")},
				},
			},
			backend: &mockBackendProvider{loadData: existingState},
			check: func(t *testing.T, result *LoadResult) {
				assert.False(t, result.Skipped)
				assert.True(t, result.Loaded, "a configured load block reports Loaded")
				assert.NotNil(t, result.Data)
				assert.Equal(t, "test-app", result.Data.Metadata.Solution)
			},
		},
		{
			name: "enabled false skips",
			config: &Config{
				Enabled: literalValueRef(false),
				Load: &LoadConfig{
					Provider: "mock-state",
					Inputs:   map[string]*spec.ValueRef{},
				},
			},
			wantSkip: true,
		},
		{
			name: "nil enabled means enabled",
			config: &Config{
				Load: &LoadConfig{
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
				Load: &LoadConfig{
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
				Load: &LoadConfig{
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
				Load: &LoadConfig{
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
				Load: &LoadConfig{
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

// TestManagerLoad_WithoutLoadBlock verifies a save-only configuration: every
// run starts from empty state, nothing is read, and there is nothing to report
// as loaded.
func TestManagerLoad_WithoutLoadBlock(t *testing.T) {
	t.Parallel()

	backend := &mockBackendProvider{loadData: NewMockData("app", "1.0.0", map[string]any{"env": "stale"})}
	reg := newTestRegistry(t, backend)
	cfg := &Config{Save: []SaveTarget{{Provider: "mock-state"}}}
	mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})

	result, err := mgr.Load(context.Background(), map[string]any{"env": "prod"}, CommandInfo{Subcommand: "run solution"})
	require.NoError(t, err)

	assert.False(t, result.Skipped)
	assert.False(t, result.Loaded, "nothing was read, so nothing is reported as loaded")
	assert.True(t, result.FirstRun)
	assert.Zero(t, backend.loadCalls, "a save-only configuration must never read state")
	require.NotNil(t, result.Data)
	assert.Empty(t, result.Data.Parameters)
	assert.Equal(t, map[string]any{"env": "prod"}, result.MergedParams)
	assert.Equal(t, "run solution", result.Data.Command.Subcommand)

	sd, ok := FromContext(result.Ctx)
	require.True(t, ok, "empty state is still injected for the state provider")
	assert.Same(t, result.Data, sd)
}

// TestManagerLoad_RequireExisting verifies WithRequireExisting: a load provider
// reporting that no state exists fails with a *NotFoundError instead of being
// treated as a first run.
func TestManagerLoad_RequireExisting(t *testing.T) {
	t.Parallel()

	fileLoad := func(inputs map[string]*spec.ValueRef) *Config {
		return &Config{Load: &LoadConfig{Provider: "mock-state", Inputs: inputs}}
	}
	pathInput := map[string]*spec.ValueRef{"path": literalValueRef("missing.json")}

	t.Run("missing state is an error", func(t *testing.T) {
		t.Parallel()
		reg := newTestRegistry(t, &mockBackendProvider{notFound: true})
		mgr := NewManager(fileLoad(pathInput), reg, settings.RuntimeProvenance{}, WithRequireExisting())

		_, err := mgr.Load(context.Background(), nil, CommandInfo{})
		require.Error(t, err)
		var notFound *NotFoundError
		require.ErrorAs(t, err, &notFound)
		assert.Equal(t, "missing.json", notFound.Location)
		assert.Equal(t, "mock-state", notFound.Provider)
		assert.Contains(t, err.Error(), `state file "missing.json" does not exist`)
	})

	t.Run("message names the provider when there is no location", func(t *testing.T) {
		t.Parallel()
		reg := newTestRegistry(t, &mockBackendProvider{notFound: true})
		mgr := NewManager(fileLoad(nil), reg, settings.RuntimeProvenance{}, WithRequireExisting())

		_, err := mgr.Load(context.Background(), nil, CommandInfo{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no state exists at the mock-state load provider")
	})

	t.Run("existing state loads", func(t *testing.T) {
		t.Parallel()
		reg := newTestRegistry(t, &mockBackendProvider{loadData: NewMockData("app", "1.0.0", map[string]any{"env": "prod"})})
		mgr := NewManager(fileLoad(pathInput), reg, settings.RuntimeProvenance{}, WithRequireExisting())

		result, err := mgr.Load(context.Background(), nil, CommandInfo{})
		require.NoError(t, err)
		assert.True(t, result.Loaded)
		assert.Equal(t, "prod", result.MergedParams["env"])
	})

	t.Run("without the option missing state is a first run", func(t *testing.T) {
		t.Parallel()
		reg := newTestRegistry(t, &mockBackendProvider{notFound: true})
		mgr := NewManager(fileLoad(pathInput), reg, settings.RuntimeProvenance{})

		result, err := mgr.Load(context.Background(), nil, CommandInfo{})
		require.NoError(t, err)
		assert.True(t, result.Loaded)
		assert.True(t, result.FirstRun)
	})
}

// TestManager_WithWorkingDirectory verifies that state provider calls -- the
// load and every save -- run with the configured working directory, so a
// relative state path resolves against the caller's directory even when the
// process directory has changed (a bundled solution's extraction directory).
func TestManager_WithWorkingDirectory(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Load: &LoadConfig{Provider: "mock-state", Inputs: map[string]*spec.ValueRef{"path": literalValueRef("state.json")}},
		Save: []SaveTarget{{Extends: ExtendsLoad}},
	}
	solMeta := SolutionMeta{Name: "app", Version: "1.0.0"}

	t.Run("applied to load and save", func(t *testing.T) {
		t.Parallel()
		backend := &mockBackendProvider{}
		mgr := NewManager(cfg, newTestRegistry(t, backend), settings.RuntimeProvenance{}, WithWorkingDirectory("/invoking/dir"))

		result, err := mgr.Load(context.Background(), nil, CommandInfo{})
		require.NoError(t, err)
		assert.Equal(t, "/invoking/dir", backend.workingDir, "load")

		_, isSet := provider.WorkingDirectoryFromContext(result.Ctx)
		assert.False(t, isSet, "resolver execution keeps its own working directory")

		backend.workingDir = ""
		_, err = mgr.Save(context.Background(), result.Data, resolver.NewContext(), nil, nil, nil, solMeta, nil)
		require.NoError(t, err)
		assert.Equal(t, "/invoking/dir", backend.workingDir, "save")
	})

	t.Run("empty keeps the context default", func(t *testing.T) {
		t.Parallel()
		backend := &mockBackendProvider{}
		mgr := NewManager(cfg, newTestRegistry(t, backend), settings.RuntimeProvenance{}, WithWorkingDirectory(""))

		ctx := provider.WithWorkingDirectory(context.Background(), "/from/context")
		_, err := mgr.Load(ctx, nil, CommandInfo{})
		require.NoError(t, err)
		assert.Equal(t, "/from/context", backend.workingDir)
	})
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
			Load: &LoadConfig{
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
			Load: &LoadConfig{
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
			Load: &LoadConfig{
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
			Load: &LoadConfig{
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
			Load: &LoadConfig{
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
			Load: &LoadConfig{
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
				Load: &LoadConfig{
					Provider: "mock-state",
					Inputs:   map[string]*spec.ValueRef{},
				},
				Save: []SaveTarget{{Extends: ExtendsLoad}},
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
				Load: &LoadConfig{
					Provider: "mock-state",
					Inputs:   map[string]*spec.ValueRef{},
				},
				Save: []SaveTarget{{Extends: ExtendsLoad}},
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
				Load: &LoadConfig{
					Provider: "mock-state",
					Inputs:   map[string]*spec.ValueRef{},
				},
				Save: []SaveTarget{{Extends: ExtendsLoad}},
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
				Load: &LoadConfig{
					Provider: "mock-state",
					Inputs:   map[string]*spec.ValueRef{},
				},
				Save: []SaveTarget{{Extends: ExtendsLoad}},
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
				Load: &LoadConfig{
					Provider: "mock-state",
					Inputs:   map[string]*spec.ValueRef{},
				},
				Save: []SaveTarget{{Extends: ExtendsLoad}},
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

			_, err := mgr.Save(context.Background(), tt.state, rctx, resolvers, tt.params, nil, solMeta, nil)
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

// TestManagerSave_Targets covers where Save writes: nowhere without a save
// target, through a target's own provider, and through the load block's
// provider via extends: load.
func TestManagerSave_Targets(t *testing.T) {
	t.Parallel()

	solMeta := SolutionMeta{Name: "my-app", Version: "1.0.0"}
	loadInputs := map[string]*spec.ValueRef{"path": literalValueRef("state.json")}

	t.Run("a load-only configuration writes nothing", func(t *testing.T) {
		t.Parallel()
		backend := &mockBackendProvider{}
		mgr := NewManager(&Config{Load: &LoadConfig{Provider: "mock-state", Inputs: loadInputs}}, newTestRegistry(t, backend), settings.RuntimeProvenance{})
		sd := NewData()

		result, err := mgr.Save(context.Background(), sd, resolver.NewContext(), nil, map[string]any{"env": "prod"}, nil, solMeta, nil)
		require.NoError(t, err)
		assert.Nil(t, result)
		assert.Empty(t, backend.saveCalls)
		assert.Empty(t, sd.Parameters, "nothing is recorded when nothing is written")
	})

	t.Run("a target with its own provider is written", func(t *testing.T) {
		t.Parallel()
		backend := &mockBackendProvider{}
		cfg := &Config{Save: []SaveTarget{{Provider: "mock-state", Inputs: map[string]*spec.ValueRef{"path": literalValueRef("out.json")}}}}
		mgr := NewManager(cfg, newTestRegistry(t, backend), settings.RuntimeProvenance{})

		result, err := mgr.Save(context.Background(), NewData(), resolver.NewContext(), nil, map[string]any{"env": "prod"}, nil, solMeta, nil)
		require.NoError(t, err)
		require.Len(t, backend.saveCalls, 1)
		assert.Equal(t, "out.json", backend.saveCalls[0]["path"])
		require.Len(t, result.Targets, 1)
		assert.Equal(t, TargetWrite{Provider: "mock-state", Location: "out.json", Format: FormatFull}, result.Targets[0])
	})

	t.Run("an extends target writes the load location with its own inputs on top", func(t *testing.T) {
		t.Parallel()
		backend := &mockBackendProvider{}
		cfg := &Config{
			Load: &LoadConfig{Provider: "mock-state", Inputs: map[string]*spec.ValueRef{
				"path":   literalValueRef("state.json"),
				"branch": literalValueRef("main"),
			}},
			Save: []SaveTarget{{Extends: ExtendsLoad, Inputs: map[string]*spec.ValueRef{"branch": literalValueRef("feature")}}},
		}
		mgr := NewManager(cfg, newTestRegistry(t, backend), settings.RuntimeProvenance{})

		result, err := mgr.Save(context.Background(), NewData(), resolver.NewContext(), nil, nil, nil, solMeta, nil)
		require.NoError(t, err)
		require.Len(t, backend.saveCalls, 1)
		assert.Equal(t, "state.json", backend.saveCalls[0]["path"], "inherited from the load block")
		assert.Equal(t, "feature", backend.saveCalls[0]["branch"], "the target's own input wins")
		require.Len(t, result.Targets, 1)
		assert.Equal(t, "mock-state", result.Targets[0].Provider)
	})

	t.Run("an extends target without a load block is refused", func(t *testing.T) {
		t.Parallel()
		backend := &mockBackendProvider{}
		mgr := NewManager(&Config{Save: []SaveTarget{{Extends: ExtendsLoad}}}, newTestRegistry(t, backend), settings.RuntimeProvenance{})

		_, err := mgr.Save(context.Background(), NewData(), resolver.NewContext(), nil, nil, nil, solMeta, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "save[0]")
		assert.Contains(t, err.Error(), "requires a state.load block")
		assert.Empty(t, backend.saveCalls)
	})

	t.Run("an unsupported extends value is refused", func(t *testing.T) {
		t.Parallel()
		backend := &mockBackendProvider{}
		cfg := &Config{
			Load: &LoadConfig{Provider: "mock-state", Inputs: loadInputs},
			Save: []SaveTarget{{Extends: "other"}},
		}
		mgr := NewManager(cfg, newTestRegistry(t, backend), settings.RuntimeProvenance{})

		_, err := mgr.Save(context.Background(), NewData(), resolver.NewContext(), nil, nil, nil, solMeta, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `unsupported extends value "other"`)
		assert.Empty(t, backend.saveCalls)
	})
}

// TestManagerCheckpoint covers the pre-action checkpoint write: it writes only
// checkpoint: true targets, locks immutable values, and leaves the saved
// parameter set as loaded -- new -r values are saved only by the final Save.
func TestManagerCheckpoint(t *testing.T) {
	t.Parallel()

	solMeta := SolutionMeta{Name: "my-app", Version: "1.0.0"}
	immutableRun := func() (*resolver.Context, []*resolver.Resolver) {
		rctx := resolver.NewContext()
		rctx.SetResult("cluster_id", &resolver.ExecutionResult{Value: "uuid-1234", Status: resolver.ExecutionStatusSuccess})
		return rctx, []*resolver.Resolver{{Name: "cluster_id", Type: "string", Immutable: true}}
	}
	// newMgr configures an extends: load target on "mock-full" (checkpoint as
	// given) and an intent target on "mock-intent", which never checkpoints.
	newMgr := func(t *testing.T, checkpoint bool) (*Manager, *mockBackendProvider, *mockBackendProvider) {
		t.Helper()
		full, intent := &mockBackendProvider{}, &mockBackendProvider{}
		reg := provider.NewRegistry()
		require.NoError(t, reg.Register(namedMockBackend("mock-full", full)))
		require.NoError(t, reg.Register(namedMockBackend("mock-intent", intent)))
		cfg := &Config{
			Load: &LoadConfig{Provider: "mock-full", Inputs: map[string]*spec.ValueRef{"path": literalValueRef("state.json")}},
			Save: []SaveTarget{
				{Extends: ExtendsLoad, Checkpoint: checkpoint},
				{Provider: "mock-intent", Format: FormatIntent, Inputs: map[string]*spec.ValueRef{"path": literalValueRef("intent.json")}},
			},
		}
		return NewManager(cfg, reg, settings.RuntimeProvenance{}), full, intent
	}

	t.Run("writes only checkpoint targets and locks immutables", func(t *testing.T) {
		t.Parallel()
		mgr, full, intent := newMgr(t, true)
		rctx, resolvers := immutableRun()
		sd := NewData()
		sd.Parameters["env"] = "dev" // as loaded

		err := mgr.Checkpoint(context.Background(), sd, rctx, resolvers, map[string]any{"env": "prod"}, nil, solMeta, nil)
		require.NoError(t, err)

		require.Len(t, full.saveCalls, 1)
		assert.Empty(t, intent.saveCalls, "a target without checkpoint is written only by the final save")
		assert.Equal(t, "uuid-1234", sd.Resolvers["cluster_id"].Value)
		assert.Equal(t, map[string]any{"env": "dev"}, sd.Parameters, "the checkpoint keeps the parameters as loaded")

		saved, ok := full.saveCalls[0]["data"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, map[string]any{"env": "dev"}, saved["parameters"])
		assert.Contains(t, saved["resolvers"], "cluster_id")
	})

	t.Run("skips resolvers whose deferred validation failed", func(t *testing.T) {
		t.Parallel()
		mgr, full, _ := newMgr(t, true)
		rctx, resolvers := immutableRun()
		sd := NewData()

		err := mgr.Checkpoint(context.Background(), sd, rctx, resolvers, nil, nil, solMeta, map[string]bool{"cluster_id": true})
		require.NoError(t, err)
		assert.NotContains(t, sd.Resolvers, "cluster_id")
		assert.Len(t, full.saveCalls, 1)
	})

	t.Run("is a no-op without checkpoint targets", func(t *testing.T) {
		t.Parallel()
		mgr, full, intent := newMgr(t, false)
		rctx, resolvers := immutableRun()
		sd := NewData()

		require.NoError(t, mgr.Checkpoint(context.Background(), sd, rctx, resolvers, nil, nil, solMeta, nil))
		assert.Empty(t, full.saveCalls)
		assert.Empty(t, intent.saveCalls)
		assert.Empty(t, sd.Resolvers, "nothing is locked when nothing is written")
		assert.True(t, sd.Metadata.CreatedAt.IsZero(), "metadata is untouched")
	})

	t.Run("checkpoint then save writes the checkpoint target twice and the rest once", func(t *testing.T) {
		t.Parallel()
		mgr, full, intent := newMgr(t, true)
		rctx, resolvers := immutableRun()
		sd := NewData()
		params := map[string]any{"env": "prod"}

		// Mirrors run solution/run action: Checkpoint before actions, Save
		// once they all succeed.
		require.NoError(t, mgr.Checkpoint(context.Background(), sd, rctx, resolvers, params, nil, solMeta, nil))
		result, err := mgr.Save(context.Background(), sd, rctx, resolvers, params, nil, solMeta, nil)
		require.NoError(t, err)

		assert.Len(t, full.saveCalls, 2, "written before actions and again at the end")
		assert.Len(t, intent.saveCalls, 1, "a side-effecting non-checkpoint target is written exactly once")
		require.Len(t, result.Targets, 2, "only the final save is reported")
		assert.Equal(t, params, sd.Parameters)
	})

	t.Run("nil config is a no-op", func(t *testing.T) {
		t.Parallel()
		mgr := NewManager(nil, provider.NewRegistry(), settings.RuntimeProvenance{})
		sd := NewData()

		require.NoError(t, mgr.Checkpoint(context.Background(), sd, resolver.NewContext(), nil, nil, nil, solMeta, nil))
		result, err := mgr.Save(context.Background(), sd, resolver.NewContext(), nil, nil, nil, solMeta, nil)
		require.NoError(t, err)
		assert.Nil(t, result)
	})
}

func TestManagerSave_Immutable(t *testing.T) {
	baseConfig := &Config{
		Enabled: literalValueRef(true),
		Load: &LoadConfig{
			Provider: "mock-state",
			Inputs:   map[string]*spec.ValueRef{},
		},
		Save: []SaveTarget{{Extends: ExtendsLoad}},
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

		_, err := mgr.Save(context.Background(), sd, rctx, resolvers, nil, nil, solMeta, nil)
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

		_, err := mgr.Save(context.Background(), sd, rctx, resolvers, nil, nil, solMeta, nil)
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

		_, err := mgr.Save(context.Background(), sd, rctx, resolvers, nil, nil, solMeta, nil)
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

		_, err := mgr.Save(context.Background(), sd, rctx, resolvers, nil, nil, solMeta, nil)
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
		_, _, err := extractStateData(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "nil execution result")
	})

	t.Run("non-map output", func(t *testing.T) {
		t.Parallel()
		_, _, err := extractStateData(&provider.ExecutionResult{
			Output: provider.Output{Data: "not a map"},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "expected map output")
	})

	t.Run("missing data field", func(t *testing.T) {
		t.Parallel()
		_, _, err := extractStateData(&provider.ExecutionResult{
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
		result, found, err := extractStateData(&provider.ExecutionResult{
			Output: provider.Output{Data: map[string]any{
				"success": true,
				"data":    expected,
			}},
		})
		assert.NoError(t, err)
		assert.True(t, found)
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
		result, found, err := extractStateData(&provider.ExecutionResult{
			Output: provider.Output{Data: map[string]any{
				"success": true,
				"data":    dataMap,
			}},
		})
		assert.NoError(t, err)
		assert.True(t, found)
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
		_, _, err := extractStateData(&provider.ExecutionResult{
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
		_, _, err := extractStateData(&provider.ExecutionResult{
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

		result, _, err := extractStateData(&provider.ExecutionResult{
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
		_, _, err := extractStateData(&provider.ExecutionResult{
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
		result, found, err := extractStateData(&provider.ExecutionResult{
			Output: provider.Output{Data: map[string]any{
				"success":      true,
				OutputKeyFound: false,
			}},
		})
		require.NoError(t, err)
		assert.False(t, found, "found:false must be reported so a required load can fail")
		assert.Equal(t, SchemaVersionCurrent, result.SchemaVersion)
		assert.Empty(t, result.Parameters)
	})

	t.Run("found false wins over zero-version data", func(t *testing.T) {
		t.Parallel()
		// Even when a backend returns a zero-value document alongside found:false,
		// the absence signal takes precedence and no version guard is applied.
		result, found, err := extractStateData(&provider.ExecutionResult{
			Output: provider.Output{Data: map[string]any{
				"success":      true,
				OutputKeyFound: false,
				OutputKeyData:  &Data{},
			}},
		})
		require.NoError(t, err)
		assert.False(t, found)
		assert.Equal(t, SchemaVersionCurrent, result.SchemaVersion)
	})

	t.Run("direct pointer empty document is fresh state", func(t *testing.T) {
		t.Parallel()
		// A zero-value *Data (no found signal) is the in-process equivalent of a
		// contentless payload and must be treated as fresh state, not a v0 file.
		result, _, err := extractStateData(&provider.ExecutionResult{
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
		result, _, err := extractStateData(&provider.ExecutionResult{
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
			Load: &LoadConfig{
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
			Load: &LoadConfig{
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
			Load: &LoadConfig{
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
			Load: &LoadConfig{
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

func TestManagerSave_TargetInputs(t *testing.T) {
	t.Parallel()

	t.Run("an extends target without inputs writes the load inputs", func(t *testing.T) {
		t.Parallel()
		backend := &mockBackendProvider{}
		reg := newTestRegistry(t, backend)
		cfg := &Config{
			Enabled: literalValueRef(true),
			Load: &LoadConfig{
				Provider: "mock-state",
				Inputs:   map[string]*spec.ValueRef{"path": literalValueRef("state.json")},
			},
			Save: []SaveTarget{{Extends: ExtendsLoad}},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		sd := NewData()
		rctx := resolver.NewContext()
		_, err := mgr.Save(context.Background(), sd, rctx, nil, nil, nil, SolutionMeta{Name: "app", Version: "1.0.0"}, nil)
		assert.NoError(t, err)
		assert.Len(t, backend.saveCalls, 1)
		assert.Equal(t, "state.json", backend.saveCalls[0]["path"])
	})

	t.Run("target inputs with literal values", func(t *testing.T) {
		t.Parallel()
		backend := &mockBackendProvider{}
		reg := newTestRegistry(t, backend)
		cfg := &Config{
			Enabled: literalValueRef(true),
			Load: &LoadConfig{
				Provider: "mock-state",
				Inputs:   map[string]*spec.ValueRef{"path": literalValueRef("state.json")},
			},
			Save: []SaveTarget{{Extends: ExtendsLoad, Inputs: map[string]*spec.ValueRef{
				"branch": literalValueRef("feature-branch"),
			}}},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		sd := NewData()
		rctx := resolver.NewContext()
		_, err := mgr.Save(context.Background(), sd, rctx, nil, nil, nil, SolutionMeta{Name: "app", Version: "1.0.0"}, nil)
		assert.NoError(t, err)
		assert.Len(t, backend.saveCalls, 1)
		assert.Equal(t, "feature-branch", backend.saveCalls[0]["branch"])
		assert.Equal(t, "state.json", backend.saveCalls[0]["path"])
	})

	t.Run("target inputs with rslvr reference", func(t *testing.T) {
		t.Parallel()
		backend := &mockBackendProvider{}
		reg := newTestRegistry(t, backend)
		branchResolver := "featureBranch"
		cfg := &Config{
			Enabled: literalValueRef(true),
			Load: &LoadConfig{
				Provider: "mock-state",
				Inputs:   map[string]*spec.ValueRef{"path": literalValueRef("state.json")},
			},
			Save: []SaveTarget{{Extends: ExtendsLoad, Inputs: map[string]*spec.ValueRef{
				"branch": {Resolver: &branchResolver},
			}}},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		sd := NewData()
		rctx := resolver.NewContext()
		resolverData := map[string]any{"featureBranch": "feat/my-feature"}
		_, err := mgr.Save(context.Background(), sd, rctx, nil, nil, resolverData, SolutionMeta{Name: "app", Version: "1.0.0"}, nil)
		assert.NoError(t, err)
		assert.Len(t, backend.saveCalls, 1)
		assert.Equal(t, "feat/my-feature", backend.saveCalls[0]["branch"])
	})

	t.Run("target inputs with CEL expression using resolver data", func(t *testing.T) {
		t.Parallel()
		backend := &mockBackendProvider{}
		reg := newTestRegistry(t, backend)
		expr := celexp.Expression("'feat/' + _.appName")
		cfg := &Config{
			Enabled: literalValueRef(true),
			Load: &LoadConfig{
				Provider: "mock-state",
				Inputs:   map[string]*spec.ValueRef{"path": literalValueRef("state.json")},
			},
			Save: []SaveTarget{{Extends: ExtendsLoad, Inputs: map[string]*spec.ValueRef{
				"branch": {Expr: &expr},
			}}},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		sd := NewData()
		rctx := resolver.NewContext()
		resolverData := map[string]any{"appName": "my-app"}
		_, err := mgr.Save(context.Background(), sd, rctx, nil, nil, resolverData, SolutionMeta{Name: "app", Version: "1.0.0"}, nil)
		assert.NoError(t, err)
		assert.Len(t, backend.saveCalls, 1)
		assert.Equal(t, "feat/my-app", backend.saveCalls[0]["branch"])
	})

	t.Run("a target input overrides the inherited load input", func(t *testing.T) {
		t.Parallel()
		backend := &mockBackendProvider{}
		reg := newTestRegistry(t, backend)
		cfg := &Config{
			Enabled: literalValueRef(true),
			Load: &LoadConfig{
				Provider: "mock-state",
				Inputs: map[string]*spec.ValueRef{
					"path":   literalValueRef("state.json"),
					"branch": literalValueRef("main"),
				},
			},
			Save: []SaveTarget{{Extends: ExtendsLoad, Inputs: map[string]*spec.ValueRef{
				"branch": literalValueRef("feature-branch"),
			}}},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		sd := NewData()
		rctx := resolver.NewContext()
		_, err := mgr.Save(context.Background(), sd, rctx, nil, nil, nil, SolutionMeta{Name: "app", Version: "1.0.0"}, nil)
		assert.NoError(t, err)
		assert.Len(t, backend.saveCalls, 1)
		// the target's own value wins
		assert.Equal(t, "feature-branch", backend.saveCalls[0]["branch"])
		// non-overridden input should remain
		assert.Equal(t, "state.json", backend.saveCalls[0]["path"])
	})

	t.Run("target inputs are not resolved at load time", func(t *testing.T) {
		t.Parallel()
		backend := &mockBackendProvider{}
		reg := newTestRegistry(t, backend)
		// a target input with a rslvr: ref -- would fail if resolved at load time
		branchResolver := "featureBranch"
		cfg := &Config{
			Enabled: literalValueRef(true),
			Load: &LoadConfig{
				Provider: "mock-state",
				Inputs:   map[string]*spec.ValueRef{"path": literalValueRef("state.json")},
			},
			Save: []SaveTarget{{Extends: ExtendsLoad, Inputs: map[string]*spec.ValueRef{
				"branch": {Resolver: &branchResolver},
			}}},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		// Load should succeed -- save target inputs are resolved at save time
		result, err := mgr.Load(context.Background(), nil, CommandInfo{Subcommand: "run solution"})
		assert.NoError(t, err)
		assert.False(t, result.Skipped)
	})

	t.Run("target input resolution error", func(t *testing.T) {
		t.Parallel()
		backend := &mockBackendProvider{}
		reg := newTestRegistry(t, backend)
		expr := celexp.Expression("_.nonexistent.nested.value")
		cfg := &Config{
			Enabled: literalValueRef(true),
			Load: &LoadConfig{
				Provider: "mock-state",
				Inputs:   map[string]*spec.ValueRef{"path": literalValueRef("state.json")},
			},
			Save: []SaveTarget{{Extends: ExtendsLoad, Inputs: map[string]*spec.ValueRef{
				"branch": {Expr: &expr},
			}}},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		sd := NewData()
		rctx := resolver.NewContext()
		_, err := mgr.Save(context.Background(), sd, rctx, nil, nil, nil, SolutionMeta{Name: "app", Version: "1.0.0"}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "save[0]: resolve inputs")
	})

	t.Run("a nil target input never erases an inherited input", func(t *testing.T) {
		t.Parallel()
		backend := &mockBackendProvider{}
		reg := newTestRegistry(t, backend)
		cfg := &Config{
			Enabled: literalValueRef(true),
			Load: &LoadConfig{
				Provider: "mock-state",
				Inputs: map[string]*spec.ValueRef{
					"path":   literalValueRef("state.json"),
					"branch": literalValueRef("main"),
				},
			},
			Save: []SaveTarget{{Extends: ExtendsLoad, Inputs: map[string]*spec.ValueRef{
				"branch": nil,
				"extra":  nil,
			}}},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		sd := NewData()
		rctx := resolver.NewContext()
		_, err := mgr.Save(context.Background(), sd, rctx, nil, nil, nil, SolutionMeta{Name: "app", Version: "1.0.0"}, nil)
		assert.NoError(t, err)
		require.Len(t, backend.saveCalls, 1)
		// A dangling (nil) target input is skipped: the inherited value stays,
		// and a key with no inherited value is simply absent.
		assert.Equal(t, "main", backend.saveCalls[0]["branch"])
		_, hasExtra := backend.saveCalls[0]["extra"]
		assert.False(t, hasExtra)
	})
}

// TestManagerSave_Format verifies that a save target's Format controls what
// shape its state_save receives.
func TestManagerSave_Format(t *testing.T) {
	t.Parallel()

	// saveAs writes one document carrying an immutable lock through a single
	// save target of the given format.
	saveAs := func(t *testing.T, format string, params map[string]any) (*mockBackendProvider, error) {
		t.Helper()
		backend := &mockBackendProvider{}
		cfg := &Config{Save: []SaveTarget{{Provider: "mock-state", Format: format, Inputs: map[string]*spec.ValueRef{"path": literalValueRef("state.json")}}}}
		mgr := NewManager(cfg, newTestRegistry(t, backend), settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		sd := NewData()
		sd.Resolvers["cluster_id"] = &PersistedEntry{Value: "abc", Type: "string", Immutable: true}
		_, err := mgr.Save(context.Background(), sd, resolver.NewContext(), nil, params, nil, SolutionMeta{Name: "app", Version: "1.0.0"}, nil)
		return backend, err
	}

	t.Run("full format saves the complete document", func(t *testing.T) {
		t.Parallel()
		backend, err := saveAs(t, FormatFull, nil)
		require.NoError(t, err)
		require.Len(t, backend.saveCalls, 1)

		data, ok := backend.saveCalls[0]["data"].(map[string]any)
		require.True(t, ok)
		assert.Contains(t, data, "resolvers", "full format must retain resolver locks")
	})

	t.Run("intent format drops resolver locks", func(t *testing.T) {
		t.Parallel()
		backend, err := saveAs(t, FormatIntent, map[string]any{"env": "prod"})
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
		backend, err := saveAs(t, "bogus", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown save target format")
		assert.Empty(t, backend.saveCalls, "the provider must not be called when projection fails")
	})
}

// registerNamedMocks returns a registry hosting one named mock state provider
// per name, and the mocks keyed by name.
func registerNamedMocks(t *testing.T, names ...string) (*provider.Registry, map[string]*mockBackendProvider) {
	t.Helper()
	reg := provider.NewRegistry()
	mocks := make(map[string]*mockBackendProvider, len(names))
	for _, name := range names {
		mocks[name] = &mockBackendProvider{}
		require.NoError(t, reg.Register(namedMockBackend(name, mocks[name])))
	}
	return reg, mocks
}

func pathInputs(path string) map[string]*spec.ValueRef {
	return map[string]*spec.ValueRef{"path": literalValueRef(path)}
}

// TestManagerSave_MultipleTargets verifies that every save target is written,
// each projected per its own Format and gated by its own Enabled condition.
func TestManagerSave_MultipleTargets(t *testing.T) {
	t.Parallel()

	solMeta := SolutionMeta{Name: "app", Version: "1.0.0"}

	t.Run("a full extends target and an intent target are both saved", func(t *testing.T) {
		t.Parallel()
		reg, mocks := registerNamedMocks(t, "mock-primary", "mock-intent")
		cfg := &Config{
			Load: &LoadConfig{Provider: "mock-primary", Inputs: pathInputs(".scafctl/state.json")},
			Save: []SaveTarget{
				{Extends: ExtendsLoad},
				{Provider: "mock-intent", Format: FormatIntent, Inputs: pathInputs("intent/sandbox.json")},
			},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{})
		sd := NewData()
		sd.Resolvers["cluster_id"] = &PersistedEntry{Value: "abc", Type: "string", Immutable: true}
		_, err := mgr.Save(context.Background(), sd, resolver.NewContext(), nil, map[string]any{"env": "prod"}, nil, solMeta, nil)
		require.NoError(t, err)

		require.Len(t, mocks["mock-primary"].saveCalls, 1)
		fullData, _ := mocks["mock-primary"].saveCalls[0]["data"].(map[string]any)
		assert.Contains(t, fullData, "resolvers", "the full target must retain resolver locks")

		require.Len(t, mocks["mock-intent"].saveCalls, 1)
		intentData, _ := mocks["mock-intent"].saveCalls[0]["data"].(map[string]any)
		assert.NotContains(t, intentData, "resolvers", "the intent target must omit resolver locks")
		intentParams, _ := intentData["parameters"].(map[string]any)
		assert.Equal(t, "prod", intentParams["env"])
	})

	t.Run("every target is saved", func(t *testing.T) {
		t.Parallel()
		reg, mocks := registerNamedMocks(t, "mock-a", "mock-b", "mock-c")
		cfg := &Config{Save: []SaveTarget{
			{Provider: "mock-a", Inputs: pathInputs("a.json")},
			{Provider: "mock-b", Format: FormatIntent, Inputs: pathInputs("b.json")},
			{Provider: "mock-c", Format: FormatIntent, Inputs: pathInputs("c.json")},
		}}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{})
		_, err := mgr.Save(context.Background(), NewData(), resolver.NewContext(), nil, nil, nil, solMeta, nil)
		require.NoError(t, err)

		for name, mock := range mocks {
			assert.Len(t, mock.saveCalls, 1, name)
		}
	})

	t.Run("enabled false skips only that target", func(t *testing.T) {
		t.Parallel()
		reg, mocks := registerNamedMocks(t, "mock-primary", "mock-intent")
		cfg := &Config{Save: []SaveTarget{
			{Provider: "mock-primary", Inputs: pathInputs("state.json")},
			{Provider: "mock-intent", Format: FormatIntent, Inputs: pathInputs("intent.json"), Enabled: literalValueRef(false)},
		}}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{})
		_, err := mgr.Save(context.Background(), NewData(), resolver.NewContext(), nil, nil, nil, solMeta, nil)
		require.NoError(t, err)

		assert.Len(t, mocks["mock-primary"].saveCalls, 1, "the other target is still saved")
		assert.Empty(t, mocks["mock-intent"].saveCalls, "a disabled target must not be saved")
	})

	t.Run("enabled absent defaults to true", func(t *testing.T) {
		t.Parallel()
		reg, mocks := registerNamedMocks(t, "mock-intent")
		cfg := &Config{Save: []SaveTarget{{Provider: "mock-intent", Format: FormatIntent, Inputs: pathInputs("intent.json")}}}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{})
		_, err := mgr.Save(context.Background(), NewData(), resolver.NewContext(), nil, nil, nil, solMeta, nil)
		require.NoError(t, err)

		assert.Len(t, mocks["mock-intent"].saveCalls, 1, "a target with no Enabled field must always save")
	})

	t.Run("enabled can reference any resolver, unlike the state Enabled", func(t *testing.T) {
		t.Parallel()
		reg, mocks := registerNamedMocks(t, "mock-intent")
		expr := celexp.Expression("_.environment == 'sandbox'")
		cfg := &Config{Save: []SaveTarget{{
			Provider: "mock-intent",
			Format:   FormatIntent,
			Inputs:   pathInputs("intent.json"),
			Enabled:  &spec.ValueRef{Expr: &expr},
		}}}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{})

		// resolverData ("_") carries the "environment" resolver's output --
		// exactly the kind of reference Config.Enabled cannot make at load time
		// (it runs before resolvers), but a save target's Enabled can, because
		// it is evaluated at save time after every resolver has run.
		resolverData := map[string]any{"environment": "sandbox"}
		_, err := mgr.Save(context.Background(), NewData(), resolver.NewContext(), nil, nil, resolverData, solMeta, nil)
		require.NoError(t, err)
		assert.Len(t, mocks["mock-intent"].saveCalls, 1)
	})

	t.Run("a failing target aborts later targets but does not undo earlier ones", func(t *testing.T) {
		t.Parallel()
		reg, mocks := registerNamedMocks(t, "mock-first", "mock-fail", "mock-never")
		mocks["mock-fail"].saveErr = assert.AnError
		cfg := &Config{Save: []SaveTarget{
			{Provider: "mock-first", Inputs: pathInputs("state.json")},
			{Provider: "mock-fail", Inputs: pathInputs("a.json")},
			{Provider: "mock-never", Inputs: pathInputs("b.json")},
		}}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{})
		result, err := mgr.Save(context.Background(), NewData(), resolver.NewContext(), nil, nil, nil, solMeta, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "save[1]")

		assert.Len(t, mocks["mock-first"].saveCalls, 1, "an earlier write already succeeded and is not undone")
		assert.Empty(t, mocks["mock-never"].saveCalls, "a target after a failing one must not run")
		require.NotNil(t, result, "the targets written before the failure are still reported")
		assert.Equal(t, []TargetWrite{{Provider: "mock-first", Location: "state.json", Format: FormatFull}}, result.Targets)
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

// TestManagerSave_SaveResult verifies that Save reports every save target in
// declaration order -- including targets skipped by their Enabled condition --
// so command-layer callers can confirm a save without re-deriving it from the
// state Config.
func TestManagerSave_SaveResult(t *testing.T) {
	t.Parallel()

	solMeta := SolutionMeta{Name: "app", Version: "1.0.0"}

	t.Run("reports a target's provider, location, and format", func(t *testing.T) {
		t.Parallel()
		reg, _ := registerNamedMocks(t, "mock-primary")
		cfg := &Config{
			Load: &LoadConfig{Provider: "mock-primary", Inputs: pathInputs("state.json")},
			Save: []SaveTarget{{Extends: ExtendsLoad}},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{})
		result, err := mgr.Save(context.Background(), NewData(), resolver.NewContext(), nil, nil, nil, solMeta, nil)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, result.Targets, 1)
		assert.Equal(t, TargetWrite{Provider: "mock-primary", Location: "state.json", Format: FormatFull}, result.Targets[0],
			"an extends target reports the inherited provider and location; an unset Format normalizes to full")
	})

	t.Run("reports every target in order, including one skipped by its Enabled condition", func(t *testing.T) {
		t.Parallel()
		reg, mocks := registerNamedMocks(t, "mock-primary", "mock-on", "mock-off")
		cfg := &Config{
			Load: &LoadConfig{Provider: "mock-primary", Inputs: pathInputs("state.json")},
			Save: []SaveTarget{
				{Extends: ExtendsLoad},
				{Provider: "mock-on", Format: FormatIntent, Inputs: pathInputs("intent.json")},
				{Provider: "mock-off", Format: FormatIntent, Inputs: pathInputs("skipped.json"), Enabled: literalValueRef(false)},
			},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{})
		result, err := mgr.Save(context.Background(), NewData(), resolver.NewContext(), nil, nil, nil, solMeta, nil)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, result.Targets, 3)

		assert.Equal(t, TargetWrite{Provider: "mock-primary", Location: "state.json", Format: FormatFull}, result.Targets[0])
		assert.Equal(t, TargetWrite{Provider: "mock-on", Location: "intent.json", Format: FormatIntent}, result.Targets[1])
		assert.Equal(t, TargetWrite{Provider: "mock-off", Format: FormatIntent, Skipped: true}, result.Targets[2],
			"a skipped target reports its declared Format; its location is never resolved")
		assert.Empty(t, mocks["mock-off"].saveCalls, "a skipped target must not actually be saved")
	})

	t.Run("a target's Parameters narrowing applies to its own payload only", func(t *testing.T) {
		t.Parallel()
		reg, mocks := registerNamedMocks(t, "mock-primary", "mock-intent")
		cfg := &Config{
			Load: &LoadConfig{Provider: "mock-primary", Inputs: pathInputs("state.json")},
			Save: []SaveTarget{
				{Extends: ExtendsLoad},
				{
					Provider:   "mock-intent",
					Format:     FormatIntent,
					Parameters: &ParameterProjection{Include: []string{"appName"}},
					Inputs:     pathInputs("intent.json"),
				},
			},
		}
		mgr := NewManager(cfg, reg, settings.RuntimeProvenance{})

		mergedParams := map[string]any{"appName": "hello", "mode": "publish"}
		result, err := mgr.Save(context.Background(), NewData(), resolver.NewContext(), nil, mergedParams, nil, solMeta, nil)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, result.Targets, 2)

		require.Len(t, mocks["mock-intent"].saveCalls, 1)
		saved, ok := mocks["mock-intent"].saveCalls[0]["data"].(map[string]any)
		require.True(t, ok)
		params, ok := saved["parameters"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, map[string]any{"appName": "hello"}, params, "the intent target's payload must reflect its own narrowing")

		// The full target is unaffected -- it has no narrowing spec.
		require.Len(t, mocks["mock-primary"].saveCalls, 1)
		fullData, ok := mocks["mock-primary"].saveCalls[0]["data"].(map[string]any)
		require.True(t, ok)
		fullParams, ok := fullData["parameters"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, mergedParams, fullParams, "the full target must save the whole parameter set")
	})

	t.Run("nil config yields a nil SaveResult and nil error", func(t *testing.T) {
		t.Parallel()
		mgr := NewManager(nil, provider.NewRegistry(), settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
		result, err := mgr.Save(context.Background(), NewData(), resolver.NewContext(), nil, nil, nil, solMeta, nil)
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
			Load: &LoadConfig{
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
			Load: &LoadConfig{
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
			Load: &LoadConfig{
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
			Load: &LoadConfig{
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
			Load: &LoadConfig{
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

	t.Run("save target inputs excluded", func(t *testing.T) {
		// Save targets resolve at save time, after resolvers run, so their
		// __params references are not required before state loads.
		expr := celexp.Expression("__params.save_branch")
		cfg := &Config{
			Load: &LoadConfig{
				Provider: "file",
				Inputs: map[string]*spec.ValueRef{
					"path": literalValueRef("fixed.json"),
				},
			},
			Save: []SaveTarget{{Extends: ExtendsLoad, Inputs: map[string]*spec.ValueRef{
				"branch": {Expr: &expr},
			}}},
		}
		result := RequiredParams(ctx, cfg)
		assert.Nil(t, result)
	})
}

func TestMissingParams(t *testing.T) {
	ctx := context.Background()

	tmpl := gotmpl.GoTemplatingContent("dynamic/{{ .__params.project }}.json")
	cfg := &Config{
		Load: &LoadConfig{
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
		Load: &LoadConfig{
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
		assert.Contains(t, missingErr.Unwrap().Error(), "resolve load inputs")
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

// TestManagerSave_TargetErrors covers failures specific to save targets.
func TestManagerSave_TargetErrors(t *testing.T) {
	t.Parallel()

	solMeta := SolutionMeta{Name: "app", Version: "1.0.0"}

	t.Run("an Enabled condition that fails to evaluate aborts the save", func(t *testing.T) {
		t.Parallel()
		backend := &mockBackendProvider{}
		expr := celexp.Expression("_.missing.field")
		cfg := &Config{Save: []SaveTarget{{Provider: "mock-state", Enabled: &spec.ValueRef{Expr: &expr}}}}
		mgr := NewManager(cfg, newTestRegistry(t, backend), settings.RuntimeProvenance{})

		_, err := mgr.Save(context.Background(), NewData(), resolver.NewContext(), nil, nil, nil, solMeta, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "evaluate save[0] enabled")
		assert.Empty(t, backend.saveCalls)
	})

	t.Run("a target with neither provider nor extends is refused", func(t *testing.T) {
		t.Parallel()
		backend := &mockBackendProvider{}
		cfg := &Config{Save: []SaveTarget{{Inputs: pathInputs("state.json")}}}
		mgr := NewManager(cfg, newTestRegistry(t, backend), settings.RuntimeProvenance{})

		_, err := mgr.Save(context.Background(), NewData(), resolver.NewContext(), nil, nil, nil, solMeta, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "save[0]: state: provider name is empty")
		assert.Empty(t, backend.saveCalls)
	})

	t.Run("Checkpoint surfaces an immutable mismatch without writing", func(t *testing.T) {
		t.Parallel()
		backend := &mockBackendProvider{}
		cfg := &Config{
			Load: &LoadConfig{Provider: "mock-state", Inputs: pathInputs("state.json")},
			Save: []SaveTarget{{Extends: ExtendsLoad, Checkpoint: true}},
		}
		mgr := NewManager(cfg, newTestRegistry(t, backend), settings.RuntimeProvenance{})

		sd := NewData()
		sd.Resolvers["cluster_id"] = &PersistedEntry{Value: "old", Type: "string", Immutable: true}
		rctx := resolver.NewContext()
		rctx.SetResult("cluster_id", &resolver.ExecutionResult{Value: "new", Status: resolver.ExecutionStatusSuccess})
		resolvers := []*resolver.Resolver{{Name: "cluster_id", Type: "string", Immutable: true}}

		err := mgr.Checkpoint(context.Background(), sd, rctx, resolvers, nil, nil, solMeta, nil)
		require.ErrorIs(t, err, ErrImmutableEntry)
		assert.Empty(t, backend.saveCalls)
	})
}

func TestManager_VerifyImmutables(t *testing.T) {
	t.Parallel()

	sd := NewData()
	sd.Resolvers["cluster_id"] = &PersistedEntry{Value: "old", Type: "string", Immutable: true}
	rctx := resolver.NewContext()
	rctx.SetResult("cluster_id", &resolver.ExecutionResult{Value: "new", Status: resolver.ExecutionStatusSuccess})
	resolvers := []*resolver.Resolver{{Name: "cluster_id", Type: "string", Immutable: true}}

	configured := NewManager(&Config{Load: &LoadConfig{Provider: "mock-state"}}, provider.NewRegistry(), settings.RuntimeProvenance{})
	assert.ErrorIs(t, configured.VerifyImmutables(sd, rctx, resolvers), ErrImmutableEntry,
		"locks are verified whether or not anything will be saved")
	assert.NoError(t, configured.VerifyImmutables(nil, rctx, resolvers), "no state data is a no-op")

	disabled := NewManager(nil, provider.NewRegistry(), settings.RuntimeProvenance{})
	assert.NoError(t, disabled.VerifyImmutables(sd, rctx, resolvers), "no state config is a no-op")
}
