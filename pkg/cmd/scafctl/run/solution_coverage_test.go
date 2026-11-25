// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/dryrun"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/output"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSolutionOptions_getActionIOStreams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		output  string
		wantNil bool
	}{
		{name: "json returns nil", output: "json", wantNil: true},
		{name: "yaml returns nil", output: "yaml", wantNil: true},
		{name: "quiet returns nil", output: "quiet", wantNil: true},
		{name: "test returns nil", output: "test", wantNil: true},
		{name: "table returns streams", output: "table", wantNil: false},
		{name: "auto returns streams", output: "auto", wantNil: false},
		{name: "empty returns streams", output: "", wantNil: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			opts := &SolutionOptions{}
			opts.Output = tc.output
			opts.IOStreams = &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}

			result := opts.getActionIOStreams()
			if tc.wantNil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.NotNil(t, result.Out)
				assert.NotNil(t, result.ErrOut)
			}
		})
	}
}

func TestNewActionProgressCallback(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	streams := &terminal.IOStreams{Out: &buf, ErrOut: &buf}
	cliParams := settings.NewCliParams()
	w := writer.New(streams, cliParams)

	cb := NewActionProgressCallback(w)
	require.NotNil(t, cb)
	assert.Equal(t, w, cb.w)
}

func TestActionProgressCallback_Methods(t *testing.T) {
	t.Parallel()

	newCB := func() (*bytes.Buffer, *ActionProgressCallback) {
		var buf bytes.Buffer
		streams := &terminal.IOStreams{Out: &buf, ErrOut: &buf}
		cliParams := settings.NewCliParams()
		cliParams.Verbose = true // needed for OnForEachProgress which still uses Verbosef
		w := writer.New(streams, cliParams)
		return &buf, NewActionProgressCallback(w)
	}

	t.Run("OnActionStart_with_description", func(t *testing.T) {
		t.Parallel()
		buf, cb := newCB()
		cb.OnActionStart("deploy", "Deploy service")
		assert.Contains(t, buf.String(), "deploy")
		assert.Contains(t, buf.String(), "Deploy service")
	})

	t.Run("OnActionStart_no_description", func(t *testing.T) {
		t.Parallel()
		buf, cb := newCB()
		cb.OnActionStart("deploy", "")
		assert.Contains(t, buf.String(), "==> deploy")
	})

	t.Run("OnActionComplete", func(t *testing.T) {
		t.Parallel()
		buf, cb := newCB()
		cb.OnActionComplete("deploy", nil)
		out := buf.String()
		assert.Contains(t, out, "done")
		assert.Contains(t, out, "deploy")
	})

	t.Run("OnActionComplete_with_timing", func(t *testing.T) {
		t.Parallel()
		buf, cb := newCB()
		cb.OnActionStart("deploy", "")
		cb.OnActionComplete("deploy", nil)
		out := buf.String()
		assert.Contains(t, out, "==> deploy")
		assert.Contains(t, out, "done")
		assert.Contains(t, out, "deploy")
	})

	t.Run("OnActionFailed", func(t *testing.T) {
		t.Parallel()
		buf, cb := newCB()
		cb.OnActionFailed("deploy", assert.AnError)
		out := buf.String()
		assert.Contains(t, out, "failed")
		assert.Contains(t, out, "deploy")
	})

	t.Run("OnActionFailed_with_timing", func(t *testing.T) {
		t.Parallel()
		buf, cb := newCB()
		cb.OnActionStart("deploy", "")
		cb.OnActionFailed("deploy", assert.AnError)
		out := buf.String()
		assert.Contains(t, out, "==> deploy")
		assert.Contains(t, out, "failed")
	})

	t.Run("OnActionSkipped", func(t *testing.T) {
		t.Parallel()
		buf, cb := newCB()
		cb.OnActionSkipped("deploy", "not ready")
		assert.Contains(t, buf.String(), "deploy")
		assert.Contains(t, buf.String(), "not ready")
	})

	t.Run("OnActionTimeout", func(t *testing.T) {
		t.Parallel()
		buf, cb := newCB()
		cb.OnActionTimeout("deploy", 30*time.Second)
		assert.Contains(t, buf.String(), "deploy")
		assert.Contains(t, buf.String(), "timeout")
	})

	t.Run("OnActionCancelled", func(t *testing.T) {
		t.Parallel()
		buf, cb := newCB()
		cb.OnActionCancelled("deploy")
		assert.Contains(t, buf.String(), "deploy")
		assert.Contains(t, buf.String(), "cancelled")
	})

	t.Run("OnRetryAttempt", func(t *testing.T) {
		t.Parallel()
		buf, cb := newCB()
		cb.OnRetryAttempt("deploy", 1, 3, assert.AnError)
		assert.Contains(t, buf.String(), "retry")
		assert.Contains(t, buf.String(), "1/3")
	})

	t.Run("OnForEachProgress", func(t *testing.T) {
		t.Parallel()
		buf, cb := newCB()
		cb.OnForEachProgress("deploy", 5, 10)
		assert.Contains(t, buf.String(), "5/10")
	})

	t.Run("OnPhaseStart_noop", func(t *testing.T) {
		t.Parallel()
		buf, cb := newCB()
		cb.OnPhaseStart(1, []string{"deploy", "test"})
		assert.Empty(t, buf.String())
	})

	t.Run("OnPhaseComplete_noop", func(t *testing.T) {
		t.Parallel()
		buf, cb := newCB()
		cb.OnPhaseComplete(1)
		assert.Empty(t, buf.String())
	})

	t.Run("OnFinallyStart_noop", func(t *testing.T) {
		t.Parallel()
		buf, cb := newCB()
		cb.OnFinallyStart()
		assert.Empty(t, buf.String())
	})

	t.Run("OnFinallyComplete_noop", func(t *testing.T) {
		t.Parallel()
		buf, cb := newCB()
		cb.OnFinallyComplete()
		assert.Empty(t, buf.String())
	})
}

