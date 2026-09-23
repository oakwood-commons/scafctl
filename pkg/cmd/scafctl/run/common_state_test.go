// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"bytes"
	"context"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/oakwood-commons/scafctl/pkg/state"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testWriterCtx(t *testing.T) (context.Context, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())
	return writer.WithWriter(context.Background(), w), &buf
}

// testQuietWriterCtx is testWriterCtx with --quiet set, for asserting that
// state's run-time indicators are fully suppressed under --quiet.
func testQuietWriterCtx(t *testing.T) (context.Context, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	params := settings.NewCliParams()
	params.IsQuiet = true
	w := writer.New(ioStreams, params)
	return writer.WithWriter(context.Background(), w), &buf
}

func TestValidateStateFlags(t *testing.T) {
	tests := []struct {
		name    string
		opts    sharedResolverOptions
		wantErr []string
	}{
		{name: "no flags"},
		{name: "state-file only", opts: sharedResolverOptions{StateFile: "in.json"}},
		{name: "state-file with state-output", opts: sharedResolverOptions{StateFile: "in.json", StateOutput: "out.json"}},
		{name: "state-file with no-state-output", opts: sharedResolverOptions{StateFile: "in.json", NoStateOutput: true}},
		{name: "allow-missing-locks with state-file", opts: sharedResolverOptions{StateFile: "in.json", AllowMissingLocks: true}},
		{name: "no-state only", opts: sharedResolverOptions{NoState: true}},
		{
			name:    "no-state with state-file",
			opts:    sharedResolverOptions{NoState: true, StateFile: "in.json"},
			wantErr: []string{"--no-state disables state entirely and cannot be combined with --state-file"},
		},
		{
			name: "no-state lists every conflicting flag",
			opts: sharedResolverOptions{NoState: true, StateOutput: "out.json", NoStateOutput: true, AllowMissingLocks: true},
			wantErr: []string{
				"cannot be combined with --state-output, --no-state-output, --allow-missing-locks",
			},
		},
		{
			name:    "state-output with no-state-output",
			opts:    sharedResolverOptions{StateOutput: "out.json", NoStateOutput: true},
			wantErr: []string{"--state-output and --no-state-output are mutually exclusive"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.validateStateFlags()
			if len(tt.wantErr) == 0 {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			for _, want := range tt.wantErr {
				assert.Contains(t, err.Error(), want)
			}
		})
	}
}

// declaredState is a solution state block with a github load, an extends:
// load target, and an intent target.
func declaredState() *state.Config {
	return &state.Config{
		Load: &state.LoadConfig{
			Provider: "github",
			Inputs:   map[string]*spec.ValueRef{"path": {Literal: "state/app.json"}},
		},
		Save: []state.SaveTarget{
			{Extends: state.ExtendsLoad},
			{Provider: state.FileProviderName, Format: state.FormatIntent, Inputs: map[string]*spec.ValueRef{state.FileInputPath: {Literal: "intent.json"}}},
		},
	}
}

func TestResolveStateConfig(t *testing.T) {
	demo := func(cfg *state.Config) *solution.Solution {
		return &solution.Solution{Metadata: solution.Metadata{Name: "demo"}, State: cfg}
	}

	t.Run("no flags returns the declared config unchanged", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		declared := declaredState()
		o := &sharedResolverOptions{}

		cfg, err := o.resolveStateConfig(ctx, demo(declared))
		require.NoError(t, err)
		assert.Same(t, declared, cfg)
		assert.Empty(t, buf.String(), "no notice when no flag is set")
	})

	t.Run("no flags and no state block returns nil", func(t *testing.T) {
		ctx, _ := testWriterCtx(t)
		o := &sharedResolverOptions{}

		cfg, err := o.resolveStateConfig(ctx, demo(nil))
		require.NoError(t, err)
		assert.Nil(t, cfg)
	})

	t.Run("state-file without a state block is a read-only replay", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		o := &sharedResolverOptions{StateFile: "intent.json"}

		cfg, err := o.resolveStateConfig(ctx, demo(nil))
		require.NoError(t, err)
		require.NotNil(t, cfg)
		require.NotNil(t, cfg.Load)
		assert.Equal(t, state.FileProviderName, cfg.Load.Provider)
		assert.Equal(t, "intent.json", cfg.Load.Inputs[state.FileInputPath].Literal)
		assert.Empty(t, cfg.Save, "reading a file never adds a write")
		assert.Empty(t, buf.String(), "nothing declared was replaced")
	})

	t.Run("state-file replaces only the declared load and says so", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		o := &sharedResolverOptions{StateFile: "intent.json"}

		cfg, err := o.resolveStateConfig(ctx, demo(declaredState()))
		require.NoError(t, err)
		assert.Equal(t, "intent.json", cfg.Load.Inputs[state.FileInputPath].Literal)
		require.Len(t, cfg.Save, 2, "the declared save targets still run")
		assert.Equal(t, "github", cfg.Save[0].Provider, "the extends target keeps the declared load's provider")
		assert.Equal(t, "state/app.json", cfg.Save[0].Inputs["path"].Literal, "and its location")
		assert.Contains(t, buf.String(), `--state-file: reading state from intent.json instead of the solution's "github" load`)
		assert.NotContains(t, buf.String(), "overriding the solution's state.enabled", "an absent enabled is not overridden")
	})

	t.Run("state-file over a conditional enabled forces state on and says so", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		o := &sharedResolverOptions{StateFile: "intent.json"}
		declared := declaredState()
		expr := celexp.Expression("__params.persist == 'true'")
		declared.Enabled = &spec.ValueRef{Expr: &expr}

		cfg, err := o.resolveStateConfig(ctx, demo(declared))
		require.NoError(t, err)
		assert.Equal(t, true, cfg.Enabled.Literal)
		assert.Contains(t, buf.String(), "--state-file: overriding the solution's state.enabled; state is enabled for this run")
	})

	t.Run("state-output alone names itself in the enabled notice", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		o := &sharedResolverOptions{StateOutput: "out.json"}
		declared := declaredState()
		declared.Enabled = &spec.ValueRef{Literal: false}

		_, err := o.resolveStateConfig(ctx, demo(declared))
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "--state-output: overriding the solution's state.enabled")
	})

	t.Run("state-file on a save-only block reports no replaced load", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		o := &sharedResolverOptions{StateFile: "intent.json"}
		saveOnly := &state.Config{Save: []state.SaveTarget{{Provider: state.FileProviderName}}}

		_, err := o.resolveStateConfig(ctx, demo(saveOnly))
		require.NoError(t, err)
		assert.Empty(t, buf.String())
	})

	t.Run("state-output replaces every declared save target and says so", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		o := &sharedResolverOptions{StateOutput: "out.json"}

		cfg, err := o.resolveStateConfig(ctx, demo(declaredState()))
		require.NoError(t, err)
		require.Len(t, cfg.Save, 1)
		assert.Equal(t, "out.json", cfg.Save[0].Inputs[state.FileInputPath].Literal)
		assert.Equal(t, state.FormatFull, cfg.Save[0].Format)
		assert.Contains(t, buf.String(), "--state-output: writing full state to out.json instead of the solution's 2 save target(s)")
	})

	t.Run("no-state-output skips every declared save target and says so", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		o := &sharedResolverOptions{NoStateOutput: true}

		cfg, err := o.resolveStateConfig(ctx, demo(declaredState()))
		require.NoError(t, err)
		assert.Empty(t, cfg.Save)
		require.NotNil(t, cfg.Load, "loading still happens")
		assert.Contains(t, buf.String(), "--no-state-output: skipping the solution's 2 save target(s)")
	})

	t.Run("no-state-output on a load-only block is silent", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		o := &sharedResolverOptions{NoStateOutput: true}
		loadOnly := &state.Config{Load: &state.LoadConfig{Provider: state.FileProviderName}}

		_, err := o.resolveStateConfig(ctx, demo(loadOnly))
		require.NoError(t, err)
		assert.Empty(t, buf.String())
	})

	t.Run("conflicting output overrides are an error", func(t *testing.T) {
		ctx, _ := testWriterCtx(t)
		o := &sharedResolverOptions{StateOutput: "out.json", NoStateOutput: true}

		_, err := o.resolveStateConfig(ctx, demo(declaredState()))
		require.Error(t, err)
	})

	t.Run("no panic without a writer", func(t *testing.T) {
		o := &sharedResolverOptions{StateFile: "intent.json"}
		cfg, err := o.resolveStateConfig(context.Background(), demo(declaredState()))
		require.NoError(t, err)
		assert.NotNil(t, cfg)
	})
}

