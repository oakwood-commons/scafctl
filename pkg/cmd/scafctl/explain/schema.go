package explain

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/schema"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// SchemaOptions holds configuration for the explain schema command
type SchemaOptions struct {
	IOStreams *terminal.IOStreams
	CliParams *settings.Run

	// Expression is a CEL-style path to drill into nested fields
	Expression string

	// Recursive shows all nested fields at full depth
	Recursive bool
}

// CommandSchema creates the 'explain <kind>' subcommand for schema browsing
func CommandSchema(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	options := &SchemaOptions{}

	// Get available kinds for documentation
	kindNames := schema.GetGlobalRegistry().Names()
	sort.Strings(kindNames)

	cCmd := &cobra.Command{
		Use:   "<kind> [field.path]",
		Short: "Explain the schema for a resource kind",
		Long: fmt.Sprintf(`Show detailed schema documentation for a resource kind.

This command displays the struct definition, field types, validation rules,
and documentation extracted from Go struct tags. Use it to understand what
fields are available and their constraints.

AVAILABLE KINDS:
  %s

FIELD PATH:
  Use dot notation to drill into nested fields:
    scafctl explain provider.schema
    scafctl explain solution.metadata
    scafctl explain action.retry

OUTPUT:
  - Field name and type
  - Required/optional status
  - Validation constraints (minLength, maxLength, pattern, etc.)
  - Field description from documentation
  - Example values

Examples:
  # Show the full Provider Descriptor schema
  scafctl explain provider

  # Drill into the schema field
  scafctl explain provider.schema

  # Show Action schema with all nested fields
  scafctl explain action --recursive

  # Show Resolver schema
  scafctl explain resolver`, strings.Join(kindNames, ", ")),
		Args: cobra.MinimumNArgs(1),
		RunE: func(cCmd *cobra.Command, args []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cCmd.Use)
			ctx := settings.IntoContext(context.Background(), cliParams)

			options.IOStreams = ioStreams
			options.CliParams = cliParams

			return options.Run(ctx, args)
		},
		ValidArgsFunction: func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				// Complete kind names
				var completions []string
				for _, kind := range schema.ListKinds() {
					if strings.HasPrefix(strings.ToLower(kind.Name), strings.ToLower(toComplete)) {
						completions = append(completions, kind.Name)
					}
				}
				return completions, cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		SilenceUsage: true,
	}

	cCmd.Flags().StringVarP(&options.Expression, "expression", "e", "", "CEL-style path to drill into nested fields (alternative to positional path)")
	cCmd.Flags().BoolVarP(&options.Recursive, "recursive", "R", false, "Show all nested fields at maximum depth")

	return cCmd
}

// Run executes the explain schema command
func (o *SchemaOptions) Run(_ context.Context, args []string) error {
	w := writer.New(o.IOStreams, o.CliParams)

	// Parse the kind and optional field path
	kindAndPath := args[0]
	parts := strings.SplitN(kindAndPath, ".", 2)
	kindName := parts[0]

	// Get field path from positional arg or --expression flag
	var fieldPath string
	if len(parts) > 1 {
		fieldPath = parts[1]
	}
	if o.Expression != "" {
		// Expression flag takes precedence
		fieldPath = o.Expression
	}

	// Look up the kind
	kindDef, ok := schema.GetKind(kindName)
	if !ok {
		err := fmt.Errorf("unknown kind %q. Available kinds: %s",
			kindName, strings.Join(schema.GetGlobalRegistry().Names(), ", "))
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// If there are additional args, treat them as path components
	if len(args) > 1 {
		// Support: scafctl explain provider schema properties
		fieldPath = strings.Join(args[1:], ".")
	}

	// Create formatter
	formatter := schema.NewFormatterWithWriter(w)
	if o.Recursive {
		formatter.WithOptions(schema.FormatOptions{
			ShowNestedFields: true,
			MaxDepth:         10,
			ShowValidation:   true,
			Compact:          false,
		})
	}

	// If no field path, show the full type
	if fieldPath == "" {
		formatter.FormatType(kindDef.TypeInfo)
		return nil
	}

	// Drill into specific field
	fieldInfo, err := schema.IntrospectField(kindDef.TypeInstance, fieldPath)
	if err != nil {
		err = fmt.Errorf("cannot find field %q in %s: %w", fieldPath, kindDef.Name, err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	formatter.FormatField(fieldInfo)
	return nil
}