func TestNewActionProgressCallback_NilWriter(t *testing.T) {
	t.Parallel()
	// When writer.FromContext returns nil (direct Run calls without a writer in context),
	// the call site must not install the callback. This test verifies that
	// NewActionProgressCallback(nil) is not called by checking the guard in the call sites
	// is correct by constructing an analogous scenario: a context without a writer yields
	// a nil writer, and the nil check prevents a non-nil interface from being set.
	ctx := context.Background() // no writer in context
	w := writer.FromContext(ctx)
	assert.Nil(t, w, "FromContext should return nil when no writer is set")

	// The guard in Run: only construct callback when w != nil
	var cb action.ProgressCallback
	if w != nil {
		cb = NewActionProgressCallback(w)
	}
	assert.Nil(t, cb, "callback must remain nil when writer is nil to avoid panic")
}

func TestSolutionOptions_writeActionOutput_Quiet(t *testing.T) {
	t.Parallel()

	opts := &SolutionOptions{}
	opts.Output = "quiet"

	err := opts.writeActionOutput(context.Background(), &action.ExecutionResult{}, nil)
	assert.NoError(t, err)
}

func TestSolutionOptions_writeActionOutput_UnsupportedFormat(t *testing.T) {
	t.Parallel()

	opts := &SolutionOptions{}
	opts.Output = "invalid-format"

	err := opts.writeActionOutput(context.Background(), &action.ExecutionResult{}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported output format")
}

func TestSolutionOptions_writeActionOutputDefault_NilWriter(t *testing.T) {
	t.Parallel()

	opts := &SolutionOptions{}
	// No writer in context
	err := opts.writeActionOutputDefault(context.Background(), &action.ExecutionResult{
		Actions: map[string]*action.ActionResult{},
	})
	assert.NoError(t, err)
}

func TestSolutionOptions_writeActionOutputDefault_StreamedSuccess(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	streams := &terminal.IOStreams{Out: &buf, ErrOut: &buf}
	cliParams := settings.NewCliParams()
	w := writer.New(streams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	opts := &SolutionOptions{}
	result := &action.ExecutionResult{
		Actions: map[string]*action.ActionResult{
			"deploy": {Streamed: true, Status: action.StatusSucceeded},
		},
	}

	err := opts.writeActionOutputDefault(ctx, result)
	assert.NoError(t, err)
	// Streamed success should produce no extra output
}

func TestSolutionOptions_writeActionOutputDefault_StreamedFailed(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	streams := &terminal.IOStreams{Out: &buf, ErrOut: &buf}
	cliParams := settings.NewCliParams()
	w := writer.New(streams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	opts := &SolutionOptions{}
	result := &action.ExecutionResult{
		Actions: map[string]*action.ActionResult{
			"deploy": {Streamed: true, Status: action.StatusFailed, Error: "timeout occurred"},
		},
	}

	err := opts.writeActionOutputDefault(ctx, result)
	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "timeout occurred")
}

func TestSolutionOptions_writeActionOutputDefault_NonStreamedSuccess(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	streams := &terminal.IOStreams{Out: &buf, ErrOut: &buf}
	cliParams := settings.NewCliParams()
	w := writer.New(streams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	opts := &SolutionOptions{}
	result := &action.ExecutionResult{
		Actions: map[string]*action.ActionResult{
			"deploy": {
				Streamed: false,
				Status:   action.StatusSucceeded,
				Results:  map[string]any{"stdout": "hello world\n"},
			},
		},
	}

	err := opts.writeActionOutputDefault(ctx, result)
	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "hello world")
}

func TestSolutionOptions_writeActionOutputDefault_NonStreamedFailed(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	streams := &terminal.IOStreams{Out: &buf, ErrOut: &buf}
	cliParams := settings.NewCliParams()
	w := writer.New(streams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	opts := &SolutionOptions{}
	result := &action.ExecutionResult{
		Actions: map[string]*action.ActionResult{
			"deploy": {
				Streamed: false,
				Status:   action.StatusFailed,
				Error:    "command failed",
				Results:  map[string]any{"stderr": "error output\n"},
			},
		},
	}

	err := opts.writeActionOutputDefault(ctx, result)
	assert.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "command failed")
}

func TestSolutionOptions_writeActionOutputDefault_Skipped(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	streams := &terminal.IOStreams{Out: &buf, ErrOut: &buf}
	cliParams := settings.NewCliParams()
	w := writer.New(streams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	opts := &SolutionOptions{}
	result := &action.ExecutionResult{
		Actions: map[string]*action.ActionResult{
			"deploy": {
				Streamed:   false,
				Status:     action.StatusSkipped,
				SkipReason: "condition not met",
			},
		},
	}

	err := opts.writeActionOutputDefault(ctx, result)
	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "condition not met")
}

func TestSolutionOptions_writeActionOutputStructured_JSON(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	streams := &terminal.IOStreams{Out: &buf, ErrOut: &buf}
	cliParams := settings.NewCliParams()
	w := writer.New(streams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	opts := &SolutionOptions{}
	result := &action.ExecutionResult{
		Actions: map[string]*action.ActionResult{
			"deploy": {Status: action.StatusSucceeded},
		},
	}

	err := opts.writeActionOutputStructured(ctx, result, nil, "json")
	assert.NoError(t, err)
	assert.True(t, strings.HasPrefix(strings.TrimSpace(buf.String()), "{"))
}

func TestSolutionOptions_writeActionOutputStructured_YAML(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	streams := &terminal.IOStreams{Out: &buf, ErrOut: &buf}
	cliParams := settings.NewCliParams()
	w := writer.New(streams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	opts := &SolutionOptions{}
	result := &action.ExecutionResult{
		Actions: map[string]*action.ActionResult{
			"deploy": {Status: action.StatusSucceeded},
		},
	}

	err := opts.writeActionOutputStructured(ctx, result, nil, "yaml")
	assert.NoError(t, err)
	assert.NotEmpty(t, buf.String())
}

func TestSolutionOptions_writeDryRunOutput_JSON(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	streams := &terminal.IOStreams{Out: &buf, ErrOut: &buf}
	cliParams := settings.NewCliParams()
	w := writer.New(streams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	opts := &SolutionOptions{}
	opts.Output = "json"

	report := &dryrun.Report{
		Solution: "test-solution",
		Version:  "1.0.0",
	}

	err := opts.writeDryRunOutput(ctx, report)
	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "test-solution")
}

func TestSolutionOptions_writeDryRunOutput_YAML(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	streams := &terminal.IOStreams{Out: &buf, ErrOut: &buf}
	cliParams := settings.NewCliParams()
	w := writer.New(streams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	opts := &SolutionOptions{}
	opts.Output = "yaml"

	report := &dryrun.Report{
		Solution: "test-solution",
	}

	err := opts.writeDryRunOutput(ctx, report)
	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "test-solution")
}

func TestSolutionOptions_writeDryRunOutput_Quiet(t *testing.T) {
	t.Parallel()

	opts := &SolutionOptions{}
	opts.Output = "quiet"

	report := &dryrun.Report{Solution: "test"}
	err := opts.writeDryRunOutput(context.Background(), report)
	assert.NoError(t, err)
}

func TestSolutionOptions_writeDryRunTable(t *testing.T) {
	t.Parallel()

	t.Run("nil writer", func(t *testing.T) {
		t.Parallel()
		opts := &SolutionOptions{}
		err := opts.writeDryRunTable(context.Background(), &dryrun.Report{})
		assert.NoError(t, err)
	})

	t.Run("with action plan", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		streams := &terminal.IOStreams{Out: &buf, ErrOut: &buf}
		cliParams := settings.NewCliParams()
		w := writer.New(streams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)

		opts := &SolutionOptions{}
		report := &dryrun.Report{
			Solution:    "my-solution",
			Version:     "2.0.0",
			HasWorkflow: true,
			ActionPlan: []dryrun.WhatIfAction{
				{
					Name:         "deploy",
					Phase:        0,
					WhatIf:       "Would deploy the application",
					When:         "env == 'prod'",
					Dependencies: []string{"build"},
				},
			},
		}

		err := opts.writeDryRunTable(ctx, report)
		assert.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "DRY RUN")
		assert.Contains(t, output, "my-solution")
		assert.Contains(t, output, "deploy")
		assert.Contains(t, output, "Would deploy the application")
		assert.Contains(t, output, "env == 'prod'")
		assert.Contains(t, output, "build")
	})

	t.Run("no workflow", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		streams := &terminal.IOStreams{Out: &buf, ErrOut: &buf}
		cliParams := settings.NewCliParams()
		w := writer.New(streams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)

		opts := &SolutionOptions{}
		report := &dryrun.Report{
			Solution:    "no-workflow",
			HasWorkflow: false,
		}

		err := opts.writeDryRunTable(ctx, report)
		assert.NoError(t, err)
		assert.Contains(t, buf.String(), "No workflow defined")
	})

	t.Run("with warnings", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		streams := &terminal.IOStreams{Out: &buf, ErrOut: &buf}
		cliParams := settings.NewCliParams()
		w := writer.New(streams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)

		opts := &SolutionOptions{}
		report := &dryrun.Report{
			Solution:    "warn-sol",
			HasWorkflow: true,
			Warnings:    []string{"resolver 'db' has no inputs", "action 'cleanup' unreachable"},
		}

		err := opts.writeDryRunTable(ctx, report)
		assert.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "Warnings")
		assert.Contains(t, output, "resolver 'db' has no inputs")
	})

	t.Run("with deferred inputs and cross section refs", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		streams := &terminal.IOStreams{Out: &buf, ErrOut: &buf}
		cliParams := settings.NewCliParams()
		w := writer.New(streams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)

		opts := &SolutionOptions{}
		report := &dryrun.Report{
			Solution:    "complex",
			HasWorkflow: true,
			ActionPlan: []dryrun.WhatIfAction{
				{
					Name:               "deploy",
					Phase:              0,
					WhatIf:             "Would deploy",
					CrossSectionRefs:   []string{"build.output"},
					DeferredInputs:     map[string]string{"image": "{{build.output.image}}"},
					MaterializedInputs: map[string]any{"env": "production"},
				},
			},
		}

		err := opts.writeDryRunTable(ctx, report)
		assert.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "build.output")
		assert.Contains(t, output, "deferred")
	})

	t.Run("with fingerprint status", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		streams := &terminal.IOStreams{Out: &buf, ErrOut: &buf}
		cliParams := settings.NewCliParams()
		w := writer.New(streams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)

		opts := &SolutionOptions{}
		report := &dryrun.Report{
			Solution:    "fp-test",
			HasWorkflow: true,
			ActionPlan: []dryrun.WhatIfAction{
				{
					Name:              "build",
					Phase:             0,
					WhatIf:            "Would compile",
					FingerprintStatus: "up-to-date",
					FingerprintReason: "up-to-date",
				},
				{
					Name:              "deploy",
					Phase:             1,
					WhatIf:            "Would deploy",
					FingerprintStatus: "stale",
					FingerprintReason: "sources changed",
				},
				{
					Name:   "cleanup",
					Phase:  1,
					WhatIf: "Would cleanup",
				},
			},
		}

		err := opts.writeDryRunTable(ctx, report)
		assert.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "fingerprint: up-to-date")
		assert.Contains(t, output, "fingerprint: stale")
		assert.Contains(t, output, "sources changed")
		// cleanup has no fingerprint status — should not show fingerprint line
		assert.NotContains(t, output, "fingerprint: )")
	})
}

