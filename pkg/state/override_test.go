// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// declaredFileState is a typical declared state block: a file load plus an
// extends: load save target and an intent save target.
func declaredFileState() *Config {
	return &Config{
		Enabled: &spec.ValueRef{Literal: true},
		Load: &LoadConfig{
			Provider: FileProviderName,
			Inputs:   map[string]*spec.ValueRef{FileInputPath: {Literal: "declared.json"}},
		},
		Save: []SaveTarget{
			{Extends: ExtendsLoad, Checkpoint: true},
			{Provider: FileProviderName, Format: FormatIntent, Inputs: map[string]*spec.ValueRef{FileInputPath: {Literal: "intent.json"}}},
		},
	}
}

func TestOverrides_IsZero(t *testing.T) {
	t.Parallel()

	assert.True(t, Overrides{}.IsZero())
	assert.False(t, Overrides{LoadFile: "a.json"}.IsZero())
	assert.False(t, Overrides{OutputFile: "b.json"}.IsZero())
	assert.False(t, Overrides{NoOutput: true}.IsZero())
}

func TestApplyOverrides_ZeroReturnsDeclared(t *testing.T) {
	t.Parallel()

	declared := declaredFileState()
	got, err := ApplyOverrides(declared, Overrides{})
	require.NoError(t, err)
	assert.Same(t, declared, got, "no override must hand back the declared config untouched")

	got, err = ApplyOverrides(nil, Overrides{})
	require.NoError(t, err)
	assert.Nil(t, got, "no state block and no override means no state")
}

