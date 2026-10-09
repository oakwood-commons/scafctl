// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package eval

import (
	"context"
	"fmt"
	"sort"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	refslib "github.com/oakwood-commons/scafctl/pkg/resolver/refs"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// RefsOptions holds options for the refs command
type RefsOptions struct {
	IOStreams *terminal.IOStreams
	flags.KvxOutputFlags
	TemplateFile string
	Template     string
	Expr         string
	LeftDelim    string
	RightDelim   string
}

// refsOutputFormats is the subset of kvx formats that render meaningfully for
// the single-object refs result; table/list/tree/mermaid add nothing for one
// object and are omitted so help, PreRunE validation, and routing stay in sync.
var refsOutputFormats = []string{
	string(kvx.OutputFormatAuto),
	string(kvx.OutputFormatJSON),
	string(kvx.OutputFormatYAML),
	string(kvx.OutputFormatCSV),
	string(kvx.OutputFormatTOML),
	string(kvx.OutputFormatText),
	string(kvx.OutputFormatQuiet),
}

// CommandRefs creates the resolver refs command
func CommandRefs(cliParams *settings.Run, ioStreams *terminal.IOStreams, binaryName string) *cobra.Command {
	opts := &RefsOptions{}

	cmd := &cobra.Command{
		Use:          "refs",
		Short:        "Extract resolver references from templates or expressions",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			Extract resolver references from Go templates or CEL expressions.
			
			This command parses templates or expressions and extracts all references
			to resolvers (_.resolverName patterns). This is useful for determining
			what to add to the 'dependsOn' field when templates are loaded dynamically.
			
			Supported input types:
			  - Go template file (--template-file)
			  - Inline Go template (--template)
			  - Inline CEL expression (--expr)
			
			Use '-' as the value for --template or --expr to read from stdin.
			
			For Go templates, custom delimiters can be specified with --left-delim
			and --right-delim flags.
		`),
		Example: heredoc.Docf(`
			# Extract references from a template file
			$ %[1]s eval refs --template-file template.tmpl
			
			# Extract references with custom delimiters
			$ %[1]s eval refs --template-file template.tmpl --left-delim '<%' --right-delim '%%>'
			
			# Extract references from inline template
			$ %[1]s eval refs --template '{{ ._.config.host }}:{{ ._.port }}'
			
			# Extract references from CEL expression
			$ %[1]s eval refs --expr '_.config.host + ":" + string(_.port)'
			
			# Output as JSON
			$ %[1]s eval refs --template-file template.tmpl -o json
			
			# Output as YAML
			$ %[1]s eval refs --expr '_.a + _.b' -o yaml
			
			# Read template from stdin
			$ cat template.tmpl | %[1]s eval refs --template -
			
			# Read CEL expression from stdin
			$ echo '_.config.host' | %[1]s eval refs --expr -
		`, binaryName),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			w := writer.FromContext(ctx)
			if w == nil {
				w = writer.New(ioStreams, cliParams)
				ctx = writer.WithWriter(ctx, w)
			}
			opts.IOStreams = ioStreams
			return runRefs(ctx, opts)
		},
	}

	cmd.Flags().StringVar(&opts.TemplateFile, "template-file", "", "Path to Go template file")
	cmd.Flags().StringVar(&opts.Template, "template", "", "Inline Go template content (use '-' to read from stdin)")
	cmd.Flags().StringVar(&opts.Expr, "expr", "", "Inline CEL expression (use '-' to read from stdin)")
	cmd.Flags().StringVar(&opts.LeftDelim, "left-delim", "{{", "Left delimiter for Go templates")
	cmd.Flags().StringVar(&opts.RightDelim, "right-delim", "}}", "Right delimiter for Go templates")
	flags.AddKvxOutputFormatFlagToStructWithFormats(cmd, &opts.KvxOutputFlags, refsOutputFormats)

	return cmd
}

func runRefs(ctx context.Context, opts *RefsOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	// Helper to write error
	writeErr := func(err error) {
		if w != nil {
			w.Errorf("%v", err)
		}
	}

	// Validate that exactly one input source is provided
	inputCount := 0
	if opts.TemplateFile != "" {
		inputCount++
	}
	if opts.Template != "" {
		inputCount++
	}
	if opts.Expr != "" {
		inputCount++
	}

	if inputCount == 0 {
		err := fmt.Errorf("one of --template-file, --template, or --expr is required")
		writeErr(err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}
	if inputCount > 1 {
		err := fmt.Errorf("only one of --template-file, --template, or --expr can be specified")
		writeErr(err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	var refs []string
	var sourceType, source string
	var err error

	switch {
	case opts.TemplateFile != "":
		sourceType = "template-file"
		source = opts.TemplateFile
		refs, err = refslib.ExtractFromTemplateFile(opts.TemplateFile, opts.LeftDelim, opts.RightDelim)

	case opts.Template != "":
		sourceType = "template"
		if opts.Template == "-" {
			sourceType = "template-stdin"
			opts.Template, err = refslib.ReadStdin(opts.IOStreams.In)
			if err != nil {
				writeErr(err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}
		}
		source = opts.Template
		refs, err = refslib.ExtractFromTemplate(opts.Template, opts.LeftDelim, opts.RightDelim)

	case opts.Expr != "":
		sourceType = "cel-expression"
		if opts.Expr == "-" {
			sourceType = "cel-expression-stdin"
			opts.Expr, err = refslib.ReadStdin(opts.IOStreams.In)
			if err != nil {
				writeErr(err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}
		}
		source = opts.Expr
		refs, err = refslib.ExtractFromCEL(ctx, opts.Expr)
	}

	if err != nil {
		writeErr(err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	lgr.V(1).Info("extracted resolver references", "count", len(refs), "sourceType", sourceType)

	// Sort refs for consistent output
	sort.Strings(refs)

	output := refslib.Output{
		Source:     source,
		SourceType: sourceType,
		References: refs,
		Count:      len(refs),
	}

	kvxOpts := flags.ToKvxOutputOptions(&opts.KvxOutputFlags,
		kvx.WithIOStreams(opts.IOStreams),
		kvx.WithOutputColumnOrder(refslib.RefsColumnOrder),
	)
	// auto is the only format rendered through the styled human block; every
	// other allowed format is a serialization that kvx emits directly.
	if kvxOpts.Format != kvx.OutputFormatAuto {
		return kvxOpts.Write(refsForFormat(output, kvxOpts.Format))
	}
	return writeRefsHuman(ctx, output)
}

// refsForFormat returns the shape kvx needs for the given format. kvx's CSV
// writer renders slice cells via %v, so References is flattened to a joined
// string for CSV; every other format consumes the struct natively via its
// json/yaml tags.
func refsForFormat(o refslib.Output, f kvx.OutputFormat) any {
	if f == kvx.OutputFormatCSV {
		return o.ToMap()
	}
	return o
}

// writeRefsHuman renders the styled auto view: a bold header, a plain bullet
// per reference, and a success-styled total.
func writeRefsHuman(ctx context.Context, output refslib.Output) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return nil
	}
	if len(output.References) == 0 {
		w.Infof("No resolver references found in %s.", output.SourceType)
		return nil
	}
	w.SectionHeader(fmt.Sprintf("Resolver references found in %s:", output.SourceType))
	for _, ref := range output.References {
		w.Plainlnf("  - %s", ref)
	}
	w.Plainln("")
	w.Successf("Total: %d reference(s)", output.Count)
	return nil
}