// Benchmarks

func BenchmarkActionProgressCallback(b *testing.B) {
	var buf bytes.Buffer
	streams := &terminal.IOStreams{Out: &buf, ErrOut: &buf}
	cliParams := settings.NewCliParams()
	w := writer.New(streams, cliParams)
	cb := NewActionProgressCallback(w)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cb.OnActionStart("deploy", "")
		cb.OnActionComplete("deploy", nil)
	}
}

func BenchmarkGetActionIOStreams(b *testing.B) {
	opts := &SolutionOptions{}
	opts.Output = "json"
	opts.IOStreams = &terminal.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		opts.getActionIOStreams()
	}
}

func TestActionProgressCallback_GitHubActionsSingleActiveGroup(t *testing.T) {
	t.Parallel()

	t.Run("sequential actions wrap in group and emit error annotation after endgroup", func(t *testing.T) {
		t.Parallel()
		var outBuf, errBuf bytes.Buffer
		cliParams := settings.NewCliParams()
		cliParams.NoColor = true
		w := writer.New(&terminal.IOStreams{Out: &outBuf, ErrOut: &errBuf}, cliParams)
		cb := NewActionProgressCallbackWithDialect(w, output.GitHubActionsDialect())

		cb.OnActionStart("build", "Compile")
		cb.OnActionComplete("build", nil)

		cb.OnActionStart("test", "Unit tests")
		cb.OnActionFailed("test", assert.AnError)

		lines := strings.Split(strings.TrimSpace(errBuf.String()), "\n")
		require.Equal(t, "::group::build: Compile", lines[0])
		assert.Contains(t, lines[1], "==> build: Compile")
		assert.Contains(t, lines[2], "done: build")
		assert.Equal(t, "::endgroup::", lines[3])
		assert.Equal(t, "::group::test: Unit tests", lines[4])
		assert.Contains(t, lines[5], "==> test: Unit tests")
		assert.Contains(t, lines[6], "failed: test")
		assert.Equal(t, "::endgroup::", lines[7])
		assert.Contains(t, lines[8], "::error title=test::")
		assert.True(t, cb.PrintedFailure())
	})

	t.Run("concurrent parallel actions never emit nested group markers", func(t *testing.T) {
		t.Parallel()
		var errBuf bytes.Buffer
		cliParams := settings.NewCliParams()
		cliParams.NoColor = true
		w := writer.New(&terminal.IOStreams{Out: &bytes.Buffer{}, ErrOut: &errBuf}, cliParams)
		cb := NewActionProgressCallbackWithDialect(w, output.GitHubActionsDialect())

		cb.OnActionStart("lint", "Lint")
		cb.OnActionStart("test", "Test")
		cb.OnActionComplete("test", nil)
		cb.OnActionComplete("lint", nil)

		stderr := errBuf.String()
		assert.Equal(t, 1, strings.Count(stderr, "::group::"), "parallel actions must not nest ::group::")
		assert.Equal(t, 1, strings.Count(stderr, "::endgroup::"), "::endgroup:: must close once when activeGroup finishes")
		assert.Contains(t, stderr, "==> lint: Lint")
		assert.Contains(t, stderr, "==> test: Test")
	})

	t.Run("timeout closes active group and emits error annotation", func(t *testing.T) {
		t.Parallel()
		var errBuf bytes.Buffer
		cliParams := settings.NewCliParams()
		cliParams.NoColor = true
		w := writer.New(&terminal.IOStreams{Out: &bytes.Buffer{}, ErrOut: &errBuf}, cliParams)
		cb := NewActionProgressCallbackWithDialect(w, output.GitHubActionsDialect())

		cb.OnActionStart("slow", "")
		cb.OnActionTimeout("slow", 5*time.Second)

		stderr := errBuf.String()
		endIdx := strings.Index(stderr, "::endgroup::")
		errIdx := strings.Index(stderr, "::error title=slow::action timed out after 5s")
		require.NotEqual(t, -1, endIdx)
		require.NotEqual(t, -1, errIdx)
		assert.Less(t, endIdx, errIdx, "::error annotation must be emitted after ::endgroup::")
	})

	t.Run("parallel sibling failure force-closes active group before emitting error annotation", func(t *testing.T) {
		t.Parallel()
		var errBuf bytes.Buffer
		cliParams := settings.NewCliParams()
		cliParams.NoColor = true
		w := writer.New(&terminal.IOStreams{Out: &bytes.Buffer{}, ErrOut: &errBuf}, cliParams)
		cb := NewActionProgressCallbackWithDialect(w, output.GitHubActionsDialect())

		cb.OnActionStart("lint", "Lint")
		cb.OnActionStart("test", "Test")
		cb.OnActionFailed("test", fmt.Errorf("unit test failed"))
		cb.OnActionComplete("lint", nil)

		stderr := errBuf.String()
		endIdx := strings.Index(stderr, "::endgroup::")
		errIdx := strings.Index(stderr, "::error title=test::unit test failed")
		require.NotEqual(t, -1, endIdx)
		require.NotEqual(t, -1, errIdx)
		assert.Less(t, endIdx, errIdx, "active group must close before sibling ::error annotation")
		assert.Equal(t, 1, strings.Count(stderr, "::endgroup::"))
	})

	t.Run("sanitizes workflow command injection in banners and error lines", func(t *testing.T) {
		t.Parallel()
		var errBuf bytes.Buffer
		cliParams := settings.NewCliParams()
		cliParams.NoColor = true
		w := writer.New(&terminal.IOStreams{Out: &bytes.Buffer{}, ErrOut: &errBuf}, cliParams)
		cb := NewActionProgressCallbackWithDialect(w, output.GitHubActionsDialect())

		cb.OnActionStart("step", "Desc\r\n::stop-commands::tok")
		cb.OnActionFailed("step", fmt.Errorf("line1\r::add-matcher::evil.json\n  ::error file=a.go::spoof"))

		stderr := errBuf.String()
		for _, line := range strings.Split(stderr, "\n") {
			trimmed := strings.TrimLeft(line, " \t")
			if strings.HasPrefix(trimmed, "::") {
				assert.True(t,
					strings.HasPrefix(trimmed, "::group::") ||
						strings.HasPrefix(trimmed, "::endgroup::") ||
						strings.HasPrefix(trimmed, "::error title=step::"),
					"unexpected raw workflow command on stderr line: %q", line,
				)
			}
		}
	})
}

