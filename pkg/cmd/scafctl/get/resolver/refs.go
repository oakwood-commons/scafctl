package resolver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// RefsOptions holds options for the refs command
type RefsOptions struct {
	TemplateFile string
	Template     string
	Expr         string
	LeftDelim    string
	RightDelim   string
	Output       string
}

// RefsOutput represents the output structure for refs command
type RefsOutput struct {
	Source     string   `json:"source" yaml:"source"`
	SourceType string   `json:"sourceType" yaml:"sourceType"`
	References []string `json:"references" yaml:"references"`
	Count      int      `json:"count" yaml:"count"`
}

// CommandRefs creates the resolver refs command
func CommandRefs(_ *settings.Run, ioStreams *terminal.IOStreams, binaryName string) *cobra.Command {
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
			$ %[1]s get resolver refs --template-file template.tmpl
			
			# Extract references with custom delimiters
			$ %[1]s get resolver refs --template-file template.tmpl --left-delim '<%' --right-delim '%%>'
			
			# Extract references from inline template
			$ %[1]s get resolver refs --template '{{ ._.config.host }}:{{ ._.port }}'
			
			# Extract references from CEL expression
			$ %[1]s get resolver refs --expr '_.config.host + ":" + string(_.port)'
			
			# Output as JSON
			$ %[1]s get resolver refs --template-file template.tmpl -o json
			
			# Output as YAML
			$ %[1]s get resolver refs --expr '_.a + _.b' -o yaml
			
			# Read template from stdin
			$ cat template.tmpl | %[1]s get resolver refs --template -
			
			# Read CEL expression from stdin
			$ echo '_.config.host' | %[1]s get resolver refs --expr -
		`, binaryName),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRefs(cmd.Context(), opts, ioStreams)
		},
	}

	cmd.Flags().StringVar(&opts.TemplateFile, "template-file", "", "Path to Go template file")
	cmd.Flags().StringVar(&opts.Template, "template", "", "Inline Go template content (use '-' to read from stdin)")
	cmd.Flags().StringVar(&opts.Expr, "expr", "", "Inline CEL expression (use '-' to read from stdin)")
	cmd.Flags().StringVar(&opts.LeftDelim, "left-delim", "{{", "Left delimiter for Go templates")
	cmd.Flags().StringVar(&opts.RightDelim, "right-delim", "}}", "Right delimiter for Go templates")
	cmd.Flags().StringVarP(&opts.Output, "output", "o", "text", "Output format: text, json, yaml")

	return cmd
}

func runRefs(ctx context.Context, opts *RefsOptions, ioStreams *terminal.IOStreams) error {
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
		refs, err = extractRefsFromTemplateFile(opts.TemplateFile, opts.LeftDelim, opts.RightDelim)

	case opts.Template != "":
		sourceType = "template"
		if opts.Template == "-" {
			sourceType = "template-stdin"
			opts.Template, err = readStdin(ioStreams.In)
			if err != nil {
				writeErr(err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}
		}
		source = opts.Template
		refs, err = extractRefsFromTemplate(opts.Template, opts.LeftDelim, opts.RightDelim)

	case opts.Expr != "":
		sourceType = "cel-expression"
		if opts.Expr == "-" {
			sourceType = "cel-expression-stdin"
			opts.Expr, err = readStdin(ioStreams.In)
			if err != nil {
				writeErr(err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}
		}
		source = opts.Expr
		refs, err = extractRefsFromCEL(opts.Expr)
	}

	if err != nil {
		writeErr(err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	lgr.V(1).Info("extracted resolver references", "count", len(refs), "sourceType", sourceType)

	// Sort refs for consistent output
	sort.Strings(refs)

	output := RefsOutput{
		Source:     source,
		SourceType: sourceType,
		References: refs,
		Count:      len(refs),
	}

	return writeOutput(ioStreams, opts.Output, output)
}

func readStdin(r io.Reader) (string, error) {
	if r == nil {
		return "", fmt.Errorf("stdin is not available")
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("failed to read from stdin: %w", err)
	}
	return strings.TrimSuffix(string(data), "\n"), nil
}

func extractRefsFromTemplateFile(filePath, leftDelim, rightDelim string) ([]string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read template file: %w", err)
	}

	return extractRefsFromTemplate(string(content), leftDelim, rightDelim)
}

func extractRefsFromTemplate(content, leftDelim, rightDelim string) ([]string, error) {
	templateRefs, err := gotmpl.GetGoTemplateReferences(content, leftDelim, rightDelim)
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}

	// Extract resolver names from paths and deduplicate
	seen := make(map[string]bool)
	var refs []string

	for _, ref := range templateRefs {
		name := extractResolverName(ref.Path)
		if name != "" && !seen[name] {
			seen[name] = true
			refs = append(refs, name)
		}
	}

	return refs, nil
}

func extractRefsFromCEL(expr string) ([]string, error) {
	celExpr := celexp.Expression(expr)
	vars, err := celExpr.GetUnderscoreVariables()
	if err != nil {
		return nil, fmt.Errorf("failed to parse CEL expression: %w", err)
	}

	return vars, nil
}

// extractResolverName extracts the resolver name from a template path
// e.g., "._.config.host" -> "config", ".config" -> "config"
func extractResolverName(path string) string {
	// Remove leading dot
	if len(path) > 0 && path[0] == '.' {
		path = path[1:]
	}

	// Handle _.resolverName pattern
	if len(path) > 2 && path[0] == '_' && path[1] == '.' {
		path = path[2:]
	} else if len(path) > 1 && path[0] == '_' {
		path = path[1:]
	}

	// Get first segment (resolver name)
	for i, c := range path {
		if c == '.' {
			return path[:i]
		}
	}

	return path
}

func writeOutput(ioStreams *terminal.IOStreams, format string, output RefsOutput) error {
	switch format {
	case "json":
		enc := json.NewEncoder(ioStreams.Out)
		enc.SetIndent("", "  ")
		return enc.Encode(output)

	case "yaml":
		enc := yaml.NewEncoder(ioStreams.Out)
		enc.SetIndent(2)
		return enc.Encode(output)

	case "text":
		if len(output.References) == 0 {
			fmt.Fprintln(ioStreams.Out, "No resolver references found.")
			return nil
		}

		fmt.Fprintf(ioStreams.Out, "Resolver references found in %s:\n", output.SourceType)
		for _, ref := range output.References {
			fmt.Fprintf(ioStreams.Out, "  - %s\n", ref)
		}
		fmt.Fprintf(ioStreams.Out, "\nTotal: %d reference(s)\n", output.Count)
		return nil

	default:
		return exitcode.WithCode(fmt.Errorf("unknown output format: %s (supported: text, json, yaml)", format), exitcode.InvalidInput)
	}
}