func TestStateManagerOptions(t *testing.T) {
	assert.Len(t, (&sharedResolverOptions{}).stateManagerOptions("/invoking"), 1,
		"relative state locations always resolve against the invoking directory")
	assert.Len(t, (&sharedResolverOptions{StateFile: "intent.json"}).stateManagerOptions("/invoking"), 2,
		"an explicitly named --state-file must also exist")
}

func TestCheckMissingLocks(t *testing.T) {
	resolvers := []*resolver.Resolver{{Name: "project_id", Immutable: true}}
	lockless := func() *state.LoadResult {
		d := state.NewData()
		d.Parameters["env"] = "prod"
		return &state.LoadResult{Data: d, Loaded: true, LoadedParams: 1, Location: "intent.json", Provider: state.FileProviderName}
	}

	t.Run("refuses a lock-less replay", func(t *testing.T) {
		ctx, _ := testWriterCtx(t)
		err := (&sharedResolverOptions{}).checkMissingLocks(ctx, lockless(), resolvers, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "has parameters but no immutable locks")
		assert.Contains(t, err.Error(), "re-run with --allow-missing-locks")
		assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
	})

	t.Run("allow-missing-locks waives the guard with a warning", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		err := (&sharedResolverOptions{AllowMissingLocks: true}).checkMissingLocks(ctx, lockless(), resolvers, false)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "--allow-missing-locks:")
		assert.Contains(t, buf.String(), "project_id")
	})

	t.Run("a dry run only warns", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		err := (&sharedResolverOptions{}).checkMissingLocks(ctx, lockless(), resolvers, true)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "a real run requires --allow-missing-locks")
	})

	t.Run("a genuine full state file passes silently", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		lr := lockless()
		lr.Data.Resolvers["project_id"] = &state.PersistedEntry{Value: "p-1", Immutable: true}
		require.NoError(t, (&sharedResolverOptions{}).checkMissingLocks(ctx, lr, resolvers, false))
		assert.Empty(t, buf.String())
	})
}