func TestApplyOverrides_OutputAndNoOutputAreExclusive(t *testing.T) {
	t.Parallel()

	_, err := ApplyOverrides(declaredFileState(), Overrides{OutputFile: "out.json", NoOutput: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

func TestApplyOverrides_NoOutputWithoutStateBlockIsNoState(t *testing.T) {
	t.Parallel()

	got, err := ApplyOverrides(nil, Overrides{NoOutput: true})
	require.NoError(t, err)
	assert.Nil(t, got, "disabling output never enables state")
}

func TestApplyOverrides_LoadFileWithoutStateBlockIsReadOnly(t *testing.T) {
	t.Parallel()

	got, err := ApplyOverrides(nil, Overrides{LoadFile: "intent.json"})
	require.NoError(t, err)
	require.NotNil(t, got)

	require.NotNil(t, got.Enabled)
	assert.Equal(t, true, got.Enabled.Literal)
	require.NotNil(t, got.Load)
	assert.Equal(t, FileProviderName, got.Load.Provider)
	assert.Equal(t, "intent.json", got.Load.Inputs[FileInputPath].Literal)
	assert.Empty(t, got.Save, "a load override alone must never add a write")
}

func TestApplyOverrides_OutputFileWithoutStateBlockIsSaveOnly(t *testing.T) {
	t.Parallel()

	got, err := ApplyOverrides(nil, Overrides{OutputFile: "out.json"})
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, true, got.Enabled.Literal)
	assert.Nil(t, got.Load, "an output override alone reads nothing")
	require.Len(t, got.Save, 1)
	assert.Equal(t, FileProviderName, got.Save[0].Provider)
	assert.Equal(t, FormatFull, got.Save[0].Format)
	assert.Equal(t, "out.json", got.Save[0].Inputs[FileInputPath].Literal)
}

// TestApplyOverrides_LoadFileNeverRedirectsExtendsTargets is the guarantee the
// load/save split exists for: reading from an explicit file must not make an
// extends: load target write to that file.
func TestApplyOverrides_LoadFileNeverRedirectsExtendsTargets(t *testing.T) {
	t.Parallel()

	got, err := ApplyOverrides(declaredFileState(), Overrides{LoadFile: "replay.json"})
	require.NoError(t, err)

	assert.Equal(t, "replay.json", got.Load.Inputs[FileInputPath].Literal, "the load block reads the override file")

	require.Len(t, got.Save, 2, "the declared save targets still run")
	extends := got.Save[0]
	assert.Empty(t, extends.Extends, "the extends target is materialized before the load block is replaced")
	assert.Equal(t, FileProviderName, extends.Provider)
	assert.Equal(t, "declared.json", extends.Inputs[FileInputPath].Literal, "an extends target writes the DECLARED load location")
	assert.True(t, extends.Checkpoint, "target settings survive materialization")
	assert.Equal(t, "intent.json", got.Save[1].Inputs[FileInputPath].Literal)
}

func TestApplyOverrides_LoadFileEnablesDisabledState(t *testing.T) {
	t.Parallel()

	declared := declaredFileState()
	declared.Enabled = &spec.ValueRef{Literal: false}

	got, err := ApplyOverrides(declared, Overrides{LoadFile: "replay.json"})
	require.NoError(t, err)
	assert.Equal(t, true, got.Enabled.Literal, "an explicit state file is an explicit request for state")
}

func TestApplyOverrides_OutputFileReplacesEverySaveTarget(t *testing.T) {
	t.Parallel()

	got, err := ApplyOverrides(declaredFileState(), Overrides{OutputFile: "out.json"})
	require.NoError(t, err)

	assert.Equal(t, "declared.json", got.Load.Inputs[FileInputPath].Literal, "the declared load block is kept")
	require.Len(t, got.Save, 1)
	assert.Equal(t, "out.json", got.Save[0].Inputs[FileInputPath].Literal)
	assert.Equal(t, FormatFull, got.Save[0].Format)
	assert.False(t, got.Save[0].Checkpoint, "the output override never checkpoints")
}

func TestApplyOverrides_NoOutputKeepsLoad(t *testing.T) {
	t.Parallel()

	declared := declaredFileState()
	got, err := ApplyOverrides(declared, Overrides{NoOutput: true})
	require.NoError(t, err)

	assert.Empty(t, got.Save)
	require.NotNil(t, got.Load)
	assert.Equal(t, "declared.json", got.Load.Inputs[FileInputPath].Literal)
	assert.Same(t, declared.Enabled, got.Enabled, "disabling output leaves activation as declared")
}

func TestApplyOverrides_NeverMutatesDeclared(t *testing.T) {
	t.Parallel()

	declared := declaredFileState()
	_, err := ApplyOverrides(declared, Overrides{LoadFile: "replay.json", OutputFile: "out.json"})
	require.NoError(t, err)

	assert.Equal(t, "declared.json", declared.Load.Inputs[FileInputPath].Literal)
	require.Len(t, declared.Save, 2)
	assert.Equal(t, ExtendsLoad, declared.Save[0].Extends)
	assert.Empty(t, declared.Save[0].Provider)
	assert.Nil(t, declared.Save[0].Inputs)
}

func TestDescribeOverrides(t *testing.T) {
	t.Parallel()

	saveOnly := &Config{Save: []SaveTarget{{Provider: FileProviderName}}}
	enabledExpr := celexp.Expression("__params.persist")
	withEnabled := func(cfg *Config, enabled *spec.ValueRef) *Config {
		cfg.Enabled = enabled
		return cfg
	}

	tests := []struct {
		name      string
		declared  *Config
		overrides Overrides
		want      OverrideInfo
	}{
		{name: "nil declared replaces nothing", declared: nil, overrides: Overrides{LoadFile: "a.json", OutputFile: "b.json"}},
		{name: "no override replaces nothing", declared: declaredFileState(), overrides: Overrides{}},
		{
			name: "load file replaces a declared load", declared: declaredFileState(), overrides: Overrides{LoadFile: "a.json"},
			want: OverrideInfo{ReplacedLoad: true, ReplacedLoadProvider: FileProviderName},
		},
		{name: "load file on a save-only config replaces no load", declared: saveOnly, overrides: Overrides{LoadFile: "a.json"}},
		{
			name: "output file replaces every save target", declared: declaredFileState(), overrides: Overrides{OutputFile: "b.json"},
			want: OverrideInfo{ReplacedSaveTargets: 2},
		},
		{
			name: "no output replaces every save target", declared: declaredFileState(), overrides: Overrides{NoOutput: true},
			want: OverrideInfo{ReplacedSaveTargets: 2},
		},
		{
			name: "load file over a conditional enabled forces state on", declared: withEnabled(declaredFileState(), &spec.ValueRef{Expr: &enabledExpr}),
			overrides: Overrides{LoadFile: "a.json"},
			want:      OverrideInfo{ReplacedLoad: true, ReplacedLoadProvider: FileProviderName, OverridesEnabled: true},
		},
		{
			name: "output file over a literal false enabled forces state on", declared: withEnabled(declaredFileState(), &spec.ValueRef{Literal: false}),
			overrides: Overrides{OutputFile: "b.json"},
			want:      OverrideInfo{ReplacedSaveTargets: 2, OverridesEnabled: true},
		},
		{
			name: "no output never enables", declared: withEnabled(declaredFileState(), &spec.ValueRef{Literal: false}),
			overrides: Overrides{NoOutput: true},
			want:      OverrideInfo{ReplacedSaveTargets: 2},
		},
		{
			name: "an absent enabled is not overridden", declared: withEnabled(declaredFileState(), nil),
			overrides: Overrides{LoadFile: "a.json"},
			want:      OverrideInfo{ReplacedLoad: true, ReplacedLoadProvider: FileProviderName},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, DescribeOverrides(tt.declared, tt.overrides))
		})
	}
}

