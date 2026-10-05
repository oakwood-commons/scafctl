// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package diff

import (
	"context"
	_ "embed"

	"github.com/oakwood-commons/kvx/pkg/core"
	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/diffreport"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

//go:embed diff_schema.json
var diffDisplaySchema []byte

// writeDiffOutput renders a diff in the configured output format, shared by all
// diff subcommands:
//   - quiet: nothing
//   - json/yaml: the native per-command struct (shape preserved)
//   - auto/text (non-interactive): the shared human diff report
//   - everything else (table/list/tree/csv/toml/mermaid/-i): flattened entries
func writeDiffOutput(
	ctx context.Context,
	w *writer.Writer,
	ioStreams *terminal.IOStreams,
	outputFlags *flags.KvxOutputFlags,
	appName string,
	native any,
	report *diffreport.Report,
) error {
	opts := flags.ToKvxOutputOptions(outputFlags,
		kvx.WithOutputContext(ctx),
		kvx.WithIOStreams(ioStreams),
		kvx.WithOutputNoColor(w.NoColor()),
		kvx.WithOutputAppName(appName),
		kvx.WithOutputDisplaySchemaJSON(diffDisplaySchema),
		// Entries are intentionally multi-column; force columnar rendering so the
		// table shows field headers (kind/group/path) instead of collapsing to a
		// KEY/VALUE object view when rows have omitempty/nested fields.
		kvx.WithOutputColumnarMode(tui.ColumnarModeAlways),
		kvx.WithOutputColumnOrder([]string{"kind", "group", "path"}),
		kvx.WithOutputColumnHints(map[string]tui.ColumnHint{
			"kind":   {MaxWidth: 10, Priority: 10},
			"group":  {MaxWidth: 16, Priority: 8},
			"path":   {MaxWidth: 40, Priority: 9},
			"detail": {Hidden: true},
			"before": {Hidden: true},
			"after":  {Hidden: true},
		}),
	)

	switch {
	case opts.Format == kvx.OutputFormatQuiet:
		return nil
	case opts.Format == kvx.OutputFormatJSON || opts.Format == kvx.OutputFormatYAML:
		return opts.Write(native)
	case isDefaultReportFormat(opts):
		diffreport.Render(w, report)
		return nil
	default:
		// Normalize the typed entry slice into kvx's generic form so tabular
		// formats (csv/toml) serialize one row per entry instead of dumping the
		// whole slice into a single cell.
		rows, err := core.LoadObject(report.Entries)
		if err != nil {
			return err
		}
		return opts.Write(rows)
	}
}

// isDefaultReportFormat reports whether output should render the shared human
// diff report rather than the flattened kvx view. Interactive mode and active
// CEL filters (-e/-w) fall through to kvx so those features keep working.
func isDefaultReportFormat(opts *kvx.OutputOptions) bool {
	if opts.Interactive || opts.Expression != "" || opts.Where != "" {
		return false
	}
	return opts.Format == kvx.OutputFormatAuto || opts.Format == kvx.OutputFormatText
}