func TestSynchronizeActionStreams_PreservesCloneAndAlignsDirectives(t *testing.T) {
	t.Parallel()

	var combined bytes.Buffer
	cliParams := settings.NewCliParams()
	cliParams.NoColor = true
	base := &terminal.IOStreams{Out: &combined, ErrOut: &combined}
	w := writer.New(base, cliParams, writer.WithHumanToStderr())

	cbWriter, synced := synchronizeActionStreams(w, &provider.IOStreams{Out: &combined, ErrOut: &combined})
	require.NotNil(t, cbWriter)
	require.NotNil(t, synced)
	assert.Same(t, synced.Out, synced.ErrOut, "identical Out and ErrOut must share one synchronized writer")

	// Ordinary provider writes split across chunks remain byte-preserving.
	_, err := synced.Out.Write([]byte("prefix"))
	require.NoError(t, err)
	_, err = synced.Out.Write([]byte("::error::literal\n"))
	require.NoError(t, err)
	assert.Equal(t, "prefix::error::literal\n", combined.String())

	// Write unterminated provider output followed by an explicit CI workflow directive.
	_, err = synced.ErrOut.Write([]byte("partial stderr without newline"))
	require.NoError(t, err)
	cbWriter.PlainStderrDirective("::endgroup::")
	assert.Equal(t, "prefix::error::literal\npartial stderr without newline\n::endgroup::\n", combined.String())

	// Also works when actionStreams is nil (structured output modes).
	cbWriterNil, syncedNil := synchronizeActionStreams(w, nil)
	assert.NotNil(t, cbWriterNil)
	assert.Nil(t, syncedNil)
}