func TestMaterializeExtends(t *testing.T) {
	t.Parallel()

	t.Run("nil config", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, materializeExtends(nil))
	})

	t.Run("merges load inputs under the target's own, target wins", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{
			Load: &LoadConfig{
				Provider: "github",
				Inputs: map[string]*spec.ValueRef{
					"path": {Literal: "state/app.json"},
					"ref":  {Literal: "main"},
				},
			},
			Save: []SaveTarget{{
				Extends: ExtendsLoad,
				Inputs: map[string]*spec.ValueRef{
					"ref":    {Literal: "feature"},
					"branch": {Literal: "feature"},
				},
			}},
		}

		got := materializeExtends(cfg)
		require.Len(t, got.Save, 1)
		target := got.Save[0]
		assert.Empty(t, target.Extends)
		assert.Equal(t, "github", target.Provider)
		assert.Equal(t, "state/app.json", target.Inputs["path"].Literal, "inherited from load")
		assert.Equal(t, "feature", target.Inputs["ref"].Literal, "the target's own input wins")
		assert.Equal(t, "feature", target.Inputs["branch"].Literal)

		assert.Equal(t, ExtendsLoad, cfg.Save[0].Extends, "the input config is not modified")
		assert.Len(t, cfg.Save[0].Inputs, 2)
		assert.Len(t, cfg.Load.Inputs, 2)
	})

	t.Run("extends without a load block stays unresolved", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{Save: []SaveTarget{{Extends: ExtendsLoad}}}
		got := materializeExtends(cfg)
		assert.Equal(t, ExtendsLoad, got.Save[0].Extends, "the manager refuses to write an unresolved target")
		assert.Empty(t, got.Save[0].Provider)
	})

	t.Run("non-extends targets are untouched", func(t *testing.T) {
		t.Parallel()
		cfg := declaredFileState()
		got := materializeExtends(cfg)
		assert.Equal(t, cfg.Save[1], got.Save[1])
	})

	t.Run("idempotent", func(t *testing.T) {
		t.Parallel()
		once := materializeExtends(declaredFileState())
		assert.Equal(t, once, materializeExtends(once))
	})
}

func BenchmarkApplyOverrides(b *testing.B) {
	declared := declaredFileState()
	overrides := Overrides{LoadFile: "replay.json", OutputFile: "out.json"}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := ApplyOverrides(declared, overrides); err != nil {
			b.Fatal(err)
		}
	}
}
