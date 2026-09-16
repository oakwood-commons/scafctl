// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"bytes"
	"context"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
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
	t.Run("ok when neither set", func(t *testing.T) {
		o := &sharedResolverOptions{}
		require.NoError(t, o.validateStateFlags())
	})

	t.Run("ok when only state-file set", func(t *testing.T) {
		o := &sharedResolverOptions{StateFile: "intent.json"}
		require.NoError(t, o.validateStateFlags())
	})

	t.Run("ok when only no-state set", func(t *testing.T) {
		o := &sharedResolverOptions{NoState: true}
		require.NoError(t, o.validateStateFlags())
	})

	t.Run("error when both set", func(t *testing.T) {
		o := &sharedResolverOptions{NoState: true, StateFile: "intent.json"}
		err := o.validateStateFlags()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "mutually exclusive")
	})
}

func TestResolveStateConfig(t *testing.T) {
	t.Run("no state-file returns the declared config unchanged", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		declared := &state.Config{Backend: state.Backend{Provider: "github"}}
		sol := &solution.Solution{Metadata: solution.Metadata{Name: "demo"}, State: declared}
		o := &sharedResolverOptions{}

		cfg, err := o.resolveStateConfig(ctx, sol)
		require.NoError(t, err)
		assert.Same(t, declared, cfg, "the declared config must be returned as-is")
		assert.Empty(t, buf.String(), "no override notice when the flag is unset")
	})

	t.Run("no state-file and no state block returns nil", func(t *testing.T) {
		ctx, _ := testWriterCtx(t)
		sol := &solution.Solution{Metadata: solution.Metadata{Name: "demo"}}
		o := &sharedResolverOptions{}

		cfg, err := o.resolveStateConfig(ctx, sol)
		require.NoError(t, err)
		assert.Nil(t, cfg)
	})

	t.Run("state-file with no state block enables full-format file state without a notice", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		sol := &solution.Solution{Metadata: solution.Metadata{Name: "demo"}}
		o := &sharedResolverOptions{StateFile: "intent.json"}

		cfg, err := o.resolveStateConfig(ctx, sol)
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, state.FileBackendProvider, cfg.Backend.Provider)
		assert.Empty(t, cfg.Backend.Format, "with no declared state block, format defaults to full (empty string)")
		assert.Equal(t, "intent.json", cfg.Backend.Inputs[state.BackendInputPath].Literal)
		assert.Empty(t, buf.String(), "no override notice when the solution declares no state block")
	})

	t.Run("state-file overrides a declared block, inherits its format, and reports the override", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		declared := &state.Config{Backend: state.Backend{Provider: "github", Format: state.FormatIntent}}
		sol := &solution.Solution{Metadata: solution.Metadata{Name: "demo"}, State: declared}
		o := &sharedResolverOptions{StateFile: "intent.json"}

		cfg, err := o.resolveStateConfig(ctx, sol)
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, state.FileBackendProvider, cfg.Backend.Provider)
		assert.Equal(t, state.FormatIntent, cfg.Backend.Format, "the inherited format must carry over from the solution's declared backend")
		assert.Empty(t, cfg.Emit, "an explicit state file is a complete substitution, never an additional output")
		out := buf.String()
		assert.Contains(t, out, "overriding")
		assert.Contains(t, out, "github", "the override notice must name the replaced provider")
		assert.Contains(t, out, "intent.json")
		assert.NotContains(t, out, "dropped", "a declared block with no Emit targets has nothing to report as dropped")
	})

	t.Run("state-file overriding a block with Emit targets reports how many were dropped", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		declared := &state.Config{
			Backend: state.Backend{Provider: "file"},
			Emit: []state.EmitTarget{
				{Backend: state.Backend{Provider: "file", Format: state.FormatIntent}},
				{Backend: state.Backend{Provider: "http", Format: state.FormatIntent}},
			},
		}
		sol := &solution.Solution{Metadata: solution.Metadata{Name: "demo"}, State: declared}
		o := &sharedResolverOptions{StateFile: "override.json"}

		cfg, err := o.resolveStateConfig(ctx, sol)
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Empty(t, cfg.Emit)
		out := buf.String()
		assert.Contains(t, out, "2 emit target(s) dropped", "the notice must name how many Emit targets were dropped")
	})
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
	t.Run("prefers Location when set", func(t *testing.T) {
		got := locationOrProvider("state.json", "file")
		assert.Equal(t, "state.json", got)
	})

	t.Run("falls back to a provider-named backend when Location is empty", func(t *testing.T) {
		got := locationOrProvider("", "github")
		assert.Equal(t, "github backend", got)
	})
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

	t.Run("first run reports no prior state", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		reportStateLoaded(ctx, &state.LoadResult{FirstRun: true, Location: "state.json"})
		out := buf.String()
		assert.Contains(t, out, "no prior state")
		assert.Contains(t, out, "state.json")
		assert.Contains(t, out, "first run")
	})

	t.Run("replay reports parameter and locked-value counts", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		reportStateLoaded(ctx, &state.LoadResult{
			FirstRun:        false,
			LoadedParams:    3,
			LoadedResolvers: 1,
			Location:        "state.json",
		})
		out := buf.String()
		assert.Contains(t, out, "reusing")
		assert.Contains(t, out, "3 parameter")
		assert.Contains(t, out, "1 locked value")
		assert.Contains(t, out, "state.json")
	})

	t.Run("falls back to a provider-named backend when Location is empty", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		reportStateLoaded(ctx, &state.LoadResult{FirstRun: true, Provider: "github"})
		assert.Contains(t, buf.String(), "github backend")
	})

	t.Run("suppressed by --quiet", func(t *testing.T) {
		ctx, buf := testQuietWriterCtx(t)
		reportStateLoaded(ctx, &state.LoadResult{FirstRun: true, Location: "state.json"})
		assert.Empty(t, buf.String())
	})

	t.Run("no panic without a writer", func(t *testing.T) {
		assert.NotPanics(t, func() {
			reportStateLoaded(context.Background(), &state.LoadResult{FirstRun: true})
		})
	})
}