func TestVerifyImmutablesOption(t *testing.T) {
	sd := state.NewData()
	sd.Resolvers["cluster_id"] = &state.PersistedEntry{Value: "old", Type: "string", Immutable: true}
	rctx := resolver.NewContext()
	rctx.SetResult("cluster_id", &resolver.ExecutionResult{Value: "new", Status: resolver.ExecutionStatusSuccess})
	resolvers := []*resolver.Resolver{{Name: "cluster_id", Type: "string", Immutable: true}}
	mgr := state.NewManager(&state.Config{Load: &state.LoadConfig{Provider: state.FileProviderName}}, provider.NewRegistry(), settings.RuntimeProvenance{})
	o := &sharedResolverOptions{}

	t.Run("a changed immutable value fails the run", func(t *testing.T) {
		ctx, _ := testWriterCtx(t)
		err := o.verifyImmutables(ctx, mgr, sd, rctx, resolvers)
		require.Error(t, err)
		assert.ErrorIs(t, err, state.ErrImmutableEntry)
		assert.Equal(t, exitcode.GeneralError, exitcode.GetCode(err))
	})

	t.Run("no manager or no state is a no-op", func(t *testing.T) {
		ctx, _ := testWriterCtx(t)
		assert.NoError(t, o.verifyImmutables(ctx, nil, sd, rctx, resolvers))
		assert.NoError(t, o.verifyImmutables(ctx, mgr, nil, rctx, resolvers))
	})

	t.Run("an unchanged immutable value passes", func(t *testing.T) {
		ctx, _ := testWriterCtx(t)
		same := resolver.NewContext()
		same.SetResult("cluster_id", &resolver.ExecutionResult{Value: "old", Status: resolver.ExecutionStatusSuccess})
		assert.NoError(t, o.verifyImmutables(ctx, mgr, sd, same, resolvers))
	})
}

