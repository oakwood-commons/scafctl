// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package diff

import (
	"bytes"
	"context"
	"testing"

	"github.com/oakwood-commons/kvx/pkg/core"
	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/diffreport"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDiffDisplaySchema_ParsesAndRenders validates the embedded interactive
// display schema (used by the -i card/detail view, which the non-TTY table
// tests never exercise) and that flattened entries render through it.
func TestDiffDisplaySchema_ParsesAndRenders(t *testing.T) {
	_, ds, err := tui.ParseSchemaWithDisplay(diffDisplaySchema)
	require.NoError(t, err)
	require.NotNil(t, ds)
	require.NotNil(t, ds.List)
	assert.Equal(t, "path", ds.List.TitleField)

	report := diffreport.New("snapshot", "a", "b")
	report.Add(diffreport.Entry{Group: "res", Path: "value", Kind: diffreport.ChangeModified, Before: "x", After: "y"})
	root, err := core.LoadObject(report.Entries)
	require.NoError(t, err)

	out := tui.RenderSnapshot(root, tui.Config{
		AppName:       "scafctl diff snapshot",
		Width:         100,
		Height:        24,
		NoColor:       true,
		DisplaySchema: ds,
	})
	assert.Contains(t, out, "value")
}

func TestIsDefaultReportFormat(t *testing.T) {
	tests := []struct {
		name string
		opts kvx.OutputOptions
		want bool
	}{
		{name: "auto", opts: kvx.OutputOptions{Format: kvx.OutputFormatAuto}, want: true},
		{name: "text", opts: kvx.OutputOptions{Format: kvx.OutputFormatText}, want: true},
		{name: "table", opts: kvx.OutputOptions{Format: kvx.OutputFormatTable}, want: false},
		{name: "auto interactive", opts: kvx.OutputOptions{Format: kvx.OutputFormatAuto, Interactive: true}, want: false},
		{name: "auto with expression", opts: kvx.OutputOptions{Format: kvx.OutputFormatAuto, Expression: "_"}, want: false},
		{name: "auto with where", opts: kvx.OutputOptions{Format: kvx.OutputFormatAuto, Where: "_.kind == 'added'"}, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := tc.opts
			assert.Equal(t, tc.want, isDefaultReportFormat(&opts))
		})
	}
}

func TestWriteDiffOutput_TableHasFieldColumns(t *testing.T) {
	report := diffreport.New("snapshot", "a", "b")
	report.Add(diffreport.Entry{Group: "res1", Path: "value", Kind: diffreport.ChangeModified, Before: "x", After: "y"})
	report.Add(diffreport.Entry{Path: "res2", Kind: diffreport.ChangeAdded})

	var stdout, stderr bytes.Buffer
	ioStreams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}
	w := writer.New(ioStreams, &settings.Run{NoColor: true})
	outputFlags := &flags.KvxOutputFlags{Output: "table"}

	err := writeDiffOutput(context.Background(), w, ioStreams, outputFlags, "scafctl diff snapshot", map[string]any{}, report)
	require.NoError(t, err)

	out := stdout.String()
	// Field-name column headers, not a two-column KEY/VALUE object view.
	assert.Contains(t, out, "kind")
	assert.Contains(t, out, "path")
	assert.NotContains(t, out, "KEY")
	assert.NotContains(t, out, "VALUE")
}

func TestWriteDiffOutput_Routing(t *testing.T) {
	report := diffreport.New("solution", "a", "b")
	report.Add(diffreport.Entry{Path: "metadata.name", Kind: diffreport.ChangeModified, Before: "x", After: "y"})
	native := map[string]any{"nativeField": "present"}

	tests := []struct {
		name        string
		format      string
		interactive bool
		wantContain string
		wantEmpty   bool
	}{
		{name: "default auto renders report", format: "", wantContain: "Diff: a -> b"},
		{name: "text renders report", format: "text", wantContain: "Diff: a -> b"},
		{name: "json uses native", format: "json", wantContain: "nativeField"},
		{name: "quiet is empty", format: "quiet", wantEmpty: true},
		{name: "table uses entries", format: "table", wantContain: "metadata.name"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			ioStreams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}
			w := writer.New(ioStreams, &settings.Run{NoColor: true})
			outputFlags := &flags.KvxOutputFlags{Output: tc.format, Interactive: tc.interactive}

			err := writeDiffOutput(context.Background(), w, ioStreams, outputFlags, "scafctl diff solution", native, report)
			require.NoError(t, err)

			if tc.wantEmpty {
				assert.Empty(t, stdout.String())
				return
			}
			assert.Contains(t, stdout.String(), tc.wantContain)
		})
	}
}