func TestReportStateSaved(t *testing.T) {
	t.Run("no-op when result is nil", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		reportStateSaved(ctx, nil)
		assert.Empty(t, buf.String())
	})

	t.Run("reports the primary write", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		reportStateSaved(ctx, &state.SaveResult{
			Primary: state.BackendWrite{Provider: "file", Location: "state.json", Format: state.FormatFull},
		})
		out := buf.String()
		assert.Contains(t, out, "updated")
		assert.Contains(t, out, "state.json")
		assert.Contains(t, out, "full")
	})

	t.Run("reports an enabled emit target", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		reportStateSaved(ctx, &state.SaveResult{
			Primary: state.BackendWrite{Provider: "file", Location: "state.json", Format: state.FormatFull},
			Emits: []state.BackendWrite{
				{Provider: "file", Location: "intent.json", Format: state.FormatIntent},
			},
		})
		out := buf.String()
		assert.Contains(t, out, "emitted")
		assert.Contains(t, out, "intent.json")
		assert.Contains(t, out, "intent")
	})

	t.Run("a skipped emit target is silent", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		reportStateSaved(ctx, &state.SaveResult{
			Primary: state.BackendWrite{Provider: "file", Location: "state.json", Format: state.FormatFull},
			Emits: []state.BackendWrite{
				{Provider: "file", Location: "intent.json", Format: state.FormatIntent, Skipped: true},
			},
		})
		out := buf.String()
		assert.Contains(t, out, "updated", "the primary write is still reported")
		assert.NotContains(t, out, "intent.json", "a skipped emit target must not be reported")
		assert.NotContains(t, out, "emitted")
	})

	t.Run("falls back to a provider-named backend when Location is empty", func(t *testing.T) {
		ctx, buf := testWriterCtx(t)
		reportStateSaved(ctx, &state.SaveResult{
			Primary: state.BackendWrite{Provider: "github", Format: state.FormatFull},
		})
		assert.Contains(t, buf.String(), "github backend")
	})

	t.Run("suppressed by --quiet", func(t *testing.T) {
		ctx, buf := testQuietWriterCtx(t)
		reportStateSaved(ctx, &state.SaveResult{
			Primary: state.BackendWrite{Provider: "file", Location: "state.json", Format: state.FormatFull},
			Emits: []state.BackendWrite{
				{Provider: "file", Location: "intent.json", Format: state.FormatIntent},
			},
		})
		assert.Empty(t, buf.String())
	})

	t.Run("no panic without a writer", func(t *testing.T) {
		assert.NotPanics(t, func() {
			reportStateSaved(context.Background(), &state.SaveResult{
				Primary: state.BackendWrite{Provider: "file", Location: "state.json", Format: state.FormatFull},
			})
		})
	})
}