func TestHandleStateLoadError_NotFound(t *testing.T) {
	ctx, _ := testWriterCtx(t)
	err := (&sharedResolverOptions{}).handleStateLoadError(ctx, &state.NotFoundError{Location: "missing.json", Provider: state.FileProviderName})
	require.Error(t, err)
	assert.Equal(t, `--state-file: state file "missing.json" does not exist`, err.Error())
	assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
}

func TestWarnSolutionMismatch(t *testing.T) {
	solWith := func(name, version string) *solution.Solution {
		s := &solution.Solution{Metadata: solution.Metadata{Name: name}}
		if version != "" {
			s.Metadata.Version = semver.MustParse(version)
		}
		return s
	}
	stateWith := func(name, version string) *state.Data {
		sd := state.NewData()
		sd.Metadata.Solution = name
		sd.Metadata.Version = version
		return sd
	}

	t.Run("warns on solution name mismatch", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		warnSolutionMismatch(ctx, stateWith("other", "1.0.0"), solWith("demo", "1.0.0"))
		assert.Contains(t, buf.String(), "other")
		assert.Contains(t, buf.String(), "demo")
	})

	t.Run("warns on version mismatch only", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		warnSolutionMismatch(ctx, stateWith("demo", "0.9.0"), solWith("demo", "1.0.0"))
		out := buf.String()
		assert.Contains(t, out, "0.9.0")
		assert.Contains(t, out, "1.0.0")
	})

	t.Run("no warning when both match", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		warnSolutionMismatch(ctx, stateWith("demo", "1.0.0"), solWith("demo", "1.0.0"))
		assert.Empty(t, buf.String())
	})

	t.Run("empty recorded values are not a mismatch", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		warnSolutionMismatch(ctx, stateWith("", ""), solWith("demo", "1.0.0"))
		assert.Empty(t, buf.String(), "an intent that omits metadata must not warn")
	})

	t.Run("no panic when solution version is nil", func(t *testing.T) {
		ctx, _ := testWriterCtx(t)
		assert.NotPanics(t, func() {
			warnSolutionMismatch(ctx, stateWith("demo", "1.0.0"), solWith("demo", ""))
		})
	})

	t.Run("no-op on nil inputs", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		warnSolutionMismatch(ctx, nil, solWith("demo", "1.0.0"))
		warnSolutionMismatch(ctx, stateWith("demo", "1.0.0"), nil)
		assert.Empty(t, buf.String())
	})

	t.Run("no panic without a writer", func(t *testing.T) {
		assert.NotPanics(t, func() {
			warnSolutionMismatch(context.Background(), stateWith("other", "1.0.0"), solWith("demo", "1.0.0"))
		})
	})
}

func TestLocationOrProvider(t *testing.T) {
	assert.Equal(t, "state.json", locationOrProvider("state.json", "file"), "prefers Location when set")
	assert.Equal(t, "github provider", locationOrProvider("", "github"), "falls back to the provider name")
}

