// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/dryrun"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
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

func TestActionRegistryAdapter_Has(t *testing.T) {
	t.Parallel()

	reg := testRegistry()
	adapter := &actionRegistryAdapter{registry: reg}

	assert.True(t, adapter.Has("static"))
	assert.False(t, adapter.Has("nonexistent"))
}

func TestActionRegistryAdapter_Get(t *testing.T) {
	t.Parallel()

	reg := testRegistry()
	adapter := &actionRegistryAdapter{registry: reg}

	provider, ok := adapter.Get("static")
	assert.True(t, ok)
	assert.NotNil(t, provider)

	provider, ok = adapter.Get("nonexistent")
	assert.False(t, ok)
	assert.Nil(t, provider)
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
		w := writer.New(streams, cliParams)
		return &buf, NewActionProgressCallback(w)
	}

	t.Run("OnActionStart", func(t *testing.T) {
		t.Parallel()
		buf, cb := newCB()
		cb.OnActionStart("deploy")
		assert.Contains(t, buf.String(), "deploy")
	})

	t.Run("OnActionComplete", func(t *testing.T) {
		t.Parallel()
		buf, cb := newCB()
		cb.OnActionComplete("deploy", nil)
		assert.Contains(t, buf.String(), "deploy")
	})

	t.Run("OnActionFailed", func(t *testing.T) {
		t.Parallel()
		buf, cb := newCB()
		cb.OnActionFailed("deploy", assert.AnError)
		assert.Contains(t, buf.String(), "deploy")
		assert.Contains(t, buf.String(), "Failed")
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
		assert.Contains(t, buf.String(), "Timeout")
	})

	t.Run("OnActionCancelled", func(t *testing.T) {
		t.Parallel()
		buf, cb := newCB()
		cb.OnActionCancelled("deploy")
		assert.Contains(t, buf.String(), "deploy")
		assert.Contains(t, buf.String(), "Cancelled")
	})

	t.Run("OnRetryAttempt", func(t *testing.T) {
		t.Parallel()
		buf, cb := newCB()
		cb.OnRetryAttempt("deploy", 1, 3, assert.AnError)
		assert.Contains(t, buf.String(), "Retry")
		assert.Contains(t, buf.String(), "1/3")
	})

	t.Run("OnForEachProgress", func(t *testing.T) {
		t.Parallel()
		buf, cb := newCB()
		cb.OnForEachProgress("deploy", 5, 10)
		assert.Contains(t, buf.String(), "5/10")
	})

	t.Run("OnPhaseStart", func(t *testing.T) {
		t.Parallel()
		buf, cb := newCB()
		cb.OnPhaseStart(1, []string{"deploy", "test"})
		assert.Contains(t, buf.String(), "phase 1")
		assert.Contains(t, buf.String(), "deploy")
	})

	t.Run("OnPhaseComplete", func(t *testing.T) {
		t.Parallel()
		buf, cb := newCB()
		cb.OnPhaseComplete(1)
		assert.Contains(t, buf.String(), "phase 1")
	})

	t.Run("OnFinallyStart", func(t *testing.T) {
		t.Parallel()
		buf, cb := newCB()
		cb.OnFinallyStart()
		assert.Contains(t, buf.String(), "finally")
	})

	t.Run("OnFinallyComplete", func(t *testing.T) {
		t.Parallel()
		buf, cb := newCB()
		cb.OnFinallyComplete()
		assert.Contains(t, buf.String(), "finally")
	})
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
		cb.OnActionStart("deploy")
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