func BenchmarkActionProgressCallback_GitHubActions(b *testing.B) {
	cliParams := settings.NewCliParams()
	cliParams.NoColor = true
	w := writer.New(&terminal.IOStreams{Out: io.Discard, ErrOut: io.Discard}, cliParams)
	cb := NewActionProgressCallbackWithDialect(w, output.GitHubActionsDialect())

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		cb.OnActionStart("build", "Compile binary")
		cb.OnActionComplete("build", nil)
	}
}

func TestWriteActionOutputDefault_DeterministicOrderStdoutBeforeStderrAndNoColorVerbose(t *testing.T) {
	t.Parallel()

	var outBuf, errBuf bytes.Buffer
	cliParams := settings.NewCliParams()
	cliParams.NoColor = true
	cliParams.Verbose = true
	w := writer.New(&terminal.IOStreams{Out: &outBuf, ErrOut: &errBuf}, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	opts := &SolutionOptions{
		Verbose:                true,
		progressPrintedFailure: true,
	}
	result := &action.ExecutionResult{
		Actions: map[string]*action.ActionResult{
			"z-cancel": {Status: action.StatusCancelled},
			"a-ok":     {Status: action.StatusSucceeded, Streamed: true},
			"b-skip":   {Status: action.StatusSkipped, SkipReason: action.SkipReasonCondition},
			"c-fail": {
				Status: action.StatusFailed,
				Error:  "exit 1",
				Results: map[string]any{
					"stdout": "unstreamed failure stdout\n",
					"stderr": "unstreamed failure stderr\n",
				},
			},
			"d-timeout": {Status: action.StatusTimeout, Error: "action timed out"},
		},
	}

	require.NoError(t, opts.writeActionOutputDefault(ctx, result))
	assert.Equal(t, "unstreamed failure stdout\n", outBuf.String())

	stderr := errBuf.String()
	assert.NotContains(t, stderr, "✓")
	assert.NotContains(t, stderr, "✗")
	assert.NotContains(t, stderr, "○")
	assert.NotContains(t, stderr, "Error [c-fail]", "duplicate Error line must be suppressed when progress callback already printed failure")

	idxA := strings.Index(stderr, "  [ok] a-ok")
	idxB := strings.Index(stderr, "  [skip] b-skip")
	idxC := strings.Index(stderr, "  [fail] c-fail")
	idxD := strings.Index(stderr, "  [fail] d-timeout")
	idxZ := strings.Index(stderr, "  [cancel] z-cancel")
	assert.True(t, idxA >= 0 && idxA < idxB && idxB < idxC && idxC < idxD && idxD < idxZ,
		"actions must be rendered in deterministic sorted order: %s", stderr)
}