func TestReportStateLoaded(t *testing.T) {
	t.Run("no-op when result is nil", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		reportStateLoaded(ctx, nil)
		assert.Empty(t, buf.String())
	})

	t.Run("no-op when load was skipped", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		reportStateLoaded(ctx, &state.LoadResult{Skipped: true})
		assert.Empty(t, buf.String())
	})

	t.Run("no-op when nothing was loaded (save-only)", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		reportStateLoaded(ctx, &state.LoadResult{FirstRun: true})
		assert.Empty(t, buf.String(), "a save-only configuration reads nothing, so there is nothing to report")
	})

	t.Run("first run reports no prior state", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		reportStateLoaded(ctx, &state.LoadResult{Loaded: true, FirstRun: true, Location: "state.json"})
		assert.Contains(t, buf.String(), "state: no prior state at state.json (first run)")
	})

	t.Run("replay reports parameter and locked-value counts", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		reportStateLoaded(ctx, &state.LoadResult{
			Loaded:          true,
			LoadedParams:    3,
			LoadedResolvers: 1,
			Location:        "state.json",
		})
		assert.Contains(t, buf.String(), "state: reusing 3 parameter(s) and 1 locked value(s) from state.json")
	})

	t.Run("falls back to the provider name when Location is empty", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		reportStateLoaded(ctx, &state.LoadResult{Loaded: true, FirstRun: true, Provider: "github"})
		assert.Contains(t, buf.String(), "github provider")
	})

	t.Run("suppressed by --quiet", func(t *testing.T) {
		ctx, buf := testQuietWriterCtx(t)
		reportStateLoaded(ctx, &state.LoadResult{Loaded: true, FirstRun: true, Location: "state.json"})
		assert.Empty(t, buf.String())
	})

	t.Run("no panic without a writer", func(t *testing.T) {
		assert.NotPanics(t, func() {
			reportStateLoaded(context.Background(), &state.LoadResult{Loaded: true, FirstRun: true})
		})
	})
}

func TestReportStateSaved(t *testing.T) {
	full := state.TargetWrite{Provider: state.FileProviderName, Location: "state.json", Format: state.FormatFull}
	intent := state.TargetWrite{Provider: state.FileProviderName, Location: "intent.json", Format: state.FormatIntent}

	t.Run("no-op when result is nil", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		reportStateSaved(ctx, nil)
		assert.Empty(t, buf.String())
	})

	t.Run("reports each written target with its format", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		reportStateSaved(ctx, &state.SaveResult{Targets: []state.TargetWrite{full, intent}})
		out := buf.String()
		assert.Contains(t, out, "state: saved state.json (full)")
		assert.Contains(t, out, "state: saved intent.json (intent)")
	})

	t.Run("a skipped target is silent", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		skipped := intent
		skipped.Skipped = true
		reportStateSaved(ctx, &state.SaveResult{Targets: []state.TargetWrite{full, skipped}})
		out := buf.String()
		assert.Contains(t, out, "state.json", "the written target is still reported")
		assert.NotContains(t, out, "intent.json", "a skipped target must not be reported")
	})

	t.Run("falls back to the provider name when Location is empty", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		reportStateSaved(ctx, &state.SaveResult{Targets: []state.TargetWrite{{Provider: "github", Format: state.FormatFull}}})
		assert.Contains(t, buf.String(), "state: saved github provider (full)")
	})

	t.Run("suppressed by --quiet", func(t *testing.T) {
		ctx, buf := testQuietWriterCtx(t)
		reportStateSaved(ctx, &state.SaveResult{Targets: []state.TargetWrite{full, intent}})
		assert.Empty(t, buf.String())
	})

	t.Run("no panic without a writer", func(t *testing.T) {
		assert.NotPanics(t, func() {
			reportStateSaved(context.Background(), &state.SaveResult{Targets: []state.TargetWrite{full}})
		})
	})
}
